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
		name                         string
		cm                           *corev1.ConfigMap
		expectedDriverNames          []string
		expectedLength               int
		expectedIndex                int
		expectedModelNameLength      int
		expectedDeviceNameLength     int
		expectedCoexistFactors       int
		expectedAttributeFactors     int
		expectedAttributeKeyLength   int
		expectedAttributeValueLength int
		expectedDriverNameLength     int
		expectedErr                  bool
		expectedErrMsg               string
	}{
		{
			name:                "When correct ConfigMap is provided",
			cm:                  cms[0],
			expectedDriverNames: []string{"test-driver-1", "test-driver-2"},
			expectedLength:      3,
			expectedErr:         false,
		},
		{
			name:        "When device-info in ConfigMap is not existed",
			cm:          cms[1],
			expectedErr: false,
		},
		{
			name:           "When device-info in ConfigMap is not formed as YAML",
			cm:             cms[2],
			expectedErr:    true,
			expectedErrMsg: "cannot unmarshal",
		},
		{
			name:           "When index is -1 in device-info",
			cm:             cms[CaseDevInfoIndexMinus],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'Index' failed on the 'gte' tag",
		},
		{
			name:           "When index is 0 in device-info",
			cm:             cms[CaseDevInfoIndexZero],
			expectedLength: 1,
			expectedErr:    false,
		},
		{
			name:           "When index is 10000",
			cm:             cms[CaseDevInfoIndex10000],
			expectedLength: 1,
			expectedIndex:  10000,
			expectedErr:    false,
		},
		{
			name:           "When index 10001B",
			cm:             cms[CaseDevInfoIndex10001],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'Index' failed on the 'lte' tag",
		},
		{
			name:                    "When cdi-model-name length is 1kB",
			cm:                      cms[CaseDevInfoModel1000B],
			expectedLength:          1,
			expectedErr:             false,
			expectedModelNameLength: 1000,
		},
		{
			name:           "When cdi-model-name length is 1001B",
			cm:             cms[CaseDevInfoModel1001B],
			expectedLength: 1,
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'CDIModelName' failed on the 'max' tag",
		},
		{
			name:                     "When k8s-device-name length is 50B",
			cm:                       cms[CaseDevInfoDevice50B],
			expectedLength:           1,
			expectedErr:              false,
			expectedDeviceNameLength: 50,
		},
		{
			name:           "When k8s-device-name length is 51B",
			cm:             cms[CaseDevInfoDevice51B],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'K8sDeviceName' failed on the 'max' tag",
		},
		{
			name:           "When k8s-device-name violates DNS label format",
			cm:             cms[CaseDevInfoDeviceNotDNSLabel],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'K8sDeviceName' failed on the 'is-dns' tag",
		},
		{
			name:                   "When cannot-coexist-with has 100 factors",
			cm:                     cms[CaseDevInfoCoexist100],
			expectedLength:         1,
			expectedErr:            false,
			expectedCoexistFactors: 100,
		},
		{
			name:           "When cannot-coexist-with has 101 factors",
			cm:             cms[CaseDevInfoCoexist101],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'CanNotCoexistWith' failed on the 'max' tag",
		},
		{
			name:                     "When dra-attributes has 100 factors",
			cm:                       cms[CaseDevInfoAttr100],
			expectedLength:           1,
			expectedErr:              false,
			expectedAttributeFactors: 100,
		},
		{
			name:           "When dra-attributes has 101 factors",
			cm:             cms[CaseDevInfoAttr101],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'DRAAttributes' failed on the 'max' tag",
		},
		{
			name:                       "When dra-attributes' key length is 1kB",
			cm:                         cms[CaseDevInfoAttrKey1000B],
			expectedErr:                false,
			expectedLength:             1,
			expectedAttributeKeyLength: 1000,
		},
		{
			name:           "When dra-attributes' key length is 1001B",
			cm:             cms[CaseDevInfoAttrKey1001B],
			expectedErr:    true,
			expectedErrMsg: "failed on the 'max' tag",
		},
		{
			name:                         "When dra-attributes' value length is 1kB",
			cm:                           cms[CaseDevInfoAttrValue1000B],
			expectedErr:                  false,
			expectedLength:               1,
			expectedAttributeValueLength: 1000,
		},
		{
			name:           "When dra-attributes' key length is 1001B",
			cm:             cms[CaseDevInfoAttrValue1001B],
			expectedErr:    true,
			expectedErrMsg: "failed on the 'max' tag",
		},
		{
			name:                     "When driver-name length is 1kB",
			cm:                       cms[CaseDevInfoDriver1000B],
			expectedErr:              false,
			expectedLength:           1,
			expectedDriverNameLength: 1000,
		},
		{
			name:           "When driver-name length is 1001B",
			cm:             cms[CaseDevInfoDriver1001B],
			expectedErr:    true,
			expectedErrMsg: "Error:Field validation for 'DriverName' failed on the 'max' tag",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			devInfos, err := GetDeviceInfos(tc.cm)
			if tc.expectedErr {
				if err == nil {
					t.Fatal("expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("expected error: %q, got %q", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if devInfos != nil {
					if len(devInfos) != tc.expectedLength {
						t.Error("unexpected length of device info")
					}
					for _, devInfo := range devInfos {
						if len(tc.expectedDriverNames) > 0 && !slices.Contains(tc.expectedDriverNames, devInfo.DriverName) {
							t.Errorf("expected driver name %v, but got %v", tc.expectedDriverNames, devInfo.DriverName)
						}
						if tc.expectedIndex != 0 && devInfo.Index != tc.expectedIndex {
							t.Errorf("unexpected index, expected index %d but got %d", tc.expectedIndex, devInfo.Index)
						}
						if tc.expectedModelNameLength > 0 && len(devInfo.CDIModelName) != tc.expectedModelNameLength {
							t.Errorf("unexpected cdi-model-name length, expected length %d but got %d", tc.expectedModelNameLength, len(devInfo.CDIModelName))
						}
						if tc.expectedDeviceNameLength > 0 && len(devInfo.K8sDeviceName) != tc.expectedDeviceNameLength {
							t.Errorf("unexpected k8s-device-name length, expected length %d but got %d", tc.expectedDeviceNameLength, len(devInfo.K8sDeviceName))
						}
						if tc.expectedCoexistFactors > 0 && len(devInfo.CanNotCoexistWith) != tc.expectedCoexistFactors {
							t.Errorf("unexpected cannot-coexist-with length, expected length %d but got %d", tc.expectedCoexistFactors, len(devInfo.CanNotCoexistWith))
						}
						if tc.expectedAttributeFactors > 0 && len(devInfo.DRAAttributes) != tc.expectedAttributeFactors {
							t.Errorf("unexpected dra-attributes length, expected length %d but got %d", tc.expectedAttributeFactors, len(devInfo.DRAAttributes))
						}
						for key, value := range devInfo.DRAAttributes {
							if tc.expectedAttributeKeyLength > 0 {
								if len(key) != tc.expectedAttributeKeyLength {
									t.Errorf("unexpected dra-attributes' key length, expected length %d but got %d", tc.expectedAttributeKeyLength, len(key))
								}
							}
							if tc.expectedAttributeValueLength > 0 {
								if len(value) != tc.expectedAttributeValueLength {
									t.Errorf("unexpected dra-attributes' key length, expected length %d but got %d", tc.expectedAttributeValueLength, len(value))
								}
							}
						}
						if tc.expectedDriverNameLength > 0 && len(devInfo.DriverName) != tc.expectedDriverNameLength {
							t.Errorf("unexpected driver-name length, expected length %d but got %d", tc.expectedDriverNameLength, len(devInfo.DriverName))
						}
					}
				}
			}
		})
	}
}

