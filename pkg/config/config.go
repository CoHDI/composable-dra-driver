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
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	validator "github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	DeviceInfoKey  = "device-info"
	LabelPrefixKey = "label-prefix"
)

type Config struct {
	LogLevel     int
	ScanInterval time.Duration
	TenantID     string
	ClusterID    string
	CDIEndpoint  string
	UseCapiBmh   bool
	UseCM        bool
}

type DeviceInfoList struct {
	DeviceInfos []DeviceInfo `yaml:"device-info" validate:"unique=Index,unique=CDIModelName,unique=K8sDeviceName,dive"`
}

type DeviceInfo struct {
	// Index of device
	Index int `yaml:"index" validate:"gte=0,lte=10000"`
	// Name of a device model registered to ResourceManager in CDI
	CDIModelName string `yaml:"cdi-model-name" validate:"required,max=1000"`
	// Attributes of ResourceSlice that will be exposed. It corresponds to vendor's ResourceSlice
	DRAAttributes map[string]string `yaml:"dra-attributes" validate:"max=32,has-productName,dive,keys,is-qualifiedName,endkeys,max=64"`
	// Name of vendor DRA driver for a device
	DriverName string `yaml:"driver-name" validate:"required,max=63,is-dnsSubdomain"`
	// DRA pool name or label name affixed to a node. Basic format is "<vendor>-<model>"
	K8sDeviceName string `yaml:"k8s-device-name" validate:"required,max=50,is-dns"`
	// List of device indexes unable to coexist in the same node
	CanNotCoexistWith []int `yaml:"cannot-coexist-with" validate:"required,max=100"`
}

func GetDeviceInfos(cm *corev1.ConfigMap) ([]DeviceInfo, error) {
	if cm.Data == nil {
		return nil, fmt.Errorf("configmap data is nil")
	}
	if devInfoStr, found := cm.Data[DeviceInfoKey]; !found {
		return nil, fmt.Errorf("configmap device-info is nil")
	} else {
		var devInfos []DeviceInfo
		bytes := []byte(devInfoStr)
		err := yaml.Unmarshal(bytes, &devInfos)
		if err != nil {
			slog.Error("Failed yaml unmarshal", "error", err)
			return nil, err
		}
		var devInfoList DeviceInfoList
		devInfoList.DeviceInfos = devInfos
		// Validate the factor in device-info
		validate := validator.New()
		validate.RegisterValidation("is-dns", ValidateDNSLabel)
		validate.RegisterValidation("is-dnsSubdomain", ValidateDNSSubdomain)
		validate.RegisterValidation("is-qualifiedName", IsQualifiedName)
		validate.RegisterValidation("has-productName", HasProductName)
		if err := validate.Struct(devInfoList); err != nil {
			return nil, err
		}
		return devInfos, nil
	}
}

func ValidateDNSLabel(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	errs := validation.IsDNS1123Label(value)
	if len(errs) > 0 {
		seen := make(map[string]bool)
		for _, err := range errs {
			if !seen[err] {
				slog.Error("validation error. It must be DNS label", "value", value, "error", err)
				seen[err] = true
			}
		}
		return false
	} else {
		return true
	}
}

func ValidateDNSSubdomain(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	errs := validation.IsDNS1123Subdomain(value)
	if len(errs) > 0 {
		seen := make(map[string]bool)
		for _, err := range errs {
			if !seen[err] {
				slog.Error("validation error. It must be DNS subdomain", "value", value, "error", err)
				seen[err] = true
			}
		}
		return false
	} else {
		return true
	}
}

func IsQualifiedName(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	errs := validation.IsQualifiedName(value)
	if len(errs) > 0 {
		seen := make(map[string]bool)
		for _, err := range errs {
			if !seen[err] {
				slog.Error("validation error. It must be QualifiedName", "value", value, "error", err)
				seen[err] = true
			}
		}
		return false
	} else {
		return true
	}
}

func HasProductName(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(map[string]string)
	if !ok {
		slog.Error("failed to convert dra-attributes to map[string]string")
		return false
	}
	_, exists := value["productName"]
	return exists
}

func GetLabelPrefix(cm *corev1.ConfigMap) (string, error) {
	if cm.Data == nil {
		return "", fmt.Errorf("configmap data is nil")
	}
	if labelPrefix, found := cm.Data[LabelPrefixKey]; !found {
		return "", fmt.Errorf("configmap label-prefix is nil")
	} else {
		errs := validation.IsDNS1123Subdomain(labelPrefix)
		if len(labelPrefix) > 100 {
			errs = append(errs, "label-prefix length exceeds 100B")
		}
		if len(errs) > 0 {
			for _, err := range errs {
				slog.Error("validation error for label-prefix", "error", err)
			}
			return "", fmt.Errorf("label-prefix validation error")
		}
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
