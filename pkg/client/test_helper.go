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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"
)

const (
	tenantID1        = "00000000-0000-0001-0000-000000000000"
	tenantID2        = "00000000-0000-0002-0000-000000000000"
	tenantIDTimeOut  = "00000000-0000-0400-0000-000000000000"
	tenantIDNotFound = "00000000-0000-0404-0000-000000000000"

	clusterID1 = "00000000-0000-0000-0001-000000000000"
)

var deviceList = []string{"DEVICE 1", "DEVICE 2", "DEVICE 3"}

func createTestServerCertificate(caCertData config.CertData) (certPEMBlock, keyPEMBlock []byte, err error) {
	privateServerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	publicServerKey := privateServerKey.Public()

	subjectServer := pkix.Name{
		CommonName:         "server-composable-dra-dds-test",
		OrganizationalUnit: []string{"CoHDI"},
		Organization:       []string{"composable-dra-dds"},
		Country:            []string{"JP"},
	}
	created := time.Now()
	expire := created.Add(config.CA_EXPIRE)
	serverTpl := &x509.Certificate{
		SerialNumber: big.NewInt(123),
		Subject:      subjectServer,
		NotAfter:     expire,
		NotBefore:    created,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	derPrivateServerKey := x509.MarshalPKCS1PrivateKey(privateServerKey)
	keyPEMBlock = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: derPrivateServerKey})

	derServerCertificate, err := x509.CreateCertificate(rand.Reader, serverTpl, caCertData.CaTpl, publicServerKey, caCertData.PrivKey)
	if err != nil {
		return nil, nil, err
	}

	certPEMBlock = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derServerCertificate})

	return certPEMBlock, keyPEMBlock, nil
}

var testAccessToken string = "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2069550000}`))

var testIMToken IMToken = IMToken{
	AccessToken:      testAccessToken,
	ExpiresIn:        1,
	RefreshExpiresIn: 2,
	RefreshToken:     "token2",
	TokenType:        "Bearer",
	IDToken:          "token3",
	NotBeforePolicy:  3,
	SessionState:     "efffca5t4",
	Scope:            "test profile",
}

var testMachineList1 FMMachineList = FMMachineList{
	Data: FMMachines{
		Machines: []FMMachine{
			{
				MachineUUID: "00000000-0000-0000-0000-000000000000",
				FabricID:    ptr.To(1),
			},
		},
	},
}

var testMachineList2 FMMachineList = FMMachineList{
	Data: FMMachines{
		Machines: []FMMachine{
			{
				MachineUUID: "00000000-0000-0000-0000-000000000000",
				FabricID:    ptr.To(1),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000001",
				FabricID:    ptr.To(2),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000002",
				FabricID:    ptr.To(3),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000003",
				FabricID:    ptr.To(1),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000004",
				FabricID:    ptr.To(2),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000005",
				FabricID:    ptr.To(3),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000006",
				FabricID:    ptr.To(1),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000007",
				FabricID:    ptr.To(2),
			},
			{
				MachineUUID: "00000000-0000-0000-0000-000000000008",
				FabricID:    ptr.To(3),
			},
		},
	},
}

var testAvailableReservedResources = map[string][]FMAvailableReservedResources{
	"DEVICE 1": {
		{
			FabricID:            1,
			ReservedResourceNum: 2,
		},
		{
			FabricID:            2,
			ReservedResourceNum: 5,
		},
		{
			FabricID:            3,
			ReservedResourceNum: 7,
		},
	},
	"DEVICE 2": {
		{
			FabricID:            1,
			ReservedResourceNum: 2,
		},
		{
			FabricID:            2,
			ReservedResourceNum: 5,
		},
		{
			FabricID:            3,
			ReservedResourceNum: 7,
		},
	},
	"DEVICE 3": {
		{
			FabricID:            1,
			ReservedResourceNum: 2,
		},
		{
			FabricID:            2,
			ReservedResourceNum: 5,
		},
		{
			FabricID:            3,
			ReservedResourceNum: 7,
		},
	},
}

var testNodeGroups1 = CMNodeGroups{
	NodeGroups: []CMNodeGroup{
		{
			Name: "NodeGroup1",
			UUID: "10000000-0000-0000-0000-000000000000",
		},
	},
}

var testNodeGroups2 = CMNodeGroups{
	NodeGroups: []CMNodeGroup{
		{
			Name: "NodeGroup1",
			UUID: "10000000-0000-0000-0000-000000000000",
		},
		{
			Name: "NodeGroup2",
			UUID: "20000000-0000-0000-0000-000000000000",
		},
		{
			Name: "NodeGroup3",
			UUID: "30000000-0000-0000-0000-000000000000",
		},
	},
}

var testNodeGroupInfos1 = []CMNodeGroupInfo{
	{
		UUID: "10000000-0000-0000-0000-000000000000",
		Name: "NodeGroup1",
		MachineIDs: []string{
			"00000000-0000-0000-0000-000000000000",
		},
	},
}

