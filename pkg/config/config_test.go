package config

import (
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGetDeviceInfos(t *testing.T) {
	cms, err := CreateConfigMap()
	if err != nil {
		t.Fatalf("failed to get configmap")
	}

	testCases := []struct {
		name                string
		cm                  *corev1.ConfigMap
		expectedDriverNames []string
		expectedLength      int
		expectedErr         bool
		expectedErrMsg      string
	}{
		{
			name:                "When provided correct ConfigMap",
			cm:                  cms[0],
			expectedDriverNames: []string{"gpu.nvidia.com"},
			expectedLength:      1,
			expectedErr:         false,
		},
		{
			name:        "When not exists device-info in ConfigMap",
			cm:          cms[1],
			expectedErr: false,
		},
		{
			name:           "When device-info is not formed as YAML",
			cm:             cms[2],
			expectedErr:    true,
			expectedErrMsg: "yaml: unmarshal errors",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			devInfos, err := GetDeviceInfos(tc.cm)
			if tc.expectedErr {
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("expected error: %q, got %q", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
			if devInfos != nil {
				if len(devInfos) != tc.expectedLength {
					t.Error("unexpected length of device info")
				}
				for _, devInfo := range devInfos {
					if !slices.Contains(tc.expectedDriverNames, devInfo.DriverName) {
						t.Errorf("expected driver name %v, but got %v", tc.expectedDriverNames, devInfo.DriverName)
					}
				}
			}
		})
	}

}
