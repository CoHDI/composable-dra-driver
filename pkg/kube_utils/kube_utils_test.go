package kube_utils

import (
	"cdi_dra/pkg/config"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGroupVersionHasResource(t *testing.T) {
	testCases := []struct {
		name         string
		DRAEnable    bool
		groupVersion string
		resourceName string
		expectedErr  bool
	}{
		{
			name:         "When cofirming DRA resource exists",
			DRAEnable:    true,
			groupVersion: "resource.k8s.io/v1beta1",
			resourceName: "resourceslices",
			expectedErr:  false,
		},
		{
			name:         "When specifying not existed resource",
			DRAEnable:    true,
			groupVersion: "dummy.k8s.io/v1",
			resourceName: "dummy",
			expectedErr:  true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			discoveryClient := CreateDiscoveryClient(tc.DRAEnable)
			available, err := groupVersionHasResource(discoveryClient, tc.groupVersion, tc.resourceName)
			if tc.expectedErr {
				if err == nil {
					t.Errorf("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !available {
					t.Errorf("expected the resource %s is available but got unavailable", tc.resourceName)
				}
			}
		})
	}
}

func TestIsDRAEnabled(t *testing.T) {
	testCases := []struct {
		name        string
		DRAEnable   bool
		expectedErr bool
	}{
		{
			name:        "When DRA is enabled",
			DRAEnable:   true,
			expectedErr: false,
		},
		{
			name:        "When DRA is disabled",
			DRAEnable:   false,
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			discoveryClient := CreateDiscoveryClient(tc.DRAEnable)
			enabled := IsDRAEnabled(discoveryClient)
			if tc.expectedErr {
				if enabled {
					t.Errorf("expected DRA is disable but enabled")
				}
			} else if !tc.expectedErr {
				if !enabled {
					t.Errorf("unexpoected error: DRA is not enabled")
				}
			}
		})
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

	testConfig := &TestConfig{}
	configMaps, err := config.CreateConfigMap()
	if err != nil {
		t.Fatalf("failed to get configmap")
	}
	testConfig.ConfigMaps = configMaps
	controllers, stop := MustCreateKubeControllers(t, testConfig)
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

func TestKubeControllerFindMachineUUIDByProviderID(t *testing.T) {
	testCases := []struct {
		name                string
		nodeCount           int
		providerID          string
		expectedErr         bool
		expectedMachineUUID string
	}{
		{
			name:                "When correctly provider ID is provided",
			nodeCount:           1,
			providerID:          "test-providerid-0",
			expectedErr:         false,
			expectedMachineUUID: "test-node-0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeCount := 1
			testConfig := &TestConfig{
				Nodes:    make([]*corev1.Node, nodeCount),
				BMHs:     make([]*unstructured.Unstructured, nodeCount),
				Machines: make([]*unstructured.Unstructured, nodeCount),
			}
			useCapiBmh := true
			for i := 0; i < nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = CreateNodeBMHMachines(i, "test-namespace", useCapiBmh)
			}
			controllers, stop := MustCreateKubeControllers(t, testConfig)
			defer stop()

			muuid, err := controllers.FindMachineUUIDByProviderID(normalizedProviderString(tc.providerID))
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if muuid != tc.expectedMachineUUID {
					t.Errorf("unexpected machine uuid, expected: %s, but got: %s", tc.expectedMachineUUID, muuid)
				}
			}
		})
	}
}
