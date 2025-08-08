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
	"cdi_dra/pkg/config"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kube_client "k8s.io/client-go/kubernetes"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

type TestControllerShutdownFunc func()

func CreateTestClient(t testing.TB, testConfig *config.TestConfig) (*fakekube.Clientset, *fakedynamic.FakeDynamicClient) {
	objects := make([]runtime.Object, 0)
	for i := range testConfig.ConfigMaps {
		objects = append(objects, testConfig.ConfigMaps[i])
	}
	if testConfig.Secret != nil {
		objects = append(objects, testConfig.Secret)
	}
	for i := range testConfig.Nodes {
		objects = append(objects, testConfig.Nodes[i])
	}

	kubeclient := fakekube.NewSimpleClientset(objects...)

	bmhAPI := &metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: Metal3APIVersion,
		},
		GroupVersion: Metal3APIGroup + "/" + Metal3APIVersion,
		APIResources: []metav1.APIResource{
			{
				Name: BareMetalHostResourceName,
			},
		},
	}
	kubeclient.Fake.Resources = append(kubeclient.Fake.Resources, bmhAPI)

	if testConfig.Spec.DRAenabled {
		resourceAPI := &metav1.APIResourceList{
			TypeMeta: metav1.TypeMeta{
				Kind:       ResourceSliceResourceName,
				APIVersion: DRAAPIVersion,
			},
			GroupVersion: DRAAPIGroup + "/" + DRAAPIVersion,
			APIResources: []metav1.APIResource{
				{
					Name: ResourceSliceResourceName,
				},
			},
		}
		kubeclient.Fake.Resources = append(kubeclient.Fake.Resources, resourceAPI)
	}

	bmhObjects := make([]runtime.Object, 0)
	for _, bmh := range testConfig.BMHs {
		if bmh != nil {
			bmhObjects = append(bmhObjects, bmh)
		}
	}
	dynamicclient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: Metal3APIGroup, Version: Metal3APIVersion, Resource: BareMetalHostResourceName}: "kindList",
		},
		bmhObjects...,
	)

	return kubeclient, dynamicclient
}

func CreateTestKubeControllers(t testing.TB, testConfig *config.TestConfig, kubeclient kube_client.Interface, dynamicclient dynamic.Interface) (*KubeControllers, TestControllerShutdownFunc) {
	discoveryclient := kubeclient.Discovery()
	stopCh := make(chan struct{})
	controllers, err := CreateKubeControllers(kubeclient, dynamicclient, discoveryclient, testConfig.Spec.UseCapiBmh, stopCh)
	if err != nil {
		t.Fatal("failed to create test controller")
	}
	if err := controllers.Run(); err != nil {
		t.Fatalf("failed to run controller: %v", err)
	}

	return controllers, func() {
		close(stopCh)
	}

}

func CreateNodeBMHs(num int, namespace string, useCapiBmh bool) (node *corev1.Node, bmh *unstructured.Unstructured) {
	if useCapiBmh {
		bmh = &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       "BareMetalHost",
				"apiVersion": Metal3APIGroup + "/" + Metal3APIVersion,
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("test-bmh-%d", num),
					"namespace": namespace,
					"uid":       fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", num),
					"annotations": map[string]interface{}{
						"cluster-manager.cdi.io/machine": fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", num),
					},
				},
			},
		}

	}
	node = &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("test-node-%d", num),
			Labels: map[string]string{},
		},
		Spec: corev1.NodeSpec{
			ProviderID: fmt.Sprintf("test://00000000-0000-0000-0000-00000000000%d", num),
		},
	}
	return node, bmh
}
