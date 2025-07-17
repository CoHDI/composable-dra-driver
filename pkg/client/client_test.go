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

package client

import (
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func init() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
}

func buildTestCDIClient(t testing.TB, tenantID string, clusterID string) (*CDIClient, *httptest.Server, ku.TestControllerShutdownFunc) {
	server, certPem := CreateTLSServer(t)
	server.StartTLS()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	secret := config.CreateSecret(certPem, 1)
	testConfig := &config.TestConfig{
		Secret: secret,
	}

	kubeclient, dynamicclient := ku.CreateTestClient(t, testConfig)
	controllers, stop := ku.CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
	config := &config.Config{
		CDIEndpoint: parsedURL.Host,
		TenantID:    tenantID,
		ClusterID:   clusterID,
	}
	client, err := BuildCDIClient(config, controllers)
	if err != nil {
		t.Fatalf("failed to build cdi client: %v", err)
	}
	return client, server, stop
}

func TestCDIClientGetIMToken(t *testing.T) {
	testCases := []struct {
		name           string
		password       string
		realm          string
		certificate    string
		expectedToken  *IMToken
		expectedErr    bool
		expectedErrMsg string
	}{
		{
			name:     "When getting IM token with a correct password",
			password: "pass",
			realm:    "CDI_DRA_Test",
			expectedToken: &IMToken{
				AccessToken:      "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2069550000}`)),
				ExpiresIn:        1,
				RefreshExpiresIn: 2,
				RefreshToken:     "token2",
				TokenType:        "Bearer",
				IDToken:          "token3",
				NotBeforePolicy:  3,
				SessionState:     "efffca5t4",
				Scope:            "test profile",
			},
			expectedErr: false,
		},
		{
			name:           "When provided a invalid password",
			password:       "invalid-pass",
			realm:          "CDI_DRA_Test",
			expectedErr:    true,
			expectedErrMsg: "received unsuccessful response",
		},
		{
			name:           "When provided a invalid realm",
			password:       "pass",
			realm:          "invalid-charactor-#$%&\t\n",
			expectedErr:    true,
			expectedErrMsg: "received unsuccessful response",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := "00000000-0000-0001-0000-000000000000"
			clusterID := "00000000-0000-0000-0001-000000000000"
			client, server, stop := buildTestCDIClient(t, tenantID, clusterID)
			defer stop()
			defer server.Close()

			idManagerSecret := idManagerSecret{
				username:      "user",
				password:      tc.password,
				realm:         tc.realm,
				client_id:     "0001",
				client_secret: "secret",
			}
			ctx := context.WithValue(context.Background(), RequestIDKey{}, "test")
			imToken, err := client.GetIMToken(ctx, idManagerSecret)
			if tc.expectedErr {
				if err == nil {
					t.Errorf("expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("unexpected error: %v", err)
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedToken, imToken) {
					t.Errorf("Expected token: %v, got %v", tc.expectedToken, imToken)
				}
			}
		})
	}
}

func TestCDIClientGetFMMachineList(t *testing.T) {
	testCases := []struct {
		name                string
		tenantId            string
		expectedErr         bool
		expectedMachineList *FMMachineList
	}{
		{
			name:        "When get FM machine list",
			tenantId:    "00000000-0000-0001-0000-000000000000",
			expectedErr: false,
			expectedMachineList: &FMMachineList{
				Data: FMMachines{
					Machines: []FMMachine{
						{
							MachineUUID: "00000000-0000-0000-0000-000000000000",
							FabricID:    ptr.To(1),
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterID := "00000000-0000-0000-0001-000000000000"
			client, server, stop := buildTestCDIClient(t, tc.tenantId, clusterID)
			defer stop()
			defer server.Close()
			mList, err := client.GetFMMachineList(context.Background())
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedMachineList, mList) {
					t.Errorf("unexpected machine list: %#v, but got %#v", tc.expectedMachineList, mList)
				}
			}
		})
	}
}

func TestCDIClientGetFMAvailableReservedResources(t *testing.T) {
	testCases := []struct {
		name                               string
		tenantId                           string
		machineUUID                        string
		deviceModel                        string
		expectedErr                        bool
		expectedAvailableReservedResources *FMAvailableReservedResources
	}{
		{
			name:        "When correctly getting FM available reserved resources",
			tenantId:    "00000000-0000-0001-0000-000000000000",
			machineUUID: "00000000-0000-0000-0000-000000000000",
			deviceModel: "DEVICE 1",
			expectedErr: false,
			expectedAvailableReservedResources: &FMAvailableReservedResources{
				FabricID:            1,
				ReservedResourceNum: 2,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterID := "00000000-0000-0000-0001-000000000000"
			client, server, stop := buildTestCDIClient(t, tc.tenantId, clusterID)
			defer stop()
			defer server.Close()

			avaialbleNum, err := client.GetFMAvailableReservedResources(context.Background(), tc.machineUUID, tc.deviceModel)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedAvailableReservedResources, avaialbleNum) {
					t.Errorf("unexpected available reserved resources: %#v, but got %#v", tc.expectedAvailableReservedResources, avaialbleNum)
				}
			}
		})
	}
}