var testNodeGroupInfos2 = []CMNodeGroupInfo{
	{
		UUID: "10000000-0000-0000-0000-000000000000",
		Name: "NodeGroup1",
		MachineIDs: []string{
			"00000000-0000-0000-0000-000000000000",
			"00000000-0000-0000-0000-000000000001",
			"00000000-0000-0000-0000-000000000002",
		},
	},
	{
		UUID: "20000000-0000-0000-0000-000000000000",
		Name: "NodeGroup2",
		MachineIDs: []string{
			"00000000-0000-0000-0000-000000000003",
			"00000000-0000-0000-0000-000000000004",
			"00000000-0000-0000-0000-000000000005",
		},
	},
	{
		UUID: "30000000-0000-0000-0000-000000000000",
		Name: "NodeGroup3",
		MachineIDs: []string{
			"00000000-0000-0000-0000-000000000006",
			"00000000-0000-0000-0000-000000000007",
			"00000000-0000-0000-0000-000000000008",
		},
	},
}

var testNodeDetails1 = createTestNodeDetails(1)

var testNodeDetails2 = createTestNodeDetails(9)

func createTestNodeDetails(nodeCount int) []CMNodeDetails {
	var nodeDetails []CMNodeDetails
	for i := 0; i < nodeCount; i++ {
		nodeDetail := CMNodeDetails{
			Data: CMTenant{
				Cluster: CMCluster{
					Machine: CMMachine{
						UUID: fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i),
						ResSpecs: []CMResSpec{
							{
								Type: "gpu",
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
							{
								Type: "gpu",
								Selector: CMSelector{
									Expression: CMExpression{
										Conditions: []Condition{
											{
												Column:   "model",
												Operator: "eq",
												Value:    "DEVICE 2",
											},
										},
									},
								},
							},
							{
								Type: "gpu",
								Selector: CMSelector{
									Expression: CMExpression{
										Conditions: []Condition{
											{
												Column:   "model",
												Operator: "eq",
												Value:    "DEVICE 3",
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
		switch i / 3 {
		case 0:
			for i := range nodeDetail.Data.Cluster.Machine.ResSpecs {
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MinResSpecCount = ptr.To(1)
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MaxResSpecCount = ptr.To(3)
			}
		case 1:
			for i := range nodeDetail.Data.Cluster.Machine.ResSpecs {
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MinResSpecCount = ptr.To(2)
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MaxResSpecCount = ptr.To(6)
			}
		case 2:
			for i := range nodeDetail.Data.Cluster.Machine.ResSpecs {
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MinResSpecCount = ptr.To(3)
				nodeDetail.Data.Cluster.Machine.ResSpecs[i].MaxResSpecCount = ptr.To(12)
			}
		}
		nodeDetails = append(nodeDetails, nodeDetail)
	}
	return nodeDetails
}

func handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		targetString := "client_id=0001&client_secret=secret&username=user&password=pass&scope=openid&response=id_token token&grant_type=password"
		if string(body) == targetString {
			if r.URL.Path == "/id_manager/realms/CDI_DRA_Test/protocol/openid-connect/token" {
				response, _ := json.Marshal(testIMToken)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(response))
			}
			if r.URL.Path == "/id_manager/realms/Nil_Test/protocol/openid-connect/token" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
			}
			if r.URL.Path == "/id_manager/realms/Time_Test/protocol/openid-connect/token" {
				expiry := time.Now().Add(35 * time.Second)
				testIMToken.AccessToken = "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expiry.Unix())))
				response, _ := json.Marshal(testIMToken)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(response))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		} else {
			response := "certification error"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(response))
		}
	}
	if r.Method == "GET" {
		if r.Header.Get("Authorization") == fmt.Sprintf("Bearer %s", testAccessToken) {
			if strings.HasPrefix(r.URL.Path, "/fabric_manager/api/v1/machines") {
				remainder := strings.TrimPrefix(r.URL.Path, "/fabric_manager/api/v1/machines")
				if remainder == "" {
					for key, value := range r.URL.Query() {
						var response []byte
						if key == "tenant_uuid" && value[0] == tenantID1 {
							response, _ = json.Marshal(testMachineList1)
						}
						if key == "tenant_uuid" && value[0] == tenantID2 {
							response, _ = json.Marshal(testMachineList2)
						}
						if len(response) > 0 {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
						if key == "tenant_uuid" && value[0] == tenantIDTimeOut {
							time.Sleep(65 * time.Second)
							response, _ := json.Marshal(testMachineList1)
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
						if key == "tenant_uuid" && value[0] == tenantIDNotFound {
							w.WriteHeader(http.StatusNotFound)
						}
					}
				}
				if strings.HasSuffix(r.URL.Path, "/available-reserved-resources") {
					muuid := strings.TrimSuffix(remainder, "/available-reserved-resources")
					regex := regexp.MustCompile("^/[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$")
					if regex.MatchString(muuid) {
						index, _ := strconv.Atoi(string(muuid[len(muuid)-1]))
						var condition Condition
						query := r.URL.Query()
						if value, exist := query["tenant_uuid"]; exist && (value[0] == tenantID1 || value[0] == tenantID2) {
							if value, exist := query["res_type"]; exist && value[0] == "gpu" {
								if value, exist := query["condition"]; exist {
									_ = json.Unmarshal([]byte(value[0]), &condition)
									if condition.Column == "model" && condition.Operator == "eq" && slices.Contains(deviceList, condition.Value) {
										response, _ := json.Marshal(testAvailableReservedResources[condition.Value][(index)%3])
										w.Header().Set("Content-Type", "application/json")
										w.WriteHeader(http.StatusOK)
										w.Write(response)
									}
								}
							}
						}
					}
				}
			}
			if strings.HasPrefix(r.URL.Path, "/cluster_manager/cluster_autoscaler") {
				remainder := strings.TrimPrefix(r.URL.Path, "/cluster_manager/cluster_autoscaler")
				if strings.HasPrefix(remainder, "/v2/tenants/") {
					remainder = strings.TrimPrefix(remainder, "/v2/tenants/")
					var tenantId string
					if strings.HasPrefix(remainder, tenantID1) {
						remainder = strings.TrimPrefix(remainder, tenantID1+"/clusters")
						tenantId = tenantID1
					}
					if strings.HasPrefix(remainder, tenantID2) {
						remainder = strings.TrimPrefix(remainder, tenantID2+"/clusters")
						tenantId = tenantID2
					}
					if len(tenantId) != 0 {
						if strings.HasSuffix(remainder, "/nodegroups") {
							clusterId := strings.TrimSuffix(remainder, "/nodegroups")
							var response []byte
							if tenantId == tenantID1 {
								if clusterId == "/"+clusterID1 {
									response, _ = json.Marshal(testNodeGroups1)
								}
							}
							if tenantId == tenantID2 {
								if clusterId == "/"+clusterID1 {
									response, _ = json.Marshal(testNodeGroups2)
								}
							}
							if len(response) > 0 {
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusOK)
								w.Write(response)
							}
						} else {
							ngId := strings.TrimPrefix(remainder, "/"+clusterID1+"/nodegroups")
							var response []byte
							if tenantId == tenantID1 {
								if ngId == "/10000000-0000-0000-0000-000000000000" {
									response, _ = json.Marshal(testNodeGroupInfos1[0])
								}
							}
							if tenantId == tenantID2 {
								if ngId == "/10000000-0000-0000-0000-000000000000" {
									response, _ = json.Marshal(testNodeGroupInfos2[0])
								}
								if ngId == "/20000000-0000-0000-0000-000000000000" {
									response, _ = json.Marshal(testNodeGroupInfos2[1])
								}
								if ngId == "/30000000-0000-0000-0000-000000000000" {
									response, _ = json.Marshal(testNodeGroupInfos2[2])
								}
							}
							if len(response) > 0 {
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusOK)
								w.Write(response)
							}
						}
					}
				}
				if strings.HasPrefix(remainder, "/v3/tenants/") {
					remainder = strings.TrimPrefix(remainder, "/v3/tenants/")
					var tenantId string
					if strings.HasPrefix(remainder, tenantID1) {
						remainder = strings.TrimPrefix(remainder, tenantID1+"/clusters")
						tenantId = tenantID1
					}
					if strings.HasPrefix(remainder, tenantID2) {
						remainder = strings.TrimPrefix(remainder, tenantID2+"/clusters")
						tenantId = tenantID2
					}
					muuid := strings.TrimPrefix(remainder, "/"+clusterID1+"/machines/")
					r := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$")
					if r.MatchString(muuid) {
						index, _ := strconv.Atoi(string(muuid[len(muuid)-1]))
						var response []byte
						if tenantId == tenantID1 {
							response, _ = json.Marshal(testNodeDetails1[index])
						}
						if tenantId == tenantID2 {
							response, _ = json.Marshal(testNodeDetails2[index])
						}
						if len(response) > 0 {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
					}
				}
			}
		}
	}
}

func CreateTLSServer(t testing.TB) (*httptest.Server, string) {
	caCertData, err := config.CreateTestCACertificate()
	if err != nil {
		t.Fatalf("failed to create CA certficate")
	}

	serverCertPEMBlock, serverKeyPEMBlock, err := createTestServerCertificate(caCertData)
	if err != nil {
		t.Fatalf("failed to create server certificate: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(handleRequests))
	cert, err := tls.X509KeyPair(serverCertPEMBlock, serverKeyPEMBlock)
	if err != nil {
		t.Fatalf("failed to load server key pair: %v", err)
	}
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	return server, caCertData.CertPem
}
