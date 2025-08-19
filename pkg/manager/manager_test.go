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

package manager

import (
	"cdi_dra/pkg/client"
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
)

const (
	fabricIdNum         = 3
	nodeGroupNum        = 3
	BCReady             = "FabricDeviceReady"
	BCFailureReschedule = "FabricDeviceReschedule"
	BCFailureFailed     = "FabricDeviceFailed"
)

const (
	CaseDriverResourceCorrect = iota
	CaseDriverResourceEmpty
	CaseDriverResourceFullLength
)

const (
	CaseDeviceCorrect = iota
	CaseDeviceMinMaxNil
)

func init() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
}

func createTestDriverResources(caseDriverResource int) map[string]*resourceslice.DriverResources {
	ndr := make(map[string]*resourceslice.DriverResources)

	switch caseDriverResource {
	case CaseDriverResourceCorrect:
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
									BindsToNode:              ptr.To(true),
									BindingConditions:        []string{BCReady},
									BindingFailureConditions: []string{BCFailureReschedule, BCFailureFailed},
								},
							},
						},
					},
					Generation: 1,
					NodeSelector: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      "cohdi.com/fabric",
										Operator: v1.NodeSelectorOpIn,
										Values: []string{
											"true",
										},
									},
								},
							},
						},
					},
				},
			},
		}
		ndr["test-driver-2"] = &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		}
	case CaseDriverResourceEmpty:
		ndr["test-driver-1"] = &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		}
		ndr["test-driver-2"] = &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		}
	case CaseDriverResourceFullLength:
		ndr[config.FullLengthDriverName] = &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		}
	}
	return ndr
}

