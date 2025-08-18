/*
Copyright 2025 The CoHDI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube_utils

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	informerscorev1 "k8s.io/client-go/informers/core/v1"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	nodeProviderIDIndex       string        = "nodeProviderIDIndex"
	bmhProviderIDIndex        string        = "bmhProviderIDIndex"
	Metal3APIGroup            string        = "metal3.io"
	Metal3APIVersion          string        = "v1alpha1"
	BareMetalHostResourceName string        = "baremetalhosts"
	DRAAPIGroup               string        = "resource.k8s.io"
	DRAAPIVersion             string        = "v1"
	ResourceSliceResourceName string        = "resourceslices"
	KubeClientTimeOut         time.Duration = 30 * time.Second
)

type normalizedProviderID string

type KubeControllers struct {
	coreInformerFactory kubeinformers.SharedInformerFactory
	bmhInformerFactory  dynamicinformer.DynamicSharedInformerFactory
	nodeInformer        informerscorev1.NodeInformer
	configMapInformer   cache.SharedIndexInformer
	secretInformer      cache.SharedIndexInformer
	bmhInformer         kubeinformers.GenericInformer
	bmhAvailable        bool
	stopChannel         <-chan struct{}
}

func NewClientConfig() (*rest.Config, error) {
	var config *rest.Config
	var err error
	config, err = rest.InClusterConfig()
	if err != nil {
		slog.Info("Create client config: not in-cluster, try local kubeconfig")
		kubeConfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		if err != nil {
			slog.Error("Failed to create out-of-cluster client config", "error", err)
			return nil, err
		}
	}
	// Set k8s API timeout
	config.Timeout = KubeClientTimeOut

	return config, nil
}

func CreateKubeControllers(coreClient kube_client.Interface, bmhClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, useCapiBmh bool, stopChannel <-chan struct{}) (*KubeControllers, error) {
	coreInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(coreClient, 0, kubeinformers.WithNamespace("composable-dra"))
	bmhInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(bmhClient, 0)

	configMapInformer := coreInformerFactory.Core().V1().ConfigMaps().Informer()
	secretInformer := coreInformerFactory.Core().V1().Secrets().Informer()
	nodeInformer := coreInformerFactory.Core().V1().Nodes()
	if err := nodeInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
		nodeProviderIDIndex: indexNodeByProviderID,
	}); err != nil {
		slog.Error("Cannot add node indexer", "error", err)
		return nil, err
	}

	var bmhInformer kubeinformers.GenericInformer
	var bmhAvailable bool

	if useCapiBmh {
		bmhAvailable, err := groupVersionHasResource(discoveryClient,
			fmt.Sprintf("%s/%s", Metal3APIGroup, Metal3APIVersion), BareMetalHostResourceName)
		if err != nil {
			return nil, err
		}
		if bmhAvailable {
			gvrBMH := schema.GroupVersionResource{
				Group:    Metal3APIGroup,
				Version:  Metal3APIVersion,
				Resource: BareMetalHostResourceName,
			}
			bmhInformer = bmhInformerFactory.ForResource(gvrBMH)
			if err := bmhInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
				bmhProviderIDIndex: indexBMHByProviderID,
			}); err != nil {
				slog.Error("Cannot add bmh indexer", "error", err)
				return nil, err
			}
		}
	}

	return &KubeControllers{
		coreInformerFactory: coreInformerFactory,
		bmhInformerFactory:  bmhInformerFactory,
		nodeInformer:        nodeInformer,
		configMapInformer:   configMapInformer,
		secretInformer:      secretInformer,
		bmhInformer:         bmhInformer,
		bmhAvailable:        bmhAvailable,
		stopChannel:         stopChannel,
	}, nil
}

func groupVersionHasResource(client discovery.DiscoveryInterface, groupVersion, resourceName string) (bool, error) {
	resourceList, err := client.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		slog.Error("failed to get ServerGroups", "error", err, "groupVersion", groupVersion)
		return false, err
	}

	for _, r := range resourceList.APIResources {
		if r.Name == resourceName {
			slog.Info("Resource available", "resourceName", r.Name)
			return true, nil
		}
	}
	slog.Warn("Resource not available", "resourceName", resourceName)
	return false, nil
}

func indexNodeByProviderID(obj interface{}) ([]string, error) {
	if node, ok := obj.(*corev1.Node); ok {
		if node.Spec.ProviderID != "" {
			return []string{string(normalizedProviderString(node.Spec.ProviderID))}, nil
		}
		return []string{}, nil
	}
	return []string{}, nil
}

func indexBMHByProviderID(obj interface{}) ([]string, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, nil
	}
	providerID, found, err := unstructured.NestedString(u.UnstructuredContent(), "metadata", "uid")
	if err != nil || !found {
		return nil, nil
	}
	if providerID == "" {
		return nil, nil
	}
	return []string{providerID}, nil
}

func normalizedProviderString(s string) normalizedProviderID {
	split := strings.Split(s, "/")
	return normalizedProviderID(split[len(split)-1])
}

func IsDRAEnabled(discoveryClient discovery.DiscoveryInterface) bool {
	draAvailable, err := groupVersionHasResource(discoveryClient,
		fmt.Sprintf("%s/%s", DRAAPIGroup, DRAAPIVersion), ResourceSliceResourceName)
	if err != nil {
		return false
	}
	return draAvailable
}

func (kc *KubeControllers) Run() error {
	kc.coreInformerFactory.Start(kc.stopChannel)
	kc.bmhInformerFactory.Start(kc.stopChannel)

	syncFuncs := []cache.InformerSynced{
		kc.nodeInformer.Informer().HasSynced,
		kc.configMapInformer.HasSynced,
		kc.secretInformer.HasSynced,
	}
	if kc.bmhAvailable {
		syncFuncs = append(syncFuncs, kc.bmhInformer.Informer().HasSynced)
	}
	slog.Info("waiting for cached to sync")
	if !cache.WaitForCacheSync(kc.stopChannel, syncFuncs...) {
		return fmt.Errorf("syncing caches failed")
	}
	return nil
}

func (kc *KubeControllers) GetNode(nodeName string) (*corev1.Node, error) {
	obj, exists, err := kc.nodeInformer.Informer().GetIndexer().GetByKey(nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}
	if !exists {
		slog.Warn("not exists node", "nodeName", nodeName)
		return nil, nil
	}
	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", obj)
	}
	return node.DeepCopy(), nil
}

func (kc *KubeControllers) GetConfigMap(key string) (*corev1.ConfigMap, error) {
	obj, exists, err := kc.configMapInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap: %w", err)
	}
	if !exists {
		slog.Warn("not exists configmap", "configMap", key)
		return nil, nil
	}
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", obj)
	}
	return cm.DeepCopy(), nil
}

func (kc *KubeControllers) GetSecret(key string) (*corev1.Secret, error) {
	obj, exists, err := kc.secretInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}
	if !exists {
		err = fmt.Errorf("not exists secret: %s", key)
		slog.Error(err.Error())
		return nil, err
	}
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", obj)
	}
	return secret.DeepCopy(), nil
}

func (kc *KubeControllers) ListProviderIDs() ([]normalizedProviderID, error) {
	var providerIDs []normalizedProviderID

	nodes, err := kc.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		slog.Error("failed to list nodes", "error", err)
		return nil, err
	}
	for _, node := range nodes {
		providerID := node.Spec.ProviderID
		if providerID != "" {
			providerIDs = append(providerIDs, normalizedProviderString(providerID))
		} else {
			slog.Warn("node has no providerID", "name", node.GetName())
		}
	}
	slog.Debug("the number of providerIDs", "providerIDNum", len(providerIDs))
	return providerIDs, nil
}

func (kc *KubeControllers) FindNodeNameByProviderID(providerID normalizedProviderID) (string, error) {
	objs, err := kc.nodeInformer.Informer().GetIndexer().ByIndex(nodeProviderIDIndex, string(providerID))
	if err != nil {
		return "", nil
	}
	switch n := len(objs); {
	case n == 0:
		return "", nil
	case n > 1:
		return "", fmt.Errorf("internal error; expected len==1, got %v", n)
	}
	node, ok := objs[0].(*corev1.Node)
	if !ok {
		return "", fmt.Errorf("internal error; unexpected type %T", objs[0])
	}
	return node.DeepCopy().GetName(), nil
}

func (kc *KubeControllers) FindMachineUUIDByProviderID(providerID normalizedProviderID) (string, error) {
	var machineUUID string

	objs, err := kc.bmhInformer.Informer().GetIndexer().ByIndex(bmhProviderIDIndex, string(providerID))
	if err != nil {
		return "", err
	}
	switch n := len(objs); {
	case n == 0:
		slog.Warn("not found BareMetalHost for the providerID", "providerID", providerID)
		return "", nil
	case n > 1:
		return "", fmt.Errorf("internal error; expected len==1, got %v", n)
	}
	bmh, ok := objs[0].(*unstructured.Unstructured)
	if !ok {
		return "", fmt.Errorf("internal error; unexpected type %T", objs[0])
	}
	bmh = bmh.DeepCopy()
	annotations, found, err := unstructured.NestedMap(bmh.UnstructuredContent(), "metadata", "annotations")
	if err != nil {
		slog.Error("failed to get machine uuid from Unstructured", "error", err)
		return "", err
	}

	if found {
		if annotations != nil {
			x, found := annotations["cluster-manager.cdi.io/machine"]
			if !found {
				return "", nil
			}
			machineUUID, ok = x.(string)
			if !ok {
				return "", fmt.Errorf("internal error; unexpected type %T", x)
			}
		}
	}
	return machineUUID, nil
}