func TestGetLabelPrefix(t *testing.T) {
	cms, err := CreateConfigMap()
	if err != nil {
		t.Fatalf("failed to get configmap")
	}
	testCases := []struct {
		name                string
		cm                  *corev1.ConfigMap
		expectedErr         bool
		expectedLabelPrefix string
		expectedLabelLength int
		expectedErrMsg      string
	}{
		{
			name:                "When correct ConfigMap is provided",
			cm:                  cms[0],
			expectedErr:         false,
			expectedLabelPrefix: "cohdi.com",
		},
		{
			name:                "When label-prefix length is 100B",
			cm:                  cms[CaseLabelPrefix100B],
			expectedErr:         false,
			expectedLabelLength: 100,
		},
		{
			name:           "When label-prefix length is 101B",
			cm:             cms[CaseLabelPrefix101B],
			expectedErr:    true,
			expectedErrMsg: "label-prefix validation error",
		},
		{
			name:           "When label-prefix is invalid label",
			cm:             cms[CaseLabelPrefixInvalid],
			expectedErr:    true,
			expectedErrMsg: "label-prefix validation error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labelPrefix, err := GetLabelPrefix(tc.cm)
			if tc.expectedErr {
				if err == nil {
					t.Fatal("expected error, but got none")
				}
				if err.Error() != tc.expectedErrMsg {
					t.Errorf("unexpected error message, expected %s but got %s", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(tc.expectedLabelPrefix) > 0 && labelPrefix != tc.expectedLabelPrefix {
					t.Errorf("unexpected label prefix, expected %s but got %s", tc.expectedLabelPrefix, labelPrefix)
				}
				if tc.expectedLabelLength > 0 && len(labelPrefix) != tc.expectedLabelLength {
					t.Errorf("unexpected label prefix length, expected %d but got %d", tc.expectedLabelLength, len(labelPrefix))
				}
			}
		})
	}
}
