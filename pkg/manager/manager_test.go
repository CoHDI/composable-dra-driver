package manager

import (
	"cdi_dra/pkg/client"
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	}

	return ndr
}

func createTestManager(t testing.TB, useCapiBmh bool, nodeCount int) (*CDIManager, *httptest.Server, ku.TestControllerShutdownFunc) {
	//kubeObjects := make([]runtime.Object, 0)
	coreClient := fakekube.NewSimpleClientset()
	ndr := createTestDriverResources()

	server, certPem := client.CreateTLSServer(t)
	server.StartTLS()

	secret := config.CreateSecret(certPem)
	testConfig := &ku.TestConfig{
		Secret:   secret,
		Nodes:    make([]*v1.Node, nodeCount),
		Machines: make([]*unstructured.Unstructured, nodeCount),
		BMHs:     make([]*unstructured.Unstructured, nodeCount),
	}
	for i := 0; i < nodeCount; i++ {
		testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = ku.CreateNodeBMHMachines(i, "test-namespace", useCapiBmh)
	}
	kc, stop := ku.MustCreateKubeControllers(t, testConfig)

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	cfg := &config.Config{
		CDIEndpoint: parsedURL.Host,
		TenantID:    "0001",
		ClusterID:   "0001",
	}
	cdiClient, err := client.BuildCDIClient(cfg, kc)
	if err != nil {
		t.Fatalf("failed to build CDIClient: %v", err)
	}

	return &CDIManager{
		coreClient:           coreClient,
		discoveryClient:      ku.CreateDiscoveryClient(true),
		namedDriverResources: ndr,
		cdiClient:            cdiClient,
		kubecontrollers:      kc,
		useCapiBmh:           useCapiBmh,
	}, server, stop

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
	m, _, stop := createTestManager(t, true, 1)
	defer stop()

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
			resourceslices, err := m.coreClient.ResourceV1beta2().ResourceSlices().List(ctx, metav1.ListOptions{})
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
							if *device.Attributes["productName"].StringValue != tc.expectedProductName {
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

func TestCDIManagerGetMachineUUID(t *testing.T) {
	testCases := []struct {
		name                string
		nodeCount           int
		nodeName            string
		useCapiBmh          bool
		expectedErr         bool
		expectedMachineUUID string
	}{
		{
			name:                "When correct machine uuid is got if useCapiBmh is true",
			nodeCount:           1,
			nodeName:            "test-node-0",
			useCapiBmh:          true,
			expectedErr:         false,
			expectedMachineUUID: "test-node-0",
		},
		{
			name:                "When correct machine uuid is got if useCapiBmh is false",
			nodeCount:           2,
			nodeName:            "test-node-1",
			useCapiBmh:          false,
			expectedErr:         false,
			expectedMachineUUID: "test-providerid-1",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			muuids, err := m.getMachineUUIDs()
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if muuids[tc.nodeName] != tc.expectedMachineUUID {
					t.Errorf("unexpected machine uuid got: expected %s, but got %s", tc.expectedMachineUUID, muuids[tc.nodeName])
				}
			}
		})
	}
}

func TestCDIManagerGetMachineList(t *testing.T) {
	testCases := []struct {
		name        string
		nodeCount   int
		useCapiBmh  bool
		expectedErr bool
	}{
		{
			name:        "When correctly getting machine list",
			nodeCount:   1,
			useCapiBmh:  true,
			expectedErr: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, server, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			defer server.Close()

			mList, err := m.getMachineList(context.Background())
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(mList.Data.Machines) != tc.nodeCount {
					t.Errorf("unexpected node length, expected %d but got %d", tc.nodeCount, len(mList.Data.Machines))
				}
			}

		})
	}

}

func TestCDIManagerGetAvailableNums(t *testing.T) {
	testCases := []struct {
		name                               string
		nodeCount                          int
		machineUUID                        string
		modelName                          string
		useCapiBmh                         bool
		expectedErr                        bool
		expectedAvailableReservedResources int
	}{
		{
			name:                               "When correctly getting available number of fabric devices in resource pool",
			nodeCount:                          1,
			machineUUID:                        "0001",
			modelName:                          "A100",
			useCapiBmh:                         true,
			expectedErr:                        false,
			expectedAvailableReservedResources: 5,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, server, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			defer server.Close()

			availableResources, err := m.getAvailableNums(context.Background(), tc.machineUUID, tc.modelName)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if availableResources != tc.expectedAvailableReservedResources {
					t.Errorf("unexpected response of available reserved resources, expected %d but got %d", tc.expectedAvailableReservedResources, availableResources)
				}
			}
		})
	}
}

