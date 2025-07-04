package kube_utils

import (
	"cdi_dra/pkg/config"
	"slices"
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
			groupVersion: "resource.k8s.io/v1beta2",
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
			testConfig := &config.TestConfig{
				Spec: config.TestSpec{
					DRAenabled: tc.DRAEnable,
				},
			}
			kubeclient, _ := CreateTestClient(t, testConfig)
			available, err := groupVersionHasResource(kubeclient.Discovery(), tc.groupVersion, tc.resourceName)
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
			testConfig := &config.TestConfig{
				Spec: config.TestSpec{
					DRAenabled: tc.DRAEnable,
				},
			}
			kubeclient, _ := CreateTestClient(t, testConfig)
			enabled := IsDRAEnabled(kubeclient.Discovery())
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

func TestKubeControllersGetNode(t *testing.T) {
	testCases := []struct {
		name             string
		nodeName         string
		expectedNodeName string
		expectedErr      bool
	}{
		{
			name:             "When correctly getting node as expected",
			nodeName:         "test-node-1",
			expectedNodeName: "test-node-1",
			expectedErr:      false,
		},
		{
			name:        "When specify not existed node name",
			nodeName:    "dummy-node",
			expectedErr: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Spec: config.TestSpec{
					UseCapiBmh: false,
				},
				Nodes:    make([]*corev1.Node, config.TestNodeCount),
				BMHs:     make([]*unstructured.Unstructured, config.TestNodeCount),
				Machines: make([]*unstructured.Unstructured, config.TestNodeCount),
			}
			for i := 0; i < config.TestNodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = CreateNodeBMHMachines(i, "test-namespace", testConfig.Spec.UseCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			node, err := controllers.GetNode(tc.nodeName)
			if tc.expectedErr {
				if err == nil {
					t.Errorf("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if node != nil {
					if node.GetName() != tc.expectedNodeName {
						t.Errorf("unexpected node got, expected %s but got %s", tc.expectedNodeName, node.GetName())
					}
				}
			}
		})
	}
}

func TestKubeControllersGetConfigMap(t *testing.T) {
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

	configMaps, err := config.CreateConfigMap()
	if err != nil {
		t.Fatalf("failed to get configmap")
	}
	testConfig := &config.TestConfig{
		ConfigMaps: configMaps,
	}

	kubeclient, dynamicclient := CreateTestClient(t, testConfig)
	controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
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

func TestKubeControllersListProviderIDs(t *testing.T) {
	testCases := []struct {
		name                     string
		nodeCount                int
		nodeIndex                int
		useCapiBmh               bool
		expectedErr              bool
		expectedProviderID       normalizedProviderID
		expectedProviderIDLength int
	}{
		{
			name:                     "When provider ID is correctly listed if useCapiBmh is true",
			nodeCount:                1,
			useCapiBmh:               true,
			expectedErr:              false,
			expectedProviderID:       "test-providerid-0",
			expectedProviderIDLength: 1,
		},
		{
			name:                     "When provider ID is correctly listed if useCapiBmh is false",
			nodeCount:                2,
			useCapiBmh:               false,
			expectedErr:              false,
			expectedProviderID:       "test-providerid-1",
			expectedProviderIDLength: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Nodes:    make([]*corev1.Node, tc.nodeCount),
				BMHs:     make([]*unstructured.Unstructured, tc.nodeCount),
				Machines: make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = CreateNodeBMHMachines(i, "test-namespace", tc.useCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			providerIDs, err := controllers.ListProviderIDs()
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(providerIDs) != tc.expectedProviderIDLength {
					t.Errorf("unexpected provider ID length: expected %d, but got %d", tc.expectedProviderIDLength, len(providerIDs))
				}
				if !slices.Contains(providerIDs, tc.expectedProviderID) {
					t.Errorf("expected provider ID not got: expected %s", tc.expectedProviderID)
				}
			}
		})
	}
}

func TestKubeControllersFindNodeNameByProviderID(t *testing.T) {
	testCases := []struct {
		name             string
		nodeCount        int
		useCapiBmh       bool
		providerID       string
		expectedErr      bool
		expectedNodeName string
	}{
		{
			name:             "When correctly providerID is provided if useCapiBmh is true",
			nodeCount:        1,
			useCapiBmh:       true,
			providerID:       "test-providerid-0",
			expectedErr:      false,
			expectedNodeName: "test-node-0",
		},
		{
			name:             "When correctly providerID is provided if useCapiBmh is false",
			nodeCount:        2,
			useCapiBmh:       false,
			providerID:       "test-providerid-1",
			expectedErr:      false,
			expectedNodeName: "test-node-1",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Nodes:    make([]*corev1.Node, tc.nodeCount),
				BMHs:     make([]*unstructured.Unstructured, tc.nodeCount),
				Machines: make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = CreateNodeBMHMachines(i, "test-namespace", tc.useCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			nodeName, err := controllers.FindNodeNameByProviderID(normalizedProviderID(tc.providerID))
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if nodeName != tc.expectedNodeName {
					t.Errorf("unexpected node name got: expected %s, but got %s", tc.expectedNodeName, nodeName)
				}
			}
		})
	}
}

func TestKubeControllersFindMachineUUIDByProviderID(t *testing.T) {
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
			testConfig := &config.TestConfig{
				Spec: config.TestSpec{
					UseCapiBmh: true,
					DRAenabled: true,
				},
				Nodes:    make([]*corev1.Node, tc.nodeCount),
				BMHs:     make([]*unstructured.Unstructured, tc.nodeCount),
				Machines: make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = CreateNodeBMHMachines(i, "test-namespace", testConfig.Spec.UseCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
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
