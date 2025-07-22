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
	"log/slog"
	"math/rand"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
)

const (
	DeviceInfoKey  = "device-info"
	LabelPrefixKey = "label-prefix"
)

type Config struct {
	LogLevel      int
	ScanInterval  time.Duration
	TenantID      string
	ClusterID     string
	CDIEndpoint   string
	UseCapiBmh    bool
	UseCM         bool
	BindingTimout *int64
}

type DeviceInfo struct {
	// Index of device
	Index int `yaml:"index"`
	// Name of a device model registered to ResourceManager in CDI
	CDIModelName string `yaml:"cdi-model-name"`
	// Attributes of ResourceSlice that will be exposed. It corresponds to vendor's ResourceSlice
	DRAAttributes map[string]string `yaml:"dra-attributes"`
	// Name of vendor DRA driver for a device
	DriverName string `yaml:"driver-name"`
	// DRA pool name or label name affixed to a node. Basic format is "<vendor>-<model>"
	K8sDeviceName string `yaml:"k8s-device-name"`
	// List of device indexes unable to coexist in the same node
	CanNotCoexistWith []int `yaml:"cannot-coexists-with"`
}

func GetDeviceInfos(cm *corev1.ConfigMap) ([]DeviceInfo, error) {
	if cm.Data == nil {
		slog.Warn("configmap data is nil")
		return nil, nil
	}
	if devInfoStr, found := cm.Data[DeviceInfoKey]; !found {
		slog.Warn("configmap device-info is nil")
		return nil, nil
	} else {
		var devInfo []DeviceInfo
		bytes := []byte(devInfoStr)
		err := yaml.Unmarshal(bytes, &devInfo)
		if err != nil {
			slog.Error("Failed yaml unmarshal", "error", err)
			return nil, err
		}
		return devInfo, nil
	}
}

func GetLabelPrefix(cm *corev1.ConfigMap) (string, error) {
	if cm.Data == nil {
		slog.Warn("configmap data is nil")
		return "", nil
	}
	if labelPrefix, found := cm.Data[LabelPrefixKey]; !found {
		slog.Warn("configmap label-prefix is nil")
		return "", nil
	} else {
		return labelPrefix, nil
	}
}

const CharSet = "123456789"

func RandomString(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = CharSet[rand.Intn(len(CharSet))]
	}
	return string(result)
}