func createTestManager(t testing.TB, testSpec config.TestSpec) (*CDIManager, *httptest.Server, ku.TestControllerShutdownFunc) {
	ndr := createTestDriverResources(testSpec.CaseDriverResource)

	server, certPem := client.CreateTLSServer(t)
	server.StartTLS()

	secret := config.CreateSecret(certPem, 1)
	testConfig := &config.TestConfig{
		Spec:   testSpec,
		Secret: secret,
		Nodes:  make([]*v1.Node, config.TestNodeCount),
		BMHs:   make([]*unstructured.Unstructured, config.TestNodeCount),
	}
	for i := 0; i < config.TestNodeCount; i++ {
		testConfig.Nodes[i], testConfig.BMHs[i] = ku.CreateNodeBMHs(i, "test-namespace", testConfig.Spec.UseCapiBmh)
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
		TenantID:    "00000000-0000-0002-0000-000000000000",
		ClusterID:   "00000000-0000-0000-0001-000000000000",
	}
	cdiClient, err := client.BuildCDIClient(cfg, kc)
	if err != nil {
		t.Fatalf("failed to build CDIClient: %v", err)
	}

	deviceInfos := config.CreateDeviceInfos(testSpec.CaseDeviceInfo)

	return &CDIManager{
		coreClient:           kubeclient,
		bmhClient:            dynamicclient,
		discoveryClient:      kubeclient.Discovery(),
		namedDriverResources: ndr,
		cdiClient:            cdiClient,
		kubecontrollers:      kc,
		deviceInfos:          deviceInfos,
		labelPrefix:          "cohdi.com",
		cdiOptions: CDIOptions{
			useCapiBmh: testSpec.UseCapiBmh,
			useCM:      testSpec.UseCM,
		},
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

func createTestMachines(ts config.TestSpec) []*machine {
	var machines []*machine
	for i := 0; i < config.TestNodeCount; i++ {
		nodeGroupUUID := fmt.Sprintf("%d0000000-0000-0000-0000-000000000000", (i/nodeGroupNum)+1)
		machine := &machine{
			nodeName:      "test-node-" + strconv.Itoa(i),
			fabricID:      ptr.To((i % fabricIdNum) + 1),
			nodeGroupUUID: nodeGroupUUID,
		}
		machine.deviceList = createTestDeviceList(ts.AvailableDeviceCount, nodeGroupUUID, ts.CaseDevice)
		machines = append(machines, machine)
	}
	return machines
}

func createTestDeviceList(availableNum int, nodeGroupUUID string, caseDevice int) deviceList {
	devList := make(deviceList)
	switch caseDevice {
	case CaseDeviceCorrect:
		devList = deviceList{
			"DEVICE 1": &device{
				k8sDeviceName: "test-device-1",
				driverName:    "test-driver-1",
				draAttributes: map[string]string{
					"productName": "TEST DEVICE 1",
				},
				availableDeviceCount: availableNum,
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
		if nodeGroupUUID == "10000000-0000-0000-0000-000000000000" {
			for deviceName := range devList {
				devList[deviceName].minDeviceCount = ptr.To(1)
				devList[deviceName].maxDeviceCount = ptr.To(3)
			}
		}
		if nodeGroupUUID == "20000000-0000-0000-0000-000000000000" {
			for deviceName := range devList {
				devList[deviceName].minDeviceCount = ptr.To(2)
				devList[deviceName].maxDeviceCount = ptr.To(6)
			}
		}
		if nodeGroupUUID == "30000000-0000-0000-0000-000000000000" {
			for deviceName := range devList {
				devList[deviceName].minDeviceCount = ptr.To(3)
				devList[deviceName].maxDeviceCount = ptr.To(12)
			}
		}
	// Add a device to check if it is no problem that min/max device count is nil
	case CaseDeviceMinMaxNil:
		devList = deviceList{
			"DEVICE 1": &device{
				k8sDeviceName: "test-device-1",
				driverName:    "test-driver-1",
				draAttributes: map[string]string{
					"productName": "TEST DEVICE 1",
				},
				availableDeviceCount: availableNum,
			},
		}
	}
	return devList
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
	options3 := resourceslice.Options{
		DriverName: config.FullLengthDriverName,
		KubeClient: kubeClitent,
		Resources: &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		},
	}
	controlles[config.FullLengthDriverName], err = resourceslice.StartController(context.Background(), options3)
	if err != nil {
		t.Fatalf("failed to start resourceslice controller: %v", err)
	}

	return controlles
}

func TestCDIManagerStartResourceSliceController(t *testing.T) {
	testCases := []struct {
		name                            string
		caseDriverResource              int
		expectedDriverName              string
		expectedPoolName                string
		expectedDeviceName              string
		expectedProductName             string
		expectedBindingFailureCondition []string
		expectedBindingTimeout          int64
		expectedErr                     bool
	}{
		{
			name:                            "When the controller starts successfully if DRA is enabled",
			caseDriverResource:              CaseDriverResourceCorrect,
			expectedDriverName:              "test-driver-1",
			expectedPoolName:                "test-device-1-fabric1",
			expectedDeviceName:              "test-device-1-gpu1",
			expectedProductName:             "TEST DEVICE 1",
			expectedBindingFailureCondition: []string{"FabricDeviceReschedule", "FabricDeviceFailed"},
			expectedBindingTimeout:          600,
			expectedErr:                     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh:         true,
				DRAenabled:         true,
				CaseDriverResource: tc.caseDriverResource,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()

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
			resourceslices, err := m.coreClient.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
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
							for _, expectedBCFailure := range tc.expectedBindingFailureCondition {
								var found bool
								for _, bcFailure := range device.BindingFailureConditions {
									if bcFailure == expectedBCFailure {
										found = true
									}
								}
								if !found {
									t.Errorf("expected BindingFailureCondition is not found: %s", expectedBCFailure)
								}
							}
						}
					}
				}
			}
			if len(tc.expectedPoolName) > 0 && sliceNumPerPool[tc.expectedPoolName] < 1 {
				t.Errorf("not found expected ResourceSlice in pool, expected pool %s", tc.expectedPoolName)
			}
			if len(tc.expectedDeviceName) > 0 && !deviceFound {
				t.Errorf("not found expected device in ResourceSlice, expected device %s", tc.expectedDeviceName)
			}
		})
	}
}

