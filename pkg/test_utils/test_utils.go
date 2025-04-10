package test_utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateConfigMap() []*corev1.ConfigMap {
	cm1 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-1",
			Namespace: "composable-dra",
		},
		Data: map[string]string{
			"device-info": `
- index: 1
  cdi-model-name: "A100 40G"
  dra-attributes:
    productName: "NVIDIA A100 40GB PCIe"
  label-key-model: "composable-a100-40G"
  driver-name: "gpu.nvidia.com"
  k8s-device-name: "nvidia-a100-40"
  cannot-coexists-with: [2, 3, 4]`,
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

	return cms
}
