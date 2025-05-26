package client

import (
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"crypto"
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
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/ptr"
)

const (
	CA_EXPIRE = 10 * time.Second
)

type certData struct {
	privKey crypto.Signer
	certPem string
	caTpl   *x509.Certificate
}

func createTestCACertificate() (certData, error) {
	privateCaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return certData{}, err
	}
	publicCaKey := privateCaKey.Public()

	subjectCa := pkix.Name{
		CommonName:         "ca-composable-dra-dds-test",
		OrganizationalUnit: []string{"InfraDDS"},
		Organization:       []string{"composable-dra-dds"},
		Country:            []string{"JP"},
	}
	created := time.Now()
	expire := created.Add(CA_EXPIRE)
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               subjectCa,
		NotAfter:              expire,
		NotBefore:             created,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caCertificate, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, publicCaKey, privateCaKey)
	if err != nil {
		return certData{}, err
	}
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertificate,
	}
	data := pem.EncodeToMemory(block)
	if data != nil {
		certData := certData{
			privKey: privateCaKey,
			certPem: string(data),
			caTpl:   caTpl,
		}
		return certData, nil
	} else {
		return certData{}, fmt.Errorf("failed to convert to pem")
	}
}

func createTestServerCertificate(caCertData certData) (certPEMBlock, keyPEMBlock []byte, err error) {
	privateServerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	publicServerKey := privateServerKey.Public()

	subjectServer := pkix.Name{
		CommonName:         "server-composable-dra-dds-test",
		OrganizationalUnit: []string{"InfraDDS"},
		Organization:       []string{"composable-dra-dds"},
		Country:            []string{"JP"},
	}
	created := time.Now()
	expire := created.Add(CA_EXPIRE)
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

	derServerCertificate, err := x509.CreateCertificate(rand.Reader, serverTpl, caCertData.caTpl, publicServerKey, caCertData.privKey)
	if err != nil {
		return nil, nil, err
	}

	certPEMBlock = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derServerCertificate})

	return certPEMBlock, keyPEMBlock, nil
}

