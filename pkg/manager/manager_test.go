package manager

import (
	"cdi_dra/pkg/client"
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
)

const (
	fabricIdNum  = 3
	nodeGroupNum = 3
)

func createDeviceInfos() []config.DeviceInfo {
	devInfo1 := config.DeviceInfo{
		Index:        1,
		CDIModelName: "A100 40G",
		DRAAttributes: map[string]string{
			"productName": "NVIDIA A100 40GB PCIe",
		},
		DriverName:        "gpu.nvidia.com",
		K8sDeviceName:     "nvidia-a100-40g",
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

	ndr["test-driver-1"] = &resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			"test-device-1-fabric1": {
				Slices: []resourceslice.Slice{
					{
						Devices: []resourceapi.Device{
							{
								Name: "test-device-1-gpu1",
								Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
									"productName": {
										StringValue: ptr.To("TEST DEVICE 1"),
									},
								},
							},
						},
					},
				},
				Generation: 1,
			},
		},
	}
	ndr["test-driver-2"] = &resourceslice.DriverResources{
		Pools: make(map[string]resourceslice.Pool),
	}
	return ndr
}

func createTestManager(t testing.TB, testSpec config.TestSpec) (*CDIManager, *httptest.Server, ku.TestControllerShutdownFunc) {
	ndr := createTestDriverResources()

	server, certPem := client.CreateTLSServer(t)
	server.StartTLS()

	secret := config.CreateSecret(certPem)
	testConfig := &config.TestConfig{
		Spec:     testSpec,
		Secret:   secret,
		Nodes:    make([]*v1.Node, config.TestNodeCount),
		Machines: make([]*unstructured.Unstructured, config.TestNodeCount),
		BMHs:     make([]*unstructured.Unstructured, config.TestNodeCount),
	}
	for i := 0; i < config.TestNodeCount; i++ {
		testConfig.Nodes[i], testConfig.BMHs[i], testConfig.Machines[i] = ku.CreateNodeBMHMachines(i, "test-namespace", testConfig.Spec.UseCapiBmh)
	}

	kubeclient, dynamicclient := ku.CreateTestClient(t, testConfig)
	kubeclient.PrependReactor("create", "resourceslices", createResourceSliceCreateReactor())
	kc, stop := ku.CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)

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
		coreClient:           kubeclient,
		discoveryClient:      kubeclient.Discovery(),
		namedDriverResources: ndr,
		cdiClient:            cdiClient,
		kubecontrollers:      kc,
		useCapiBmh:           testSpec.UseCapiBmh,
		labelPrefix:          "cohdi.com",
	}, server, stop

}

func createResourceSliceCreateReactor() func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
	nameCounter := 0
	var mutex sync.Mutex
	return func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		mutex.Lock()
		defer mutex.Unlock()
		resourceslice := action.(k8stesting.CreateAction).GetObject().(*resourceapi.ResourceSlice)
		if resourceslice.Name == "" && resourceslice.GenerateName != "" {
			resourceslice.Name = fmt.Sprintf("%s%d", resourceslice.GenerateName, nameCounter)
		}
		nameCounter++
		return false, nil, nil
	}
}

func createTestMachines(availableDeviceCount int) []*machine {
	var machines []*machine
	for i := 0; i < config.TestNodeCount; i++ {
		machine := &machine{
			nodeName:      "test-node-" + strconv.Itoa(i),
			fabricID:      ptr.To((i % fabricIdNum) + 1),
			nodeGroupUUID: strconv.Itoa((i / nodeGroupNum) + 1),
		}
		machine.deviceList = createTestDeviceList(availableDeviceCount)
		machines = append(machines, machine)
	}
	return machines
}

func createTestDeviceList(availableNum int) deviceList {
	deviceList := deviceList{
		"DEVICE 1": &device{
			k8sDeviceName: "test-device-1",
			driverName:    "test-driver-1",
			draAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
			availableDeviceCount: availableNum,
			minDeviceCount:       ptr.To(1),
			maxDeviceCount:       ptr.To(6),
		},
		"DEVICE 2": &device{
			k8sDeviceName:        "test-device-2",
			driverName:           "test-driver-1",
			availableDeviceCount: availableNum,
		},
		"DEVICE 3": &device{
			k8sDeviceName: "test-device-3",
			driverName:    "test-driver-2",
			draAttributes: map[string]string{
				"productName": "TEST DEVICE 3",
			},
			availableDeviceCount: availableNum,
		},
	}
	return deviceList
}