func TestCheckResourcePoolLoop(t *testing.T) {
	testCases := []struct {
		name                     string
		useCapiBmh               bool
		useCM                    bool
		nodeName                 string
		caseDevInfo              int
		caseDriverResource       int
		deletedAnnotationBmh     []string
		expectedErr              bool
		expectedErrMsg           string
		expectedPoolName         string
		expectedDriverName       string
		expectedDeviceName       string
		expectedAttributes       map[string]string
		expectedAttributeFactors int
		expectedBCFailure        []string
		expectedAvailableDevices int
		expectedResourceSliceNum int
		expectedFabric           string
		expectedMaxDevice        string
		expectedMinDevice        string
	}{
		{
			name:                     "When the loop is done successfully with USE_CM/USE_CAPI_BMH is false",
			useCapiBmh:               false,
			useCM:                    false,
			caseDriverResource:       CaseDriverResourceEmpty,
			nodeName:                 "test-node-0",
			expectedResourceSliceNum: 9,
			expectedPoolName:         "test-device-1-fabric1",
			expectedAvailableDevices: 2,
			expectedFabric:           "1",
			expectedDeviceName:       "test-device-1",
			expectedDriverName:       "test-driver-1",
			expectedAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
			expectedBCFailure: []string{"FabricDeviceReschedule", "FabricDeviceFailed"},
			expectedMaxDevice: "",
			expectedMinDevice: "",
		},
		{
			name:               "When the loop is done successfully with USE_CM/USE_CAPI_BMH is true",
			useCapiBmh:         true,
			useCM:              true,
			caseDriverResource: CaseDriverResourceEmpty,
			nodeName:           "test-node-0",
			expectedDeviceName: "test-device-1",
			expectedFabric:     "1",
			expectedMaxDevice:  "3",
			expectedMinDevice:  "1",
		},
		{
			name:                     "When some BMH have no machine uuid",
			useCapiBmh:               true,
			useCM:                    true,
			caseDriverResource:       CaseDriverResourceEmpty,
			deletedAnnotationBmh:     []string{"test-bmh-0", "test-bmh-3", "test-bmh-6"},
			nodeName:                 "test-node-0",
			expectedDeviceName:       "test-device-2",
			expectedFabric:           "",
			expectedMaxDevice:        "",
			expectedMinDevice:        "",
			expectedResourceSliceNum: 6,
			expectedAvailableDevices: 5,
			expectedPoolName:         "test-device-2-fabric2",
			expectedDriverName:       "test-driver-1",
			expectedAttributes: map[string]string{
				"productName": "TEST DEVICE 2",
			},
			expectedBCFailure: []string{"FabricDeviceReschedule", "FabricDeviceFailed"},
		},
		{
			name:                 "When all BMHs have no machine uuid",
			useCapiBmh:           true,
			useCM:                true,
			caseDriverResource:   CaseDriverResourceEmpty,
			deletedAnnotationBmh: []string{"ALL"},
			expectedErr:          true,
			expectedErrMsg:       "not any machine uuid is found",
		},
		{
			name:               "When cdi-model-name includes symbol",
			useCapiBmh:         false,
			useCM:              false,
			caseDevInfo:        config.CaseDevInfoModelSymbol,
			caseDriverResource: CaseDriverResourceEmpty,
			expectedErr:        true,
			expectedErrMsg:     "FM available reserved resources API failed",
		},
		{
			name:                     "When DeviceInfo has factors with full length name",
			useCapiBmh:               true,
			useCM:                    true,
			nodeName:                 "test-node-8",
			caseDevInfo:              config.CaseDevInfoFullLength,
			caseDriverResource:       CaseDriverResourceFullLength,
			expectedResourceSliceNum: 3,
			expectedPoolName:         "test-device-1-fabric1",
			expectedAvailableDevices: 128,
			expectedFabric:           "3",
			expectedDeviceName:       config.FullLengthDeviceName,
			expectedDriverName:       config.FullLengthDriverName,
			expectedAttributes: map[string]string{
				config.FullLengthAttrKey: config.FullLengthAttrValue,
			},
			expectedAttributeFactors: 32,
			expectedBCFailure:        []string{"FabricDeviceReschedule", "FabricDeviceFailed"},
			expectedMaxDevice:        "12",
			expectedMinDevice:        "3",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh:         tc.useCapiBmh,
				UseCM:              tc.useCM,
				DRAenabled:         true,
				CaseDeviceInfo:     tc.caseDevInfo,
				CaseDriverResource: tc.caseDriverResource,
			}
			m, server, stop := createTestManager(t, testSpec)
			defer server.Close()
			defer stop()

			if len(tc.deletedAnnotationBmh) > 0 {
				var bmhNames []string
				if tc.deletedAnnotationBmh[0] == "ALL" {
					for i := 0; i < 9; i++ {
						bmhNames = append(bmhNames, fmt.Sprintf("test-bmh-%d", i))
					}
				} else {
					bmhNames = tc.deletedAnnotationBmh
				}
				for _, bmhName := range bmhNames {
					bmh, err := m.bmhClient.Resource(ku.GVK_BMH).Namespace("test-namespace").Get(context.Background(), bmhName, metav1.GetOptions{})
					if err != nil {
						t.Fatalf("failed to get BareMetalHost: %v", err)
					}
					bmh = bmh.DeepCopy()
					unstructured.RemoveNestedField(bmh.UnstructuredContent(), "metadata", "annotations", "cluster-manager.cdi.io/machine")
					_, err = m.bmhClient.Resource(ku.GVK_BMH).Namespace("test-namespace").Update(context.Background(), bmh, metav1.UpdateOptions{})
					if err != nil {
						t.Fatalf("failed to update BareMetalHost: %v", err)
					}
				}
			}

			controlles := createTestControllers(t, m.coreClient)

			err := m.startCheckResourcePoolLoop(context.Background(), controlles)

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
				if len(tc.expectedErrMsg) > 0 && !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("unexpected error message, expected %s but got %s", tc.expectedErrMsg, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				time.Sleep(3 * time.Second)
				resourceslices, err := m.coreClient.ResourceV1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Errorf("unexpected error in kube client List")
				}
				if tc.expectedResourceSliceNum != 0 && len(resourceslices.Items) != tc.expectedResourceSliceNum {
					t.Errorf("unexpected ResourceSlice num, expected %d, but got %d", tc.expectedResourceSliceNum, len(resourceslices.Items))
				}
				sliceNumPerPool := make(map[string]int)
				for _, resourceslice := range resourceslices.Items {
					poolName := resourceslice.Spec.Pool.Name
					if len(tc.expectedPoolName) > 0 && poolName == tc.expectedPoolName {
						sliceNumPerPool[poolName]++
						if sliceNumPerPool[poolName] > 1 {
							t.Errorf("more than one slice exists in pool, pool name %s", poolName)
						}
						if len(tc.expectedDriverName) > 0 && resourceslice.Spec.Driver != tc.expectedDriverName {
							t.Error("unexpected driver name in ResourceSlice")
						}
						if len(resourceslice.Spec.Devices) != tc.expectedAvailableDevices {
							t.Errorf("unexpected device num, expected %d but got %d", tc.expectedAvailableDevices, len(resourceslice.Spec.Devices))
						}
						var deviceFound bool
						for _, device := range resourceslice.Spec.Devices {
							if len(tc.expectedDeviceName) > 0 && device.Name == tc.expectedDeviceName+"-gpu0" {
								deviceFound = true
								for expectedKey, expectedValue := range tc.expectedAttributes {
									value := device.Attributes[resourceapi.QualifiedName(expectedKey)]
									if value.StringValue != nil && len(expectedKey) > 0 && *value.StringValue != expectedValue {
										t.Errorf("unexpected ProductName, expected %s but got %s", expectedValue, *value.StringValue)
									}
								}
								if tc.expectedAttributeFactors > 0 {
									if len(device.Attributes) != tc.expectedAttributeFactors {
										t.Errorf("unexpected attributes length, expected %d but got %d", tc.expectedAttributeFactors, len(device.Attributes))
									}
								}
								for _, expectedBCFailure := range tc.expectedBCFailure {
									var found bool
									for _, bcFailure := range device.BindingFailureConditions {
										if bcFailure == expectedBCFailure {
											found = true
										}
									}
									if !found {
										t.Errorf("expected BindingFailureCondition is not found, expected %s", expectedBCFailure)
									}
								}
							}
						}
						if len(tc.expectedDeviceName) > 0 && !deviceFound {
							t.Errorf("expected device is not found, expected %s", tc.expectedDeviceName)
						}
					}
				}
				node, err := m.coreClient.CoreV1().Nodes().Get(context.Background(), tc.nodeName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("not found node, node name: %s", tc.nodeName)
				}
				if node != nil {
					if node.Labels["cohdi.com/fabric"] != tc.expectedFabric {
						t.Errorf("unexpected label of fabric id, expected %s but got %s", tc.expectedFabric, node.Labels["cohdi.com/fabric"])
					}
					maxLabel := fmt.Sprintf("cohdi.com/%s-size-max", tc.expectedDeviceName)
					if node.Labels[maxLabel] != tc.expectedMaxDevice {
						t.Errorf("unexpected label of max device num, expected %s but got %s", tc.expectedMaxDevice, node.Labels[maxLabel])
					}
					minLabel := fmt.Sprintf("cohdi.com/%s-size-min", tc.expectedDeviceName)
					if node.Labels[minLabel] != tc.expectedMinDevice {
						t.Errorf("unexpected label of min device num, expected %s but got %s", tc.expectedMinDevice, node.Labels[minLabel])
					}
				}
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
			name:                "When correct machine uuid is obtained if USE_CAPI_BMH is true",
			nodeCount:           1,
			nodeName:            "test-node-0",
			useCapiBmh:          true,
			expectedErr:         false,
			expectedMachineUUID: "00000000-0000-0000-0000-000000000000",
		},
		{
			name:                "When correct machine uuid is obtained if USE_CAPI_BMH is false",
			nodeCount:           2,
			nodeName:            "test-node-1",
			useCapiBmh:          false,
			expectedErr:         false,
			expectedMachineUUID: "00000000-0000-0000-0000-000000000001",
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
		name              string
		expectedNodeCount int
		expectedErr       bool
	}{
		{
			name:              "When correct machine list is obtained as expected",
			expectedNodeCount: 9,
			expectedErr:       false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: false,
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
				if mList != nil {
					if len(mList.Data.Machines) != tc.expectedNodeCount {
						t.Errorf("unexpected node length, expected %d but got %d", tc.expectedNodeCount, len(mList.Data.Machines))
					}
				}
			}

		})
	}
}

