package client

import (
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
		OrganizationalUnit: []string{"CoHDI"},
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
		OrganizationalUnit: []string{"CoHDI"},
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
				MachineUUID: "0001",
				FabricID:    ptr.To(1),
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
					remainder = strings.TrimPrefix(remainder, "/v2/tenants/0001/clusters")
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
					muuid := strings.TrimPrefix(remainder, "/v3/tenants/0001/clusters/0001/machines/")
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

func CreateTLSServer(t testing.TB) (*httptest.Server, string) {
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