func TestCDIClientGetCMNodeGroups(t *testing.T) {
	testCases := []struct {
		name               string
		tenantId           string
		clusterId          string
		expectedErr        bool
		expectedNodeGroups *CMNodeGroups
	}{
		{
			name:        "When correctly getting CM nodegroups",
			tenantId:    "00000000-0000-0001-0000-000000000000",
			clusterId:   "00000000-0000-0000-0001-000000000000",
			expectedErr: false,
			expectedNodeGroups: &CMNodeGroups{
				NodeGroups: []CMNodeGroup{
					{
						UUID: "10000000-0000-0000-0000-000000000000",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, server, stop := buildTestCDIClient(t, tc.tenantId, tc.clusterId)
			defer stop()
			defer server.Close()

			nodeGroups, err := client.GetCMNodeGroups(context.Background())
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedNodeGroups, nodeGroups) {
					t.Errorf("expected node groups: %#v, got %#v", tc.expectedNodeGroups, nodeGroups)
				}
			}
		})
	}
}

func TestCDIClientGetCMNodeGroupInfo(t *testing.T) {
	testCases := []struct {
		name                  string
		tenantId              string
		clusterId             string
		nodeGroupUUID         string
		expectedErr           bool
		expectedNodeGroupInfo *CMNodeGroupInfo
	}{
		{
			name:          "When correctly getting CM nodegroups",
			tenantId:      "00000000-0000-0001-0000-000000000000",
			clusterId:     "00000000-0000-0000-0001-000000000000",
			nodeGroupUUID: "10000000-0000-0000-0000-000000000000",
			expectedErr:   false,
			expectedNodeGroupInfo: &CMNodeGroupInfo{
				UUID: "10000000-0000-0000-0000-000000000000",
				MachineIDs: []string{
					"00000000-0000-0000-0000-000000000000",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, server, stop := buildTestCDIClient(t, tc.tenantId, tc.clusterId)
			defer stop()
			defer server.Close()

			nodeGroup := CMNodeGroup{
				UUID: tc.nodeGroupUUID,
			}
			ngInfo, err := client.GetCMNodeGroupInfo(context.Background(), nodeGroup)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedNodeGroupInfo, ngInfo) {
					t.Errorf("expected node groups: %#v, got %#v", tc.expectedNodeGroupInfo, ngInfo)
				}
			}
		})
	}
}

