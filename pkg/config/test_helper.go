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
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	TestNodeCount = 9
	CA_EXPIRE     = 10 * time.Second
)

const (
	CaseDevInfoCorrect = 0
)

const (
	CaseDevInfoIndexMinus = iota + 3
	CaseDevInfoIndexZero
	CaseDevInfoIndex10000
	CaseDevInfoIndex10001
	CaseDevInfoModel1000B
	CaseDevInfoModel1001B
	CaseDevInfoDevice50B
	CaseDevInfoDevice51B
	CaseDevInfoDeviceNotDNSLabel
	CaseDevInfoCoexist100
	CaseDevInfoCoexist101
	CaseDevInfoAttr32
	CaseDevInfoAttr33
	CaseDevInfoAttrKey63B
	CaseDevInfoAttrKey64B
	CaseDevInfoAttrValue64B
	CaseDevInfoAttrValue65B
	CaseDevInfoDriver63B
	CaseDevInfoDriver64B
	CaseDevInfoEmptyDriver
	CaseDevInfoEmptyModel
	CaseDevInfoEmptyAttr
	CaseDevInfoDuplicateIndex
	CaseDevInfoDuplicateModel
	CaseDevInfoDuplicateDevice
	CaseDevInfoModelSymbol
	CaseDevInfoFullLength

	CaseLabelPrefix100B
	CaseLabelPrefix101B
	CaseLabelPrefixInvalid
)

var FullLengthModel string = RandomString(1000)
var FullLengthAttrKey string = RandomString(63) + "/" + RandomString(32)
var FullLengthAttrValue string = RandomString(64)
var FullLengthDriverName string = RandomString(63)
var FullLengthDeviceName string = RandomString(50)

var ExceededSecretInfo string = RandomString(1000)
var UnExceededSecretInfo string = RandomString(999)

type TestConfig struct {
	Spec       TestSpec
	ConfigMaps []*corev1.ConfigMap
	Secret     *corev1.Secret
	Nodes      []*corev1.Node
	BMHs       []*unstructured.Unstructured
}

type TestSpec struct {
	UseCapiBmh           bool
	UseCM                bool
	DRAenabled           bool
	AvailableDeviceCount int
	CaseDriverResource   int
	CaseDeviceInfo       int
	CaseDevice           int
	TenantID             string
	ClusterID            string
}

