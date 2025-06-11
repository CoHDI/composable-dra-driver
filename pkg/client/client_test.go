package client

import (
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func buildTestCDIClient(t testing.TB, tenantID string, clusterID string) (*CDIClient, *httptest.Server, ku.TestControllerShutdownFunc) {
	server, certPem := CreateTLSServer(t)
	server.StartTLS()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	secret := config.CreateSecret(certPem)
	testConfig := ku.TestConfig{
		Secret: secret,
	}
	controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
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
	server, certPem := CreateTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	testCases := []struct {
		name           string
		host           string
		password       string
		realm          string
		certificate    string
		expectedToken  *IMToken
		expectedErr    bool
		expectedErrMsg string
	}{
		{
			name:        "When getting IM token with a correct password",
			host:        parsedURL.Host,
			password:    "pass",
			realm:       "CDI_DRA_Test",
			certificate: certPem,
			expectedToken: &IMToken{
				AccessToken:      "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":775710000}`)),
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
			name:           "When getting error response due to certification error",
			host:           parsedURL.Host,
			password:       "invalid-pass",
			realm:          "CDI_DRA_Test",
			certificate:    certPem,
			expectedErr:    true,
			expectedErrMsg: "received unsuccessful response",
		},
		{
			name:           "When failing to connect to non-existent server",
			host:           "non-existent-host",
			password:       "pass",
			realm:          "CDI_DRA_Test",
			certificate:    certPem,
			expectedErr:    true,
			expectedErrMsg: "dial tcp: lookup",
		},
		{
			name:           "When provided a invalid realm",
			host:           parsedURL.Host,
			password:       "pass",
			realm:          "invalid-charactor-#$%&\t\n",
			certificate:    certPem,
			expectedErr:    true,
			expectedErrMsg: "received unsuccessful response",
		},
		{
			name:           "When getting nil response",
			host:           parsedURL.Host,
			password:       "pass",
			realm:          "Nil_Test",
			certificate:    certPem,
			expectedErr:    true,
			expectedErrMsg: "failed to read response data",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret := config.CreateSecret(tc.certificate)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: tc.host,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
			idManagerSecret := idManagerSecret{
				username:      "user",
				password:      tc.password,
				realm:         tc.realm,
				client_id:     "0001",
				client_secret: "secret",
				certificate:   tc.certificate,
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
			tenantId:    "0001",
			expectedErr: false,
			expectedMachineList: &FMMachineList{
				Data: FMMachines{
					Machines: []FMMachine{
						{
							MachineUUID: "0001",
							FabricID:    ptr.To(1),
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterID := "0001"
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
					t.Errorf("expected machine list: %#v, got %#v", tc.expectedMachineList, mList)
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
			tenantId:    "0001",
			machineUUID: "0001",
			deviceModel: "A100",
			expectedErr: false,
			expectedAvailableReservedResources: &FMAvailableReservedResources{
				ReservedResourceNum: 5,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterID := "0001"
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
					t.Errorf("expected available reserved resources: %#v, got %#v", tc.expectedAvailableReservedResources, avaialbleNum)
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
			tenantId:    "0001",
			clusterId:   "0001",
			expectedErr: false,
			expectedNodeGroups: &CMNodeGroups{
				NodeGroups: []CMNodeGroup{
					{
						UUID: "0001",
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
			tenantId:      "0001",
			clusterId:     "0001",
			nodeGroupUUID: "0001",
			expectedErr:   false,
			expectedNodeGroupInfo: &CMNodeGroupInfo{
				UUID: "0001",
				MachineIDs: []string{
					"0001",
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
			tenantId:    "0001",
			clusterId:   "0001",
			machineUUID: "0001",
			expectedErr: false,
			expectedNodeDetails: &CMNodeDetails{
				Data: CMTenant{
					Cluster: CMCluster{
						Machine: CMMachine{
							UUID: "0001",
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
													Value:    "A100",
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
					t.Errorf("expected node groups: %#v, got %#v", tc.expectedNodeDetails, nodeDetails)
				}
			}
		})
	}
}