func TestCDIClientGetCMNodeDetails(t *testing.T) {
	testCases := []struct {
		name                string
		tenantId            string
		clusterId           string
		machineUUID         string
		expectedErr         bool
		expectedNodeDetails *CMNodeDetails
	}{
		{
			name:        "When correctly getting CM nodegroups",
			tenantId:    "00000000-0000-0001-0000-000000000000",
			clusterId:   "00000000-0000-0000-0001-000000000000",
			machineUUID: "00000000-0000-0000-0000-000000000000",
			expectedErr: false,
			expectedNodeDetails: &CMNodeDetails{
				Data: CMTenant{
					Cluster: CMCluster{
						Machine: CMMachine{
							UUID: "00000000-0000-0000-0000-000000000000",
							ResSpecs: []CMResSpec{
								{
									Type:            "gpu",
									MinResSpecCount: ptr.To(1),
									MaxResSpecCount: ptr.To(3),
									Selector: CMSelector{
										Expression: CMExpression{
											Conditions: []Condition{
												{
													Column:   "model",
													Operator: "eq",
													Value:    "DEVICE 1",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, server, stop := buildTestCDIClient(t, tc.tenantId, tc.clusterId)
			defer stop()
			defer server.Close()

			nodeDetails, err := client.GetCMNodeDetails(context.Background(), tc.machineUUID)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedNodeDetails, nodeDetails) {
					t.Errorf("unexpected node groups, expected %#v but got %#v", tc.expectedNodeDetails, nodeDetails)
				}
			}
		})
	}
}

func TestCDIClientDo(t *testing.T) {
	expectedMachineList, _ := json.Marshal(testMachineList1)
	testCases := []struct {
		name               string
		nonExistent        string
		tenantId           string
		expectedErr        bool
		expectedErrMsg     string
		expectedBody       []byte
		expectedStatusCode int
	}{
		{
			name:               "When correct response is returned",
			tenantId:           "00000000-0000-0001-0000-000000000000",
			expectedBody:       expectedMachineList,
			expectedStatusCode: 200,
		},
		{
			name:           "When request is timeout",
			tenantId:       "00000000-0000-0400-0000-000000000000",
			expectedErr:    true,
			expectedErrMsg: "context deadline exceeded",
		},
		{
			name:               "When not found response is returned",
			tenantId:           "00000000-0000-0404-0000-000000000000",
			expectedErr:        false,
			expectedStatusCode: 404,
		},
		{
			name:           "When connecting to not-existent destination",
			nonExistent:    "dummy-",
			expectedErr:    true,
			expectedErrMsg: "context deadline exceeded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterId := "00000000-0000-0000-0001-000000000000"
			client, server, stop := buildTestCDIClient(t, tc.tenantId, clusterId)
			defer stop()
			defer server.Close()

			parsedURL, _ := url.Parse(server.URL)
			url := &url.URL{
				Scheme: "https",
				Host:   tc.nonExistent + parsedURL.Host,
				Path:   "/fabric_manager/api/v1/machines",
				RawQuery: url.Values{
					"tenant_uuid": []string{tc.tenantId},
				}.Encode(),
			}

			httpReq, err := http.NewRequest("GET", url.String(), nil)
			if err != nil {
				t.Fatalf("failed to create HTTP request, url: %s", url.String())
			}
			httpReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", testAccessToken))

			ctx := context.WithValue(context.Background(), RequestIDKey{}, config.RandomString(6))
			result, err := client.do(ctx, httpReq)

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("unexpected error message, expected %s but got %s", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(result.body) != string(tc.expectedBody) {
					t.Errorf("unexpected result body, expected %s but got %s", tc.expectedBody, result.body)
				}
				if result.statusCode != tc.expectedStatusCode {
					t.Errorf("unexpected status code, expected %d but got %d", tc.expectedStatusCode, result.statusCode)
				}
			}
		})
	}
}

func TestResultInto(t *testing.T) {
	resultBody1, _ := json.Marshal(testMachineList1)
	unSccressResponse := unsuccessfulResponse{
		Status: "400",
		Detail: responseDetail{
			Code:    "Exxxxx",
			Message: "Exxxxx Error message",
		},
	}
	resultBody2, _ := json.Marshal(unSccressResponse)

	resultBody3 := []byte("something error message")

	testCases := []struct {
		name                string
		resultBody          []byte
		statusCode          int
		intoStruct          *FMMachineList
		expectedErr         bool
		expectedErrMsg      string
		expectedMachineUUID string
	}{
		{
			name:                "When correct data is converted",
			resultBody:          resultBody1,
			statusCode:          200,
			intoStruct:          &FMMachineList{},
			expectedErr:         false,
			expectedMachineUUID: "00000000-0000-0000-0000-000000000000",
		},
		{
			name:           "When error response is returned in the form of unsuccessfulResponse",
			resultBody:     resultBody2,
			statusCode:     400,
			intoStruct:     &FMMachineList{},
			expectedErr:    true,
			expectedErrMsg: "Exxxxx Error message",
		},
		{
			name:           "When error response is returned not in the form of unsuccessfuleResponse",
			resultBody:     resultBody3,
			statusCode:     400,
			intoStruct:     &FMMachineList{},
			expectedErr:    true,
			expectedErrMsg: "something error message",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := &result{
				body:       tc.resultBody,
				statusCode: tc.statusCode,
			}

			err := result.into(context.Background(), tc.intoStruct)

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("unexpected error message, expected %s but got %s", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				var machineUUID string
				if len(tc.intoStruct.Data.Machines) > 0 {
					machineUUID = tc.intoStruct.Data.Machines[0].MachineUUID
				}
				if machineUUID != tc.expectedMachineUUID {
					t.Errorf("unexpected machine uuid, expected %s but got %s", tc.expectedMachineUUID, machineUUID)
				}
			}
		})

	}
}
