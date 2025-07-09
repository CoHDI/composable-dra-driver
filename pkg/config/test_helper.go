package config

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	TestNodeCount = 9
	CA_EXPIRE     = 10 * time.Second
)

var ExceededSecretInfo string = RandomString(1000)
var UnExceededSecretInfo string = RandomString(999)

type TestConfig struct {
	Spec       TestSpec
	ConfigMaps []*corev1.ConfigMap
	Secret     *corev1.Secret
	Nodes      []*corev1.Node
	BMHs       []*unstructured.Unstructured
	Machines   []*unstructured.Unstructured
}

type TestSpec struct {
	UseCapiBmh bool
	DRAenabled bool
}

func CreateConfigMap() ([]*corev1.ConfigMap, error) {
	deviceInfos := []DeviceInfo{
		{
			Index:        1,
			CDIModelName: "A100 40G",
			DRAAttributes: map[string]string{
				"productName": "NVIDIA A100 40GB PCIe",
			},
			DriverName:        "gpu.nvidia.com",
			K8sDeviceName:     "nvidia-a100-40",
			CanNotCoexistWith: []int{2, 3, 4},
		},
	}

	data, err := yaml.Marshal(deviceInfos)
	if err != nil {
		return nil, err
	}
	cm1 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-1",
			Namespace: "composable-dra",
		},
		Data: map[string]string{
			DeviceInfoKey: string(data),
		},
	}
	cm2 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-2",
			Namespace: "cdi-dra-dds",
		},
		Data: map[string]string{
			"not-exist-device-info": "test-not-exists",
		},
	}

	cm3 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-3",
			Namespace: "cdi-dra-dds",
		},
		Data: map[string]string{
			"device-info": "not-formed-yaml",
		},
	}

	cms := []*corev1.ConfigMap{cm1, cm2, cm3}

	return cms, nil
}

func CreateSecret(certPem string, secretCase int) *corev1.Secret {
	var secret *corev1.Secret
	secretType := metav1.TypeMeta{
		Kind: "Secret",
	}
	secretObject := metav1.ObjectMeta{
		Name:      "composable-dra-secret",
		Namespace: "composable-dra",
	}
	switch secretCase {
	case 1:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"username":      []byte("user"),
				"password":      []byte("pass"),
				"realm":         []byte("CDI_DRA_Test"),
				"client_id":     []byte("0001"),
				"client_secret": []byte("secret"),
				"certificate":   []byte(certPem),
			},
		}
	case 2:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"username": []byte(ExceededSecretInfo),
			},
		}
	case 3:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"password": []byte(ExceededSecretInfo),
			},
		}
	case 4:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"realm": []byte(ExceededSecretInfo),
			},
		}
	case 5:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"client_id": []byte(ExceededSecretInfo),
			},
		}
	case 6:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"client_secret": []byte(ExceededSecretInfo),
			},
		}
	case 7:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"username": []byte(UnExceededSecretInfo),
			},
		}
	case 8:
		secret = &corev1.Secret{
			TypeMeta:   secretType,
			ObjectMeta: secretObject,
			Data: map[string][]byte{
				"username":      []byte("user"),
				"password":      []byte("pass"),
				"realm":         []byte("Time_Test"),
				"client_id":     []byte("0001"),
				"client_secret": []byte("secret"),
				"certificate":   []byte(certPem),
			},
		}
	}
	return secret
}

type CertData struct {
	PrivKey crypto.Signer
	CertPem string
	CaTpl   *x509.Certificate
}

func CreateTestCACertificate() (CertData, error) {
	privateCaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return CertData{}, err
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
		return CertData{}, err
	}
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertificate,
	}
	data := pem.EncodeToMemory(block)
	if data != nil {
		certData := CertData{
			PrivKey: privateCaKey,
			CertPem: string(data),
			CaTpl:   caTpl,
		}
		return certData, nil
	} else {
		return CertData{}, fmt.Errorf("failed to convert to pem")
	}
}
