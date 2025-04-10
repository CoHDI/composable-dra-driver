package kube_utils

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	nodeProviderIDIndex       string = "nodeProviderIDIndex"
	bmhProviderIDIndex        string = "bmhProviderIDIndex"
	MachineAPIGroup           string = "machine.openshift.io"
	MachineAPIVersion         string = "v1beta1"
	MachineResourceName       string = "machines"
	Metal3APIGroup            string = "metal3.io"
	Metal3APIVersion          string = "v1alpha1"
	BareMetalHostResourceName string = "baremetalhosts"
	DRAAPIGroup               string = "resource.k8s.io"
	DRAAPIVersion             string = "v1beta1"
	ResourceSliceResourceName string = "resourceslices"
)

type normalizedProviderID string

type KubeClientSets struct {
	CoreClient    kube_client.Interface
	MachineClient dynamic.Interface
}

type KubeControllers struct {
	coreInformerFactory    kubeinformers.SharedInformerFactory
	machineInformerFactory dynamicinformer.DynamicSharedInformerFactory
	nodeInformer           cache.SharedIndexInformer
	configMapInformer      cache.SharedIndexInformer
	secretInformer         cache.SharedIndexInformer
	machineInformer        kubeinformers.GenericInformer
	machineAvailable       bool
	bmhInformer            kubeinformers.GenericInformer
	bmhAvailable           bool
	stopChannel            <-chan struct{}
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
	return config, nil
}

func CreateKubeControllers(coreClient kube_client.Interface, machineClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, stopChannel <-chan struct{}) (*KubeControllers, error) {
	coreInformerFactory := kubeinformers.NewSharedInformerFactory(coreClient, 0)
	machineInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(machineClient, 0)

	configMapInformer := coreInformerFactory.Core().V1().ConfigMaps().Informer()
	secretInformer := coreInformerFactory.Core().V1().Secrets().Informer()
	nodeInformer := coreInformerFactory.Core().V1().Nodes().Informer()
	if err := nodeInformer.GetIndexer().AddIndexers(cache.Indexers{
		nodeProviderIDIndex: indexNodeByProviderID,
	}); err != nil {
		slog.Error("Cannot add node indexer", "error", err)
		return nil, err
	}

	var machineInformer kubeinformers.GenericInformer
	var bmhInformer kubeinformers.GenericInformer

	machineAvailable, err := groupVersionHasResource(discoveryClient,
		fmt.Sprintf("%s/%s", MachineAPIGroup, MachineAPIVersion), MachineResourceName)
	if err != nil {
		return nil, err
	}
	if machineAvailable {
		gvrMachine := schema.GroupVersionResource{
			Group:    MachineAPIGroup,
			Version:  MachineAPIVersion,
			Resource: MachineResourceName,
		}
		machineInformer = machineInformerFactory.ForResource(gvrMachine)
	}

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
		bmhInformer = machineInformerFactory.ForResource(gvrBMH)
		if err := bmhInformer.Informer().GetIndexer().AddIndexers(cache.Indexers{
			bmhProviderIDIndex: indexBMHByProviderID,
		}); err != nil {
			slog.Error("Cannot add bmh indexer", "error", err)
			return nil, err
		}
	}

	return &KubeControllers{
		coreInformerFactory:    coreInformerFactory,
		machineInformerFactory: machineInformerFactory,
		nodeInformer:           nodeInformer,
		configMapInformer:      configMapInformer,
		secretInformer:         secretInformer,
		machineInformer:        machineInformer,
		machineAvailable:       machineAvailable,
		bmhInformer:            bmhInformer,
		bmhAvailable:           bmhAvailable,
		stopChannel:            stopChannel,
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
	kc.machineInformerFactory.Start(kc.stopChannel)

	syncFuncs := []cache.InformerSynced{
		kc.nodeInformer.HasSynced,
		kc.configMapInformer.HasSynced,
		kc.secretInformer.HasSynced,
	}
	if kc.machineAvailable {
		syncFuncs = append(syncFuncs, kc.machineInformer.Informer().HasSynced)
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

func (kc *KubeControllers) GetConfigMap(key string) (*corev1.ConfigMap, error) {
	obj, exists, err := kc.configMapInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap: %w", err)
	}
	if !exists {
		slog.Warn("not exists configmap")
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
		return nil, nil
	}
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", obj)
	}
	return secret.DeepCopy(), nil
}

func (kc *KubeControllers) ListProviderIDs() ([]normalizedProviderID, error) {
	var providerIDs []normalizedProviderID

	objs, err := kc.machineInformer.Lister().List(labels.Everything())
	if err != nil {
		slog.Error("failed to list machine", "error", err)
		return nil, err
	}
	machines := make([]*unstructured.Unstructured, 0, len(objs))
	for _, x := range objs {
		u, ok := x.(*unstructured.Unstructured)
		if !ok {
			slog.Error("unexpected unstructured resource from lister", "type", x)
			return nil, fmt.Errorf("expected unstructured resource from lister: %T", x)
		}
		machines = append(machines, u.DeepCopy())
	}

	for _, machine := range machines {
		providerID, found, err := unstructured.NestedString(machine.UnstructuredContent(), "spec", "providerID")
		if err != nil {
			slog.Error("failed to get provider id from Unstructured", "error", err)
			return nil, err
		}

		if found {
			if providerID != "" {
				providerIDs = append(providerIDs, normalizedProviderString(providerID))
				continue
			}
		}
		slog.Warn("machine has no providerID", "name", machine.GetName())
	}
	slog.Info("the number of providerIDs", "providerIDNum", len(providerIDs))
	return providerIDs, nil
}

func (kc *KubeControllers) FindNodeNameByProviderID(providerID normalizedProviderID) (string, error) {
	objs, err := kc.nodeInformer.GetIndexer().ByIndex(nodeProviderIDIndex, string(providerID))
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
		return "", nil
	}
	switch n := len(objs); {
	case n == 0:
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
				return "", fmt.Errorf("not found machine uuid from bmh")
			}
			machineUUID, ok = x.(string)
			if !ok {
				return "", fmt.Errorf("internal error; unexpected type %T", x)
			}
		}
	}
	return machineUUID, nil
}