var testAccessToken string = "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":775710000}`))

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

var testMachineList FMMachineList = FMMachineList{
	Data: FMMachines{
		Machines: []FMMachine{
			{
				FabricID: ptr.To(1),
			},
		},
	},
}

var testAvailableReservedResources = FMAvailableReservedResources{
	ReservedResourceNum: 5,
}

var testNodeGroups = CMNodeGroups{
	NodeGroups: []CMNodeGroup{
		{
			UUID: "0001",
		},
	},
}

var testNodeGroupInfo = CMNodeGroupInfo{
	UUID: "0001",
	MachineIDs: []string{
		"0001",
	},
}

var testNodeDetails = CMNodeDetails{
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
}

func handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		if r.URL.Path == "/id_manager/realms/CDI_DRA_Test/protocol/openid-connect/token" {
			body, _ := io.ReadAll(r.Body)
			targetString := "client_id=0001&client_secret=secret&username=user&password=pass&scope=openid&response=id_token token&grant_type=password"
			if string(body) == targetString {
				response, _ := json.Marshal(testIMToken)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(response))
			}
		}
	}
	if r.Method == "GET" {
		if r.Header.Get("Authorization") == fmt.Sprintf("Bearer %s", testAccessToken) {
			if strings.HasPrefix(r.URL.Path, "/fabric_manager/api/v1/machines") {
				remainder := strings.TrimPrefix(r.URL.Path, "/fabric_manager/api/v1/machines")
				if remainder == "" {
					for key, value := range r.URL.Query() {
						if key == "tenant_uuid" && value[0] == "0001" {
							response, _ := json.Marshal(testMachineList)
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
					}
				}
				if strings.HasSuffix(r.URL.Path, "/available-reserved-resources") {
					muuid := strings.TrimSuffix(remainder, "/available-reserved-resources")
					if muuid == "/0001" {
						var condition Condition
						query := r.URL.Query()
						if value, exist := query["tenant_uuid"]; exist && value[0] == "0001" {
							if value, exist := query["res_type"]; exist && value[0] == "gpu" {
								if value, exist := query["condition"]; exist {
									_ = json.Unmarshal([]byte(value[0]), &condition)
									if condition.Column == "model" && condition.Operator == "eq" && condition.Value == "A100" {
										response, _ := json.Marshal(testAvailableReservedResources)
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
				if strings.HasPrefix(remainder, "/v2") {
					remainder = strings.TrimPrefix(remainder, "/v2/tenants/t1/clusters")
					if strings.HasSuffix(remainder, "/nodegroups") {
						clusterId := strings.TrimSuffix(remainder, "/nodegroups")
						if clusterId == "/0001" {
							response, _ := json.Marshal(testNodeGroups)
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
					} else {
						ngId := strings.TrimPrefix(remainder, "/0001/nodegroups")
						if ngId == "/0001" {
							response, _ := json.Marshal(testNodeGroupInfo)
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							w.Write(response)
						}
					}
				}
				if strings.HasPrefix(remainder, "/v3") {
					muuid := strings.TrimPrefix(remainder, "/v3/tenants/t1/clusters/0001/machines/")
					if muuid == "0001" {
						response, _ := json.Marshal(testNodeDetails)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						w.Write(response)
					}
				}
			}
		}
	}
}

func createTLSServer(t testing.TB) (*httptest.Server, string) {
	caCertData, err := createTestCACertificate()
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
	return server, caCertData.certPem
}

func TestCDIClientGetIMToken(t *testing.T) {
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	testCases := []struct {
		name          string
		password      string
		certificate   string
		expectedToken *IMToken
		expectedErr   bool
	}{
		{
			name:        "When get IM token with a correct password",
			password:    "pass",
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
				CDIEndpoint: parsedURL.Host,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
			idManagerSecret := idManagerSecret{
				username:      "user",
				password:      tc.password,
				realm:         "CDI_DRA_Test",
				client_id:     "0001",
				client_secret: "secret",
				certificate:   tc.certificate,
			}

			imToken, err := client.GetIMToken(context.Background(), idManagerSecret)
			if tc.expectedErr {

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
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

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
							FabricID: ptr.To(1),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret := config.CreateSecret(certPem)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: parsedURL.Host,
				TenantID:    tc.tenantId,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
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
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

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
			secret := config.CreateSecret(certPem)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: parsedURL.Host,
				TenantID:    tc.tenantId,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
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
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	testCases := []struct {
		name               string
		tenantId           string
		clusterId          string
		expectedErr        bool
		expectedNodeGroups *CMNodeGroups
	}{
		{
			name:        "When correctly getting CM nodegroups",
			tenantId:    "t1",
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
			secret := config.CreateSecret(certPem)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: parsedURL.Host,
				TenantID:    tc.tenantId,
				ClusterID:   tc.clusterId,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
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
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

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
			tenantId:      "t1",
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
			secret := config.CreateSecret(certPem)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: parsedURL.Host,
				TenantID:    tc.tenantId,
				ClusterID:   tc.clusterId,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
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
	server, certPem := createTLSServer(t)
	server.StartTLS()
	defer server.Close()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

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
			tenantId:    "t1",
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
			secret := config.CreateSecret(certPem)
			testConfig := ku.TestConfig{
				Secret: secret,
			}
			controllers, stop := ku.MustCreateKubeControllers(t, &testConfig)
			defer stop()
			config := &config.Config{
				CDIEndpoint: parsedURL.Host,
				TenantID:    tc.tenantId,
				ClusterID:   tc.clusterId,
			}
			client, err := BuildCDIClient(config, controllers)
			if err != nil {
				t.Fatalf("failed to build cdi client: %v", err)
			}
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