func createTestControllers(t testing.TB, kubeClitent kubernetes.Interface) map[string]*resourceslice.Controller {
	var err error
	controlles := make(map[string]*resourceslice.Controller)
	options1 := resourceslice.Options{
		DriverName: "test-driver-1",
		KubeClient: kubeClitent,
		Resources: &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		},
	}
	controlles["test-driver-1"], err = resourceslice.StartController(context.Background(), options1)
	if err != nil {
		t.Fatalf("failed to start resourceslice controller: %v", err)
	}
	options2 := resourceslice.Options{
		DriverName: "test-driver-2",
		KubeClient: kubeClitent,
		Resources: &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		},
	}
	controlles["test-driver-2"], err = resourceslice.StartController(context.Background(), options2)
	if err != nil {
		t.Fatalf("failed to start resourceslice controller: %v", err)
	}

	return controlles
}

func TestCDIManagerStartResourceSliceController(t *testing.T) {
	testSpec := config.TestSpec{
		UseCapiBmh: true,
		DRAenabled: true,
	}
	m, _, stop := createTestManager(t, testSpec)
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
			expectedDriverName:  "test-driver-1",
			expectedPoolName:    "test-device-1-fabric1",
			expectedDeviceName:  "test-device-1-gpu1",
			expectedProductName: "TEST DEVICE 1",
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
			var deviceFound bool
			sliceNumPerPool := make(map[string]int)
			for _, resourceslice := range resourceslices.Items {
				poolName := resourceslice.Spec.Pool.Name
				if poolName == tc.expectedPoolName {
					sliceNumPerPool[poolName]++
					if sliceNumPerPool[poolName] > 1 {
						t.Errorf("more than one sliece exist per pool, pool name: %s", poolName)
					}
					if resourceslice.Spec.Driver != tc.expectedDriverName {
						t.Errorf("unexpected driver name, expected %s but got %s", tc.expectedDriverName, resourceslice.Spec.Driver)
					}
					for _, device := range resourceslice.Spec.Devices {
						if device.Name == tc.expectedDeviceName {
							deviceFound = true
							if *device.Attributes["productName"].StringValue != tc.expectedProductName {
								t.Errorf("unexpected attributes of productName, expected %s but got %s", tc.expectedProductName, *device.Attributes["productName"].StringValue)
							}
						}
					}
				}
			}
			if sliceNumPerPool[tc.expectedPoolName] < 1 {
				t.Errorf("not found expected ResourceSlice in pool, expected pool %s", tc.expectedPoolName)
			}
			if !deviceFound {
				t.Errorf("not found expected device in ResourceSlice, expected device %s", tc.expectedDeviceName)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, _, stop := createTestManager(t, testSpec)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
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
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
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

func TestCDIManagerManageCDIResourceSlices(t *testing.T) {
	testCases := []struct {
		name                 string
		availableDeviceCount []int
		expectedPoolName     string
		expectedDriverName   string
		expectedDeviceName   string
		expectedProductName  string
		expectedUpdated      bool
		expectedGeneration   int
	}{
		{
			name:                 "When ResourceSlice is correctly created and updated",
			availableDeviceCount: []int{3, 5, 1},
			expectedPoolName:     "test-device-1-fabric2",
			expectedDriverName:   "test-driver-1",
			expectedDeviceName:   "test-device-1-gpu0",
			expectedProductName:  "TEST DEVICE 1",
			expectedUpdated:      true,
			expectedGeneration:   1,
		},
		{
			name:                 "When ResourceSlice is not updated",
			availableDeviceCount: []int{3, 3, 3},
			expectedPoolName:     "test-device-1-fabric2",
			expectedDriverName:   "test-driver-1",
			expectedDeviceName:   "test-device-1-gpu2",
			expectedProductName:  "TEST DEVICE 1",
			expectedUpdated:      false,
			expectedGeneration:   1,
		},
		{
			name:                 "When available device count is zero",
			availableDeviceCount: []int{0, 0, 0},
			expectedPoolName:     "test-device-1-fabric2",
			expectedDriverName:   "test-driver-1",
			expectedUpdated:      false,
			expectedGeneration:   1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: false,
				DRAenabled: true,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()
			controlles := createTestControllers(t, m.coreClient)
			for i, availableDevice := range tc.availableDeviceCount {
				machines := createTestMachines(availableDevice)
				m.manageCDIResourceSlices(machines, controlles)
				time.Sleep(time.Second)
				resourceslices, err := m.coreClient.ResourceV1beta2().ResourceSlices().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Errorf("unexpected error in kube client List")
				}

				if len(resourceslices.Items) != len(machines[0].deviceList)*fabricIdNum {
					t.Errorf("unexpected ResourceSlice num, expected 9, but got %d", len(resourceslices.Items))
				}
				sliceNumPerPool := make(map[string]int)
				var deviceFound bool
				for _, resourceslice := range resourceslices.Items {
					poolName := resourceslice.Spec.Pool.Name
					if poolName == tc.expectedPoolName {
						sliceNumPerPool[poolName]++
						if sliceNumPerPool[poolName] > 1 {
							t.Errorf("more than one slice exists in pool, pool name %s", poolName)
						}
						if resourceslice.Spec.Driver != tc.expectedDriverName {
							t.Error("unexpected driver name in ResourceSlice")
						}
						if len(resourceslice.Spec.Devices) != availableDevice {
							t.Errorf("unexpected device num, expected %d but got %d", availableDevice, len(resourceslice.Spec.Devices))
						}
						for _, device := range resourceslice.Spec.Devices {
							if device.Name == tc.expectedDeviceName {
								deviceFound = true
								productName := device.Attributes["productName"]
								if productName.StringValue != nil && *productName.StringValue != tc.expectedProductName {
									t.Errorf("unexpected ProductName, expected %s but got %s", tc.expectedProductName, *productName.StringValue)
								}
							}
						}
						if tc.expectedUpdated {
							if resourceslice.Spec.Pool.Generation != int64(tc.expectedGeneration+i) {
								t.Errorf("unexpected generation of pool %s, expected generation %d, but got %d", tc.expectedPoolName, tc.expectedGeneration+i, resourceslice.Spec.Pool.Generation)
							}
						} else {
							if resourceslice.Spec.Pool.Generation != int64(tc.expectedGeneration) {
								t.Errorf("expected generation not updated but done, pool %s generation %d", poolName, resourceslice.Spec.Pool.Generation)
							}
						}
					}
				}
				if sliceNumPerPool[tc.expectedPoolName] < 1 {
					t.Errorf("not found ResourceSlice in expected pool, expected pool name %s", tc.expectedPoolName)
				}
				if availableDevice != 0 && !deviceFound {
					t.Errorf("not found expected device, expected device name %s", tc.expectedDeviceName)
				}
			}
		})
	}
}