func CreateDeviceInfos(devInfoCase int) []DeviceInfo {
	switch devInfoCase {
	case CaseDevInfoIndexMinus:
		devInfo := DeviceInfo{
			Index:         -1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoIndexZero:
		devInfo := DeviceInfo{
			Index:         0,
			CDIModelName:  "DEVICE1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoIndex10000:
		devInfo := DeviceInfo{
			Index:         10000,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoIndex10001:
		devInfo := DeviceInfo{
			Index:         10001,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoModel1000B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  RandomString(1000),
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoModel1001B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  RandomString(1001),
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDevice50B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: RandomString(50),
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDevice51B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: RandomString(51),
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDeviceNotDNSLabel:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "TEST-DEVICE-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoCoexist100:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		for i := 2; i < 102; i++ {
			devInfo.CanNotCoexistWith = append(devInfo.CanNotCoexistWith, i)
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoCoexist101:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		for i := 2; i < 103; i++ {
			devInfo.CanNotCoexistWith = append(devInfo.CanNotCoexistWith, i)
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoAttr32:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		for i := 0; i < 31; i++ {
			devInfo.DRAAttributes[strconv.Itoa(i)] = "attribute-" + strconv.Itoa(i)
		}
		deviInfos := []DeviceInfo{devInfo}
		return deviInfos
	case CaseDevInfoAttr33:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		for i := 0; i < 32; i++ {
			devInfo.DRAAttributes[strconv.Itoa(i)] = "attribute-" + strconv.Itoa(i)
		}
		deviInfos := []DeviceInfo{devInfo}
		return deviInfos
	case CaseDevInfoAttrKey63B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName":    "TEST DEVICE 1",
				RandomString(63): "key length test",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoAttrKey64B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName":    "TEST DEVICE 1",
				RandomString(64): "key length test",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoAttrValue64B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": RandomString(64),
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoAttrValue65B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": RandomString(65),
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDriver63B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    RandomString(63),
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDriver64B:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    RandomString(64),
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoEmptyDriver:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoEmptyModel:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoEmptyAttr:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "TEST DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			// draAttributes is empty
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoDuplicateIndex:
		devInfo1 := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfo2 := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 2",
			DriverName:    "test-driver-2",
			K8sDeviceName: "test-device-2",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 2",
			},
		}
		devInfos := []DeviceInfo{devInfo1, devInfo2}
		return devInfos
	case CaseDevInfoDuplicateModel:
		devInfo1 := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfo2 := DeviceInfo{
			Index:         2,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-2",
			K8sDeviceName: "test-device-2",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 2",
			},
		}
		devInfos := []DeviceInfo{devInfo1, devInfo2}
		return devInfos
	case CaseDevInfoDuplicateDevice:
		devInfo1 := DeviceInfo{
			Index:         1,
			CDIModelName:  "DEVICE 1",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfo2 := DeviceInfo{
			Index:         2,
			CDIModelName:  "DEVICE 2",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 2",
			},
		}
		devInfos := []DeviceInfo{devInfo1, devInfo2}
		return devInfos
	case CaseDevInfoModelSymbol:
		devInfo := DeviceInfo{
			Index:         1,
			CDIModelName:  "TEST_-/+.()#:*@_DEVICE",
			DriverName:    "test-driver-1",
			K8sDeviceName: "test-device-1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	case CaseDevInfoFullLength:
		devInfo := DeviceInfo{
			Index:         10000,
			CDIModelName:  FullLengthModel,
			DriverName:    FullLengthDriverName,
			K8sDeviceName: FullLengthDeviceName,
			DRAAttributes: map[string]string{
				"productName":     "TEST DEVICE 1",
				FullLengthAttrKey: FullLengthAttrValue,
			},
		}
		for i := 0; i < 30; i++ {
			devInfo.DRAAttributes[strconv.Itoa(i)] = "attribute-" + strconv.Itoa(i)
		}
		devInfos := []DeviceInfo{devInfo}
		return devInfos
	default:
		devInfo0 := DeviceInfo{
			Index:        1,
			CDIModelName: "DEVICE 1",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
			DriverName:        "test-driver-1",
			K8sDeviceName:     "test-device-1",
			CanNotCoexistWith: []int{2, 3},
		}
		devInfo1 := DeviceInfo{
			Index:        2,
			CDIModelName: "DEVICE 2",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 2",
			},
			DriverName:        "test-driver-1",
			K8sDeviceName:     "test-device-2",
			CanNotCoexistWith: []int{1, 3},
		}
		devInfo2 := DeviceInfo{
			Index:        3,
			CDIModelName: "DEVICE 3",
			DRAAttributes: map[string]string{
				"productName": "TEST DEVICE 3",
			},
			DriverName:        "test-driver-2",
			K8sDeviceName:     "test-device-3",
			CanNotCoexistWith: []int{1, 2},
		}

		devInfos := []DeviceInfo{devInfo0, devInfo1, devInfo2}

		return devInfos
	}
}

func CreateLabelPrefix(labelPrefixCase int) string {
	switch labelPrefixCase {
	case CaseLabelPrefix100B:
		return RandomString(100)
	case CaseLabelPrefix101B:
		return RandomString(101)
	case CaseLabelPrefixInvalid:
		return "-cohdi.com"
	default:
		return "cohdi.com"
	}
}

func CreateConfigMap() ([]*corev1.ConfigMap, error) {
	deviceInfos0 := CreateDeviceInfos(CaseDevInfoCorrect)
	data0, err := yaml.Marshal(deviceInfos0)
	if err != nil {
		return nil, err
	}
	cm0 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-0",
			Namespace: "composable-dra",
		},
		Data: map[string]string{
			DeviceInfoKey:  string(data0),
			LabelPrefixKey: "cohdi.com",
		},
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
			"not-exist-device-info": "test-not-exists",
		},
	}
	cm2 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-2",
			Namespace: "composable-dra",
		},
		Data: map[string]string{
			"device-info": "not-formed-yaml",
		},
	}

	cms := []*corev1.ConfigMap{cm0, cm1, cm2}

	for caseNum := CaseDevInfoIndexMinus; caseNum <= CaseLabelPrefixInvalid; caseNum++ {
		deviceInfos := CreateDeviceInfos(caseNum)
		data, err := yaml.Marshal(deviceInfos)
		if err != nil {
			return nil, err
		}
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind: "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-configmap-%d", caseNum),
				Namespace: "composable-dra",
			},
			Data: map[string]string{
				DeviceInfoKey:  string(data),
				LabelPrefixKey: CreateLabelPrefix(caseNum),
			},
		}
		cms = append(cms, cm)
	}
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
