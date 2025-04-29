package config

import (
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

func CreateSecret(certPem string) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind: "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "composable-dra-secret",
			Namespace: "composable-dra",
		},
		Data: map[string][]byte{
			"certificate": []byte(certPem),
		},
	}
	return secret
}