func TestCDIManagerUpdatePool(t *testing.T) {
	testCases := []struct {
		name                 string
		availableDeviceCount []int
		fabricID             int
		expectedUpdated      bool
		expectedGeneration   int64
	}{
		{
			name:                 "When pool is correctly updated",
			availableDeviceCount: []int{2, 5, 0},
			fabricID:             1,
			expectedUpdated:      true,
			expectedGeneration:   2,
		},
		{
			name:                 "When pool is newly created",
			availableDeviceCount: []int{2},
			fabricID:             2,
			expectedUpdated:      true,
			expectedGeneration:   1,
		},
		{
			name:                 "When pool is not updated",
			availableDeviceCount: []int{1},
			fabricID:             1,
			expectedUpdated:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: false,
				DRAenabled: true,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()
			for i, availableNum := range tc.availableDeviceCount {
				deviceList := createTestDeviceList(availableNum)
				device := deviceList["DEVICE 1"]
				poolName := device.k8sDeviceName + "-fabric" + strconv.Itoa(tc.fabricID)
				var updated bool
				if _, exist := m.namedDriverResources[device.driverName]; exist {
					updated = m.updatePool(device.driverName, poolName, device, tc.fabricID)
				}
				if tc.expectedUpdated {
					if !updated {
						t.Errorf("expected pool is updated but not")
					}
					pool := m.namedDriverResources[device.driverName].Pools[poolName]
					if pool.Generation != tc.expectedGeneration+int64(i) {
						t.Errorf("unexpected generation of the pool(%s), expected %d but got %d", poolName, tc.expectedGeneration+int64(i), pool.Generation)
					}
				} else if !tc.expectedUpdated {
					if updated {
						t.Errorf("expected pool is not updated but done")
					}
				}
			}
		})
	}
}

