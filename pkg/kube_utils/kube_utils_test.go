package kube_utils

import (
	"cdi_dra/pkg/test_utils"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

type testConfig struct {
	configMaps []*corev1.ConfigMap
}

type testConfigMap struct {
	cmName      string
	cmNameSpace string
	data        map[string]string
}

type testControllerShutdownFunc func()

func mustCreateKubeControllers(t testing.TB, testConfig *testConfig) (*KubeControllers, testControllerShutdownFunc) {
	configMapObjects := make([]runtime.Object, 0)
	for _, cm := range testConfig.configMaps {
		configMapObjects = append(configMapObjects, cm)
	}
	machineObjects := make([]runtime.Object, 0)
	kubeclient := fakekube.NewSimpleClientset(configMapObjects...)
	dynamicclient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: Metal3APIGroup, Version: Metal3APIVersion, Resource: BareMetalHostResourceName}: "kindList",
			{Group: MachineAPIGroup, Version: MachineAPIVersion, Resource: MachineResourceName}:     "kindList",
		},
		machineObjects...,
	)

	stopCh := make(chan struct{})
	controllers, err := CreateKubeControllers(kubeclient, dynamicclient, stopCh)
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

func TestKubeControllerGetConfigMap(t *testing.T) {
	type testConfigMap struct {
		name      string
		namespace string
	}
	testCases := []struct {
		name              string
		cmkey             string
		expectedConfigMap testConfigMap
		expectedErr       bool
	}{
		{
			name:  "When correctly creating ConfigMap",
			cmkey: "cdi-dra-dds/test-configmap-1",
			expectedConfigMap: testConfigMap{
				name:      "test-configmap-1",
				namespace: "cdi-dra-dds",
			},
			expectedErr: false,
		},
		{
			name:        "When get non-exisistence ConfigMap",
			cmkey:       "non-exist-ns/non-exist-cm",
			expectedErr: false,
		},
		{
			name:        "When provided invalid key",
			cmkey:       "not-key-formed",
			expectedErr: false,
		},
	}

	config := &testConfig{}
	config.configMaps = test_utils.CreateConfigMap()
	controllers, stop := mustCreateKubeControllers(t, config)
	defer stop()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := controllers.GetConfigMap(tc.cmkey)
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				} else {
					// TODO: no test case of expectedErr = true
					t.Errorf("expected error message, got %q", err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
			if cm != nil {
				if cm.Name != tc.expectedConfigMap.name {
					t.Errorf("expected %q, got %q", tc.expectedConfigMap.name, cm.Name)
				}
			}
		})
	}
}
