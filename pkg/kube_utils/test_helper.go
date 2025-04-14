package kube_utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

type TestConfig struct {
	ConfigMaps []*corev1.ConfigMap
	Secret     *corev1.Secret
}

type TestControllerShutdownFunc func()

func MustCreateKubeControllers(t testing.TB, testConfig *TestConfig) (*KubeControllers, TestControllerShutdownFunc) {
	objects := make([]runtime.Object, 0)
	for _, cm := range testConfig.ConfigMaps {
		objects = append(objects, cm)
	}
	objects = append(objects, testConfig.Secret)
	machineObjects := make([]runtime.Object, 0)
	kubeclient := fakekube.NewSimpleClientset(objects...)
	dynamicclient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: Metal3APIGroup, Version: Metal3APIVersion, Resource: BareMetalHostResourceName}: "kindList",
			{Group: MachineAPIGroup, Version: MachineAPIVersion, Resource: MachineResourceName}:     "kindList",
		},
		machineObjects...,
	)
	machineAPI := &metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			Kind:       MachineResourceName,
			APIVersion: MachineAPIVersion,
		},
		GroupVersion: MachineAPIGroup + "/" + MachineAPIVersion,
	}
	bmhAPI := &metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			Kind:       BareMetalHostResourceName,
			APIVersion: Metal3APIVersion,
		},
		GroupVersion: Metal3APIGroup + "/" + Metal3APIVersion,
	}
	kubeclient.Fake.Resources = append(kubeclient.Fake.Resources, machineAPI, bmhAPI)
	discoveryclient := kubeclient.Discovery()

	stopCh := make(chan struct{})
	controllers, err := CreateKubeControllers(kubeclient, dynamicclient, discoveryclient, stopCh)
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
