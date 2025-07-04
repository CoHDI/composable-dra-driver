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

	machineAPI := &metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: MachineAPIVersion,
		},
		GroupVersion: MachineAPIGroup + "/" + MachineAPIVersion,
		APIResources: []metav1.APIResource{
			{
				Name: MachineResourceName,
			},
		},
	}
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
	kubeclient.Fake.Resources = append(kubeclient.Fake.Resources, machineAPI, bmhAPI)

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

	machineObjects := make([]runtime.Object, 0)
	for _, machine := range testConfig.Machines {
		if machine != nil {
			machineObjects = append(machineObjects, machine)
		}
	}
	for _, bmh := range testConfig.BMHs {
		if bmh != nil {
			machineObjects = append(machineObjects, bmh)
		}
	}
	dynamicclient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: Metal3APIGroup, Version: Metal3APIVersion, Resource: BareMetalHostResourceName}: "kindList",
			{Group: MachineAPIGroup, Version: MachineAPIVersion, Resource: MachineResourceName}:     "kindList",
		},
		machineObjects...,
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

func CreateNodeBMHMachines(num int, namespace string, useCapiBmh bool) (node *corev1.Node, bmh *unstructured.Unstructured, machine *unstructured.Unstructured) {
	if useCapiBmh {
		bmh = &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       "BareMetalHost",
				"apiVersion": Metal3APIGroup + "/" + Metal3APIVersion,
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("test-bmh-%d", num),
					"namespace": namespace,
					"uid":       fmt.Sprintf("test-providerid-%d", num),
					"annotations": map[string]interface{}{
						"cluster-manager.cdi.io/machine": fmt.Sprintf("test-node-%d", num),
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
			ProviderID: fmt.Sprintf("test://test-providerid-%d", num),
		},
	}
	return node, bmh, machine
}

//func CreateBMHs(bmhNum int, namespace string) *unstructured.Unstructured {
//	bmh := &unstructured.Unstructured{
//		Object: map[string]interface{}{
//			"kind":       "BareMetalHost",
//			"apiVersion": Metal3APIVersion,
//			"metadata": map[string]interface{}{
//				"name":      fmt.Sprintf("test-bmh-%d", bmhNum),
//				"namespace": namespace,
//				"uid":       fmt.Sprintf("test-providerid-%d", bmhNum),
//			},
//		},
//	}
//}