func TestCDIManagerGetAvailableNums(t *testing.T) {
	testCases := []struct {
		name                               string
		machineUUID                        string
		modelName                          string
		expectedErr                        bool
		expectedErrMsg                     string
		expectedAvailableReservedResources int
	}{
		{
			name:                               "When available number of fabric devices are obtained as expected",
			machineUUID:                        "00000000-0000-0000-0000-000000000000",
			modelName:                          "DEVICE 1",
			expectedErr:                        false,
			expectedAvailableReservedResources: 2,
		},
		{
			name:           "When not-existsted device model is specified",
			machineUUID:    "00000000-0000-0000-0000-000000000000",
			modelName:      "DUMMY DEVICE",
			expectedErr:    true,
			expectedErrMsg: "FM available reserved resources API failed",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
			defer stop()
			defer server.Close()

			availableResources, err := m.getAvailableNums(context.Background(), tc.machineUUID, tc.modelName)
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.expectedErrMsg) {
					t.Errorf("unexpected error message, expected %s but got %s", tc.expectedErrMsg, err.Error())
				}
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
		expectedErr             bool
		expectedNodeGroupLength int
	}{
		{
			name:                    "When correct node groups are obtained as expected",
			expectedErr:             false,
			expectedNodeGroupLength: 3,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
			defer stop()
			defer server.Close()

			nodeGroups, err := m.getNodeGroups(context.Background())
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if nodeGroups != nil {
					if len(nodeGroups.NodeGroups) != tc.expectedNodeGroupLength {
						t.Errorf("unexpected node groups length, expected %d but got %d", tc.expectedNodeGroupLength, len(nodeGroups.NodeGroups))
					}
				}
			}
		})
	}
}