func TestCDIManagerGetNodeGroups(t *testing.T) {
	testCases := []struct {
		name                    string
		nodeCount               int
		useCapiBmh              bool
		expectedErr             bool
		expectedNodeGroupLength int
	}{
		{
			name:                    "When correctly getting node groups",
			nodeCount:               1,
			useCapiBmh:              true,
			expectedErr:             false,
			expectedNodeGroupLength: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, server, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			defer server.Close()

			nodeGroups, err := m.getNodeGroups(context.Background())
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(nodeGroups.NodeGroups) != tc.expectedNodeGroupLength {
					t.Errorf("unexpected node groups length, expected %d but got %d", tc.expectedNodeGroupLength, len(nodeGroups.NodeGroups))
				}
			}
		})
	}
}

func TestCDIManagerGetNodeGroupInfo(t *testing.T) {
	testCases := []struct {
		name                  string
		nodeCount             int
		useCapiBmh            bool
		nodeGroup             client.CMNodeGroup
		machineUUID           string
		expectedErr           bool
		expectedNodeGroupUUID string
	}{
		{
			name:       "When correctly getting node group info",
			nodeCount:  1,
			useCapiBmh: true,
			nodeGroup: client.CMNodeGroup{
				UUID: "0001",
			},
			machineUUID:           "0001",
			expectedErr:           false,
			expectedNodeGroupUUID: "0001",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, server, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			defer server.Close()

			nodeGroupInfo, err := m.getNodeGroupInfo(context.Background(), tc.nodeGroup)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				for _, machineID := range nodeGroupInfo.MachineIDs {
					if machineID == tc.machineUUID {
						if nodeGroupInfo.UUID != tc.expectedNodeGroupUUID {
							t.Errorf("unexpected node group UUID, expected %s, but got %s", tc.expectedNodeGroupUUID, nodeGroupInfo.UUID)
						}
					}
				}
			}
		})
	}
}

func TestCDIManagerGetMinMaxNums(t *testing.T) {
	testCases := []struct {
		name        string
		nodeCount   int
		useCapiBmh  bool
		machineUUID string
		modelName   string
		expectedErr bool
		expectedMin int
		expectedMax int
	}{
		{
			name:        "When correctly getting min/max number of fabric devices",
			nodeCount:   1,
			useCapiBmh:  true,
			machineUUID: "0001",
			modelName:   "A100",
			expectedErr: false,
			expectedMin: 1,
			expectedMax: 3,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, server, stop := createTestManager(t, tc.useCapiBmh, tc.nodeCount)
			defer stop()
			defer server.Close()

			min, max, err := m.getMinMaxNums(context.Background(), tc.machineUUID, tc.modelName)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if min != nil {
					if *min != tc.expectedMin {
						t.Errorf("unexpected min value, expected %d but got %d", tc.expectedMin, *min)
					}
				}
				if max != nil {
					if *max != tc.expectedMax {
						t.Errorf("unexpected max value, expected %d but got %d", tc.expectedMax, *max)
					}
				}
			}
		})
	}
}

func TestGetFabricID(t *testing.T) {
	testCases := []struct {
		name             string
		machineList      *client.FMMachineList
		machineUUID      string
		expectedErr      bool
		expectedFabricID *int
	}{
		{
			name: "When correctly getting fabric ID",
			machineList: &client.FMMachineList{
				Data: client.FMMachines{
					Machines: []client.FMMachine{
						{
							MachineUUID: "0001",
							FabricID:    ptr.To(1),
						},
					},
				},
			},
			machineUUID:      "0001",
			expectedErr:      false,
			expectedFabricID: ptr.To(1),
		},
		{
			name: "When fabric id is nil",
			machineList: &client.FMMachineList{
				Data: client.FMMachines{
					Machines: []client.FMMachine{
						{
							MachineUUID: "0002",
						},
					},
				},
			},
			machineUUID:      "0002",
			expectedErr:      false,
			expectedFabricID: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fabricID := getFabricID(tc.machineList, tc.machineUUID)
			if tc.expectedErr {

			} else if !tc.expectedErr {
				if fabricID != nil && tc.expectedFabricID != nil {
					if *fabricID != *tc.expectedFabricID {
						t.Errorf("unexpected fabric id, expected %d but got %d", *tc.expectedFabricID, *fabricID)

					}
				} else {
					if tc.expectedFabricID != nil {
						t.Errorf("unexpected fabric id, expected %d but got nil", *tc.expectedFabricID)
					}
				}
			}
		})
	}
}
