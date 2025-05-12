package manager

import (
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"reflect"
	"testing"
	"time"

	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
)

func createDeviceInfos() []config.DeviceInfo {
	devInfo1 := config.DeviceInfo{
		Index:        1,
		CDIModelName: "A100 40G",
		DRAAttributes: map[string]string{
			"productName": "NVIDIA A100 40GB PCIe",
		},
		DriverName:        "gpu.nvidia.com",
		K8sDeviceName:     "nvidia-a100-40G",
		CanNotCoexistWith: []int{2, 3},
	}
	devInfo2 := config.DeviceInfo{
		Index:        2,
		CDIModelName: "H100",
		DRAAttributes: map[string]string{
			"productName": "NVIDIA H100 PCIe",
		},
		DriverName:        "gpu.nvidia.com",
		K8sDeviceName:     "nvidia-h100",
		CanNotCoexistWith: []int{1, 3},
	}

	devInfo3 := config.DeviceInfo{
		Index:        3,
		CDIModelName: "Gaudi3",
		DRAAttributes: map[string]string{
			"productName": "Intel Gaudi3",
		},
		DriverName:        "gpu.resource.intel.com",
		K8sDeviceName:     "intel-gaudi3",
		CanNotCoexistWith: []int{1, 2},
	}

	devInfos := []config.DeviceInfo{devInfo1, devInfo2, devInfo3}

	return devInfos
}

func createTestDriverResources() map[string]*resourceslice.DriverResources {
	ndr := make(map[string]*resourceslice.DriverResources)

	ndr["test.driver.com"] = &resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			"test-a100-40": {
				Slices: []resourceslice.Slice{
					{
						Devices: []resourceapi.Device{
							{
								Name: "test-a100-40-gpu1",
								Basic: &resourceapi.BasicDevice{
									Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
										"productName": {
											StringValue: ptr.To("TEST A100 40GB PCIe"),
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

	return ndr
}

func createTestManager(useCapiBmh bool) *CDIManager {
	//kubeObjects := make([]runtime.Object, 0)
	coreClient := fakekube.NewSimpleClientset()
	ndr := createTestDriverResources()

	return &CDIManager{
		coreClient:           coreClient,
		discoveryClient:      ku.CreateDiscoveryClient(true),
		namedDriverResources: ndr,
		useCapiBmh:           useCapiBmh,
	}

}

func TestInitDrvierResources(t *testing.T) {
	devInfos := createDeviceInfos()

	testCases := []struct {
		name                string
		expectedDriverNames []string
		expectedDRLength    int
		expectedDR          *resourceslice.DriverResources
	}{
		{
			name:                "When correct DeviceInfo provided",
			expectedDriverNames: []string{"gpu.nvidia.com", "gpu.resource.intel.com"},
			expectedDRLength:    2,
			expectedDR: &resourceslice.DriverResources{
				Pools: make(map[string]resourceslice.Pool),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ndr := initDriverResources(devInfos)
			if len(ndr) != tc.expectedDRLength {
				t.Errorf("not expected DriverResoures length: %d", len(ndr))
			}
			for _, drName := range tc.expectedDriverNames {
				if dr, found := ndr[drName]; !found {
					t.Errorf("not exists expected DriverName in NamedDriverResource: %s", drName)
				} else if !reflect.DeepEqual(dr, tc.expectedDR) {
					t.Error("unexpected init DriverResource")
				}
			}
		})
	}
}

func TestCDIManagerStartResourceSliceController(t *testing.T) {
	m := createTestManager(true)

	testCases := []struct {
		name                string
		expectedDriverName  string
		expectedPoolName    string
		expectedDeviceName  string
		expectedProductName string
		expectedErr         bool
	}{
		{
			name:                "When correctly create manager",
			expectedDriverName:  "test.driver.com",
			expectedPoolName:    "test-a100-40",
			expectedDeviceName:  "test-a100-40-gpu1",
			expectedProductName: "TEST A100 40GB PCIe",
			expectedErr:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cs, err := m.startResourceSliceController(ctx)
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
			count := 0
			for _, c := range cs {
				for !(c.GetStats().NumCreates > 0) && !(count == 3) {
					count++
					time.Sleep(time.Second)
				}
			}
			resourceslices, err := m.coreClient.ResourceV1beta1().ResourceSlices().List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Errorf("unexpected error in kube client List")
			}
			var rsFound bool
			var deviceFound bool
			for _, resourceslice := range resourceslices.Items {
				if resourceslice.Spec.Driver == tc.expectedDriverName {
					rsFound = true
					if resourceslice.Spec.Pool.Name != tc.expectedPoolName {
						t.Error("unexpected pool name")
					}
					for _, device := range resourceslice.Spec.Devices {
						if device.Name == tc.expectedDeviceName {
							deviceFound = true
							if *device.Basic.Attributes["productName"].StringValue != tc.expectedProductName {
								t.Error("unexpected ProductName")
							}
						}

					}
				}
			}

			if !rsFound || !deviceFound {
				t.Error("not create expected ResourceSlice")
			}
		})
	}
}
