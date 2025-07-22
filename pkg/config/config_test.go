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

package config

import (
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func init() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
}

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
			expectedDriverNames: []string{"test-driver-1", "test-driver-2"},
			expectedLength:      3,
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