func TestCDIManagerGetNodeGroupInfo(t *testing.T) {
	testCases := []struct {
		name                  string
		useCapiBmh            bool
		nodeGroup             client.CMNodeGroup
		machineUUID           string
		expectedErr           bool
		expectedNodeGroupUUID string
	}{
		{
			name:       "When correct node group info is obtained as expected",
			useCapiBmh: true,
			nodeGroup: client.CMNodeGroup{
				UUID: "10000000-0000-0000-0000-000000000000",
			},
			machineUUID:           "00000000-0000-0000-0000-000000000000",
			expectedErr:           false,
			expectedNodeGroupUUID: "10000000-0000-0000-0000-000000000000",
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
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if nodeGroupInfo != nil {
					for _, machineID := range nodeGroupInfo.MachineIDs {
						if machineID == tc.machineUUID {
							if nodeGroupInfo.UUID != tc.expectedNodeGroupUUID {
								t.Errorf("unexpected node group UUID, expected %s, but got %s", tc.expectedNodeGroupUUID, nodeGroupInfo.UUID)
							}
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
		machineUUID string
		modelName   string
		expectedErr bool
		expectedMin int
		expectedMax int
	}{
		{
			name:        "When correct min/max number of fabric devices is obtained as expected",
			machineUUID: "00000000-0000-0000-0000-000000000000",
			modelName:   "DEVICE 1",
			expectedErr: false,
			expectedMin: 1,
			expectedMax: 3,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh: false,
				DRAenabled: true,
			}
			m, server, stop := createTestManager(t, testSpec)
			defer stop()
			defer server.Close()

			min, max, err := m.getMinMaxNums(context.Background(), tc.machineUUID, tc.modelName)
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
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
		name                   string
		availableDeviceCount   []int
		expectedPoolName       string
		expectedDriverName     string
		expectedDeviceName     string
		expectedProductName    string
		expectedBCFailure      []string
		expectedBintindTimeout int64
		expectedUpdated        bool
		expectedGeneration     int
	}{
		{
			name:                   "When ResourceSlice is correctly created and updated",
			availableDeviceCount:   []int{3, 5, 1},
			expectedPoolName:       "test-device-1-fabric2",
			expectedDriverName:     "test-driver-1",
			expectedDeviceName:     "test-device-1-gpu0",
			expectedProductName:    "TEST DEVICE 1",
			expectedBCFailure:      []string{"FabricDeviceReschedule", "FabricDeviceFailed"},
			expectedBintindTimeout: 100,
			expectedUpdated:        true,
			expectedGeneration:     1,
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
				UseCapiBmh:         false,
				DRAenabled:         true,
				CaseDriverResource: CaseDriverResourceCorrect,
				CaseDevice:         CaseDeviceCorrect,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()
			controlles := createTestControllers(t, m.coreClient)
			for i, availableDevice := range tc.availableDeviceCount {
				testSpec.AvailableDeviceCount = availableDevice
				machines := createTestMachines(testSpec)
				m.manageCDIResourceSlices(machines, controlles)
				time.Sleep(time.Second)
				resourceslices, err := m.coreClient.ResourceV1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
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
								for _, expectedBCFailure := range tc.expectedBCFailure {
									var found bool
									for _, bcFailure := range device.BindingFailureConditions {
										if bcFailure == expectedBCFailure {
											found = true
										}
									}
									if !found {
										t.Errorf("expected BindingFailureCondition is not found, expected %s", expectedBCFailure)
									}
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
				nodeGroup := "10000000-0000-0000-0000-000000000000"
				deviceList := createTestDeviceList(availableNum, nodeGroup, testSpec.CaseDevice)
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
			name:          "When correct pool is generated as expected",
			useCapiBmh:    false,
			nodeCount:     1,
			k8sDeviceName: "test-device-1",
			draAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
			availableDeviceCount: 3,
			bindingTimeout:       ptr.To(int64(600)),
			expectedDeviceName:   "test-device-1-gpu0",
			expectedLabel:        "cohdi.com/test-device-1",
		},
		{
			name:          "When BindingTimeout is nil",
			useCapiBmh:    false,
			nodeCount:     1,
			k8sDeviceName: "test-device-1",
			draAttributes: map[string]string{
				"productName": "TEST DEVICE 1",
			},
			availableDeviceCount: 3,
			bindingTimeout:       nil,
			expectedDeviceName:   "test-device-1-gpu0",
			expectedLabel:        "cohdi.com/test-device-1",
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
		caseDevice        int
		maxNumChanged     bool
		expectedFabric    string
		expectedMinDevice string
		expectedMaxDevice []string
		expectedErr       bool
	}{
		{
			name:              "When correctly nodes labeled",
			nodeName:          "test-node-0",
			deviceName:        "test-device-1",
			caseDevice:        CaseDeviceCorrect,
			expectedFabric:    "1",
			expectedMinDevice: "1",
			expectedMaxDevice: []string{"3"},
			expectedErr:       false,
		},
		{
			name:              "When device min and max is nil",
			nodeName:          "test-node-0",
			deviceName:        "test-device-1",
			caseDevice:        CaseDeviceMinMaxNil,
			expectedFabric:    "1",
			expectedMaxDevice: []string{""},
			expectedMinDevice: "",
			expectedErr:       false,
		},
		{
			name:              "When max device num is changed",
			nodeName:          "test-node-0",
			deviceName:        "test-device-1",
			caseDevice:        CaseDeviceCorrect,
			maxNumChanged:     true,
			expectedFabric:    "1",
			expectedMaxDevice: []string{"3", "5"},
			expectedMinDevice: "1",
			expectedErr:       false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testSpec := config.TestSpec{
				UseCapiBmh:           false,
				UseCM:                true,
				DRAenabled:           true,
				AvailableDeviceCount: 3,
				CaseDevice:           tc.caseDevice,
			}
			m, _, stop := createTestManager(t, testSpec)
			defer stop()

			count := 1
			if tc.maxNumChanged {
				count++
			}
			for i := 0; i < count; i++ {
				machines := createTestMachines(testSpec)
				if i > 0 && tc.maxNumChanged {
					for _, machine := range machines {
						if machine.nodeName == tc.nodeName {
							for _, device := range machine.deviceList {
								if device.k8sDeviceName == tc.deviceName {
									if device.maxDeviceCount != nil {
										*device.maxDeviceCount += i * 2
									}
								}
							}
						}
					}
				}
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
							t.Errorf("unexpected label of fabric id, expected %s but got %s", tc.expectedFabric, node.Labels["cohdi.com/fabric"])
						}
						maxLabel := fmt.Sprintf("cohdi.com/%s-size-max", tc.deviceName)
						if node.Labels[maxLabel] != tc.expectedMaxDevice[i] {
							t.Errorf("unexpected label of max device num, expected %s but got %s", tc.expectedMaxDevice, node.Labels[maxLabel])
						}
						minLabel := fmt.Sprintf("cohdi.com/%s-size-min", tc.deviceName)
						if node.Labels[minLabel] != tc.expectedMinDevice {
							t.Errorf("unexpected label of min device num, expected %s but got %s", tc.expectedMinDevice, node.Labels[minLabel])
						}
					}
				}
			}
		})
	}
}

func TestInitDrvierResources(t *testing.T) {
	testCases := []struct {
		name                string
		caseDevInfo         int
		expectedDriverNames []string
		expectedDRLength    int
		expectedDR          *resourceslice.DriverResources
	}{
		{
			name:                "When correct DeviceInfo is provided and DriverResource is initialized as expected",
			caseDevInfo:         config.CaseDevInfoCorrect,
			expectedDriverNames: []string{"test-driver-1", "test-driver-2"},
			expectedDRLength:    2,
			expectedDR: &resourceslice.DriverResources{
				Pools: make(map[string]resourceslice.Pool),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			deviceInfos := config.CreateDeviceInfos(tc.caseDevInfo)
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
		expectedFabricID *int
	}{
		{
			name: "When correct fabric ID is obtained as expected",
			machineList: &client.FMMachineList{
				Data: client.FMMachines{
					Machines: []client.FMMachine{
						{
							MachineUUID: "00000000-0000-0000-0000-000000000001",
							FabricID:    ptr.To(1),
						},
					},
				},
			},
			machineUUID:      "00000000-0000-0000-0000-000000000001",
			expectedFabricID: ptr.To(1),
		},
		{
			name: "When fabric id is nil",
			machineList: &client.FMMachineList{
				Data: client.FMMachines{
					Machines: []client.FMMachine{
						{
							MachineUUID: "00000000-0000-0000-0000-000000000002",
						},
					},
				},
			},
			machineUUID:      "00000000-0000-0000-0000-000000000002",
			expectedFabricID: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fabricID := getFabricID(tc.machineList, tc.machineUUID)
			if fabricID != nil && tc.expectedFabricID != nil {
				if *fabricID != *tc.expectedFabricID {
					t.Errorf("unexpected fabric id, expected %d but got %d", *tc.expectedFabricID, *fabricID)
				}
			} else {
				if tc.expectedFabricID != nil {
					t.Errorf("unexpected fabric id, expected %d but got nil", *tc.expectedFabricID)
				}
			}
		})
	}
}
