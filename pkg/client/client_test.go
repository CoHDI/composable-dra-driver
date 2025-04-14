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
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
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

func handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		if r.URL.Path == "/id_manager/realms/CDI_DRA_Test/protocol/openid-connect/token" {
			body, _ := io.ReadAll(r.Body)
			targetString := "client_id=0001&client_secret=secret&username=user&password=pass&scope=openid&response=id_token token&grant_type=password"
			if string(body) == targetString {
				response := `{"access_token":"token1","expires_in":1,"refresh_expires_in":2,"refresh_token":"token2","token_type":"Bearer","id_token":"token3","not-before-policy":3,"session_state":"efffca5t4","scope":"test profile"}`
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(response))
			}
		}
	}

}

func TestGetIMToken(t *testing.T) {
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
	server.StartTLS()
	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	defer server.Close()

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
			certificate: caCertData.certPem,
			expectedToken: &IMToken{
				AccessToken:      "token1",
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
