package kube_utils

import (
	"cdi_dra/pkg/config"
	"testing"
)

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