func TestCDIManagerGeneratePool(t *testing.T) {
	testCases := []struct {
		name                 string
		useCapiBmh           bool
		nodeCount            int
		k8sDeviceName        string
		draAttributes        map[string]string
		availableDeviceCount int
		bindingTimeout       *int64
		expectedDeviceName   string
		expectedLabel        string
	}{
		{
			name:          "When correctly provided device",
			useCapiBmh:    false,
			nodeCount:     1,
			k8sDeviceName: "nvidia-a100-40g",
			draAttributes: map[string]string{
				"productName": "NVIDIA A100 40GB PCIe",
			},
			availableDeviceCount: 3,
			bindingTimeout:       ptr.To(int64(600)),
			expectedDeviceName:   "nvidia-a100-40g-gpu0",
			expectedLabel:        "cohdi.com/nvidia-a100-40g",
		},
		{
			name:          "When bindingTimeout is nil",
			useCapiBmh:    false,
			nodeCount:     1,
			k8sDeviceName: "nvidia-a100-40g",
			draAttributes: map[string]string{
				"productName": "NVIDIA A100 40GB PCIe",
			},
			availableDeviceCount: 3,
			bindingTimeout:       nil,
			expectedDeviceName:   "nvidia-a100-40g-gpu0",
			expectedLabel:        "cohdi.com/nvidia-a100-40g",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: tc.useCapiBmh,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
			defer stop()
			defer server.Close()

			device := &device{
				k8sDeviceName:        tc.k8sDeviceName,
				draAttributes:        tc.draAttributes,
				availableDeviceCount: tc.availableDeviceCount,
				bindingTimeout:       tc.bindingTimeout,
			}
			fabricID := 1
			generation := 0
			pool := m.generatePool(device, fabricID, int64(generation))

			if len(pool.Slices[0].Devices) > 0 {
				devices := pool.Slices[0].Devices
				if devices[0].Name != tc.expectedDeviceName {
					t.Errorf("unexpected device name in generated pool, expected %s but got %s", tc.expectedDeviceName, devices[0].Name)
				}
				foundLabel := false
				for _, selctorTerms := range pool.NodeSelector.NodeSelectorTerms {
					for _, exprs := range selctorTerms.MatchExpressions {
						if exprs.Key == tc.expectedLabel {
							foundLabel = true
						}
					}
				}
				if !foundLabel {
					t.Errorf("expected nodeSelector is not set, expected label: %s", tc.expectedLabel)
				}
			}
		})
	}
}

func TestCDIManagerManageCDINodeLabel(t *testing.T) {
	testCases := []struct {
		name              string
		nodeName          string
		deviceName        string
		expectedFabric    string
		expectedMaxDevice string
		expectedMinDevice string
		expectedErr       bool
	}{
		{
			name:              "When correctly nodes labeled",
			nodeName:          "test-node-0",
			deviceName:        "test-device-1",
			expectedFabric:    "1",
			expectedMaxDevice: "6",
			expectedMinDevice: "1",
			expectedErr:       false,
		},
		{
			name:              "When device min and max is nil",
			nodeName:          "test-node-0",
			deviceName:        "test-device-2",
			expectedFabric:    "1",
			expectedMaxDevice: "",
			expectedMinDevice: "",
			expectedErr:       false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: false,
				DRAenabled: true,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()
			availableDevice := 3
			machines := createTestMachines(availableDevice)

			err := m.manageCDINodeLabel(context.Background(), machines)

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				node, err := m.coreClient.CoreV1().Nodes().Get(context.Background(), tc.nodeName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("not found node, node name: %s", tc.nodeName)
				}
				if node != nil {
					if node.Labels["cohdi.com/fabric"] != tc.expectedFabric {
						t.Errorf("unexpected label of fabric id, expected %s but got %s", node.Labels["cohdi.com/fabric"], tc.expectedFabric)
					}
					maxLabel := fmt.Sprintf("cohdi.com/%s-size-max", tc.deviceName)
					if node.Labels[maxLabel] != tc.expectedMaxDevice {
						t.Errorf("unexpected label of max device num, expected %s but got %s", tc.expectedMaxDevice, node.Labels[maxLabel])
					}
					minLabel := fmt.Sprintf("cohdi.com/%s-size-min", tc.deviceName)
					if node.Labels[minLabel] != tc.expectedMinDevice {
						t.Errorf("unexpected label of min device num, expected %s but got %s", tc.expectedMinDevice, node.Labels[minLabel])
					}
				}
			}
		})
	}
}

func TestInitDrvierResources(t *testing.T) {
	deviceInfos := createDeviceInfos()

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
			ndr := initDriverResources(deviceInfos)
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
