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
	"log/slog"
	"os"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func init() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
}

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
			groupVersion: "resource.k8s.io/v1",
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
		name           string
		DRAEnable      bool
		expectedEnable bool
	}{
		{
			name:           "When DRA is enabled",
			DRAEnable:      true,
			expectedEnable: true,
		},
		{
			name:           "When DRA is disabled",
			DRAEnable:      false,
			expectedEnable: false,
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
			if enabled != tc.expectedEnable {
				t.Errorf("unexpected result, expected DRA is %t but got %t", tc.expectedEnable, enabled)
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
			name:             "When correct node is obtained as expected",
			nodeName:         "test-node-1",
			expectedNodeName: "test-node-1",
			expectedErr:      false,
		},
		{
			name:        "When not existed node name is specified",
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
				Nodes: make([]*corev1.Node, config.TestNodeCount),
				BMHs:  make([]*unstructured.Unstructured, config.TestNodeCount),
			}
			for i := 0; i < config.TestNodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i] = CreateNodeBMHs(i, "test-namespace", testConfig.Spec.UseCapiBmh)
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
			name:  "When correct ConfigMap is obtained as expected",
			cmkey: "cdi-dra-dds/test-configmap-1",
			expectedConfigMap: testConfigMap{
				name:      "test-configmap-1",
				namespace: "cdi-dra-dds",
			},
			expectedErr: false,
		},
		{
			name:        "When non-exisistent ConfigMap key is provided",
			cmkey:       "non-exist-ns/non-exist-cm",
			expectedErr: false,
		},
		{
			name:        "When provided input is not formed as key",
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

func TestKubeControllersGetSecret(t *testing.T) {
	caData, err := config.CreateTestCACertificate()
	if err != nil {
		t.Fatalf("failed to create ca certificate: %v", err)
	}
	testCases := []struct {
		name                 string
		certPem              string
		secretCase           int
		expectedErr          bool
		expectedUserName     string
		expectedPassword     string
		expectedRealm        string
		expectedClientId     string
		expectedClientSecret string
		expectedCertificate  string
	}{
		{
			name:                 "When correct Secret is obtained as expected",
			certPem:              caData.CertPem,
			secretCase:           1,
			expectedErr:          false,
			expectedUserName:     "user",
			expectedPassword:     "pass",
			expectedRealm:        "CDI_DRA_Test",
			expectedClientId:     "0001",
			expectedClientSecret: "secret",
			expectedCertificate:  caData.CertPem,
		},
		{
			name:             "When Secret has username which exceeds the limit",
			secretCase:       2,
			expectedErr:      false,
			expectedUserName: config.ExceededSecretInfo,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret := config.CreateSecret(tc.certPem, tc.secretCase)
			testConfig := &config.TestConfig{
				Secret: secret,
			}
			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()
			secret, err := controllers.GetSecret("composable-dra/composable-dra-secret")
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if secret != nil {
					if len(tc.expectedUserName) > 0 && string(secret.Data["username"]) != tc.expectedUserName {
						t.Errorf("unexpected username, expected %s but got %s", tc.expectedUserName, string(secret.Data["username"]))
					}
					if len(tc.expectedPassword) > 0 && string(secret.Data["password"]) != tc.expectedPassword {
						t.Errorf("unexpected password, expected %s but got %s", tc.expectedPassword, string(secret.Data["password"]))
					}
					if len(tc.expectedRealm) > 0 && string(secret.Data["realm"]) != tc.expectedRealm {
						t.Errorf("unexpected realm, expected %s but got %s", tc.expectedRealm, string(secret.Data["realm"]))
					}
					if len(tc.expectedClientId) > 0 && string(secret.Data["client_id"]) != tc.expectedClientId {
						t.Errorf("unexpected client_id, expected %s but got %s", tc.expectedClientId, string(secret.Data["client_id"]))
					}
					if len(tc.expectedClientSecret) > 0 && string(secret.Data["client_secret"]) != tc.expectedClientSecret {
						t.Errorf("unexpected client_secret, expected %s but got %s", tc.expectedClientSecret, string(secret.Data["client_secret"]))
					}
					if len(tc.expectedCertificate) > 0 && string(secret.Data["certificate"]) != tc.expectedCertificate {
						t.Errorf("unexpected certificate, expected %s but got %s", tc.expectedCertificate, string(secret.Data["certificate"]))
					}
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
			expectedProviderID:       "00000000-0000-0000-0000-000000000000",
			expectedProviderIDLength: 1,
		},
		{
			name:                     "When provider ID is correctly listed if useCapiBmh is false",
			nodeCount:                2,
			useCapiBmh:               false,
			expectedErr:              false,
			expectedProviderID:       "00000000-0000-0000-0000-000000000001",
			expectedProviderIDLength: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Nodes: make([]*corev1.Node, tc.nodeCount),
				BMHs:  make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i] = CreateNodeBMHs(i, "test-namespace", tc.useCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			providerIDs, err := controllers.ListProviderIDs()
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
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
			name:             "When correct node name is obtained as expected if useCapiBmh is true",
			nodeCount:        1,
			useCapiBmh:       true,
			providerID:       "00000000-0000-0000-0000-000000000000",
			expectedErr:      false,
			expectedNodeName: "test-node-0",
		},
		{
			name:             "When correct node name is obtained as expected if useCapiBmh is false",
			nodeCount:        2,
			useCapiBmh:       false,
			providerID:       "00000000-0000-0000-0000-000000000001",
			expectedErr:      false,
			expectedNodeName: "test-node-1",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Nodes: make([]*corev1.Node, tc.nodeCount),
				BMHs:  make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i] = CreateNodeBMHs(i, "test-namespace", tc.useCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			nodeName, err := controllers.FindNodeNameByProviderID(normalizedProviderID(tc.providerID))
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
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
			name:                "When correct machine UUID is obtained as expected if useCapiBmh is true",
			nodeCount:           1,
			providerID:          "00000000-0000-0000-0000-000000000000",
			expectedErr:         false,
			expectedMachineUUID: "00000000-0000-0000-0000-000000000000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testConfig := &config.TestConfig{
				Spec: config.TestSpec{
					UseCapiBmh: true,
					DRAenabled: true,
				},
				Nodes: make([]*corev1.Node, tc.nodeCount),
				BMHs:  make([]*unstructured.Unstructured, tc.nodeCount),
			}
			for i := 0; i < tc.nodeCount; i++ {
				testConfig.Nodes[i], testConfig.BMHs[i] = CreateNodeBMHs(i, "test-namespace", testConfig.Spec.UseCapiBmh)
			}

			kubeclient, dynamicclient := CreateTestClient(t, testConfig)
			controllers, stop := CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			muuid, err := controllers.FindMachineUUIDByProviderID(normalizedProviderString(tc.providerID))
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
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
