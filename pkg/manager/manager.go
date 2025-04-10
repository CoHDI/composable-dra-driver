package manager

import (
	"cdi_dra/pkg/client"
	"cdi_dra/pkg/config"
	"cdi_dra/pkg/kube_utils"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/strings/slices"
)

const (
	configMapName = "composable-dra/composable-dra-dds"
)

type CDIManager struct {
	coreClient           kube_client.Interface
	machineClient        dynamic.Interface
	discoveryClient      discovery.DiscoveryInterface
	namedDriverResources map[string]*resourceslice.DriverResources
	deviceInfos          []DeviceInfo
	cdiClient            *client.CDIClient
	kubecontrollers      *kube_utils.KubeControllers
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

type machineInfos struct {
	machines []*machine
}

type machine struct {
	nodeName    string
	machineUUID string
	fabricID    int
	deviceList  deviceList
}

type deviceList struct {
	devices []*device
}

type device struct {
	modelName            string
	k8sDeviceName        string
	driverName           string
	availableDeviceCount int
	minDeviceCount       int
	maxDeviceCount       int
}

func StartCDIManager(ctx context.Context, config *config.Config) error {
	kconfig, err := kube_utils.NewClientConfig()
	if err != nil {
		return err
	}

	coreclient, err := kube_client.NewForConfig(kconfig)
	if err != nil {
		slog.Error("Failed to create core client", "error", err)
		return err
	}

	machineclient, err := dynamic.NewForConfig(kconfig)
	if err != nil {
		slog.Error("Failed to create machine client", "error", err)
		return err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kconfig)
	if err != nil {
		slog.Error("Failed to create discovery client", "error", err)
		return err
	}

	// Create k8s controllers for Nodes, ConfigMap, Secret, Machine and BMH
	kc, err := kube_utils.CreateKubeControllers(coreclient, machineclient, discoveryClient, ctx.Done())
	if err != nil {
		slog.Error("Failed to create kube controllers")
		return err
	}

	// Run k8s controllers
	if err := kc.Run(); err != nil {
		slog.Error("Failed to run kube controllers")
		return err
	}

	// Build client to connect CDI components like FM, IM and CM
	cdiclient, err := client.BuildCDIClient(config, kc)
	if err != nil {
		return err
	}

	// Get DeviceInfo from ConfigMap
	cm, err := kc.GetConfigMap(configMapName)
	if err != nil {
		slog.Error("Cannot get config map for device config", "error", err)
		return err
	}
	var devInfos []DeviceInfo
	if cm != nil {
		devInfos, err = getDeviceInfos(cm)
		if err != nil {
			return err
		}
	}

	// Init DriverResource for every driver name
	ndr := initDriverResources(devInfos)

	m := &CDIManager{
		coreClient:           coreclient,
		machineClient:        machineclient,
		discoveryClient:      discoveryClient,
		namedDriverResources: ndr,
		deviceInfos:          devInfos,
		cdiClient:            cdiclient,
		kubecontrollers:      kc,
	}

	controllers, err := m.startResourceSliceController(ctx)
	if err != nil {
		return err
	}

	wait.Until(func() {
		slog.Info("Loop Start")
		err := m.startCheckResourcePoolLoop(ctx, controllers)
		if err != nil {
			slog.Error("Loop Failed", "error", err)
		}
	}, config.ScanInterval, ctx.Done())
	return nil
}

func (m *CDIManager) startResourceSliceController(ctx context.Context) (map[string]*resourceslice.Controller, error) {
	if !kube_utils.IsDRAEnabled(m.discoveryClient) {
		return nil, fmt.Errorf("not enabled feature gate of Dynamic Resource Allocation")
	}
	controllers := make(map[string]*resourceslice.Controller)
	for driverName, driverResource := range m.namedDriverResources {
		options := resourceslice.Options{
			DriverName: driverName,
			KubeClient: m.coreClient,
			Resources:  driverResource,
		}
		slog.Info("Start publishing ResourceSlices for CDI fabric devices...", "driverName", driverName)
		controller, err := resourceslice.StartController(ctx, options)
		if err != nil {
			slog.Error("error starting resource slice controller", "error", err)
			return nil, err
		}
		controllers[driverName] = controller
	}
	return controllers, nil
}

func (m *CDIManager) startCheckResourcePoolLoop(ctx context.Context, controllers map[string]*resourceslice.Controller) error {
	// Get the map of node name vs machine uuid
	muuids, err := m.getMachineUUIDs()
	if err != nil {
		slog.Error("failed to get machine UUID")
		return err
	}

	// Create machine which have information of node and devices
	var machineInfos machineInfos
	for nodeName, muuid := range muuids {
		machine := &machine{
			nodeName:    nodeName,
			machineUUID: muuid,
		}
		machineInfos.machines = append(machineInfos.machines, machine)
	}

	// Get fabric id of every machine
	for _, machine := range machineInfos.machines {
		fabricID, err := m.getFabricID(machine.machineUUID)
		if err != nil {
			return err
		}
		machine.fabricID = fabricID
	}

	// Get the number of free devices in a fabric pool
	// It is executed per a fabric for reducing API calls
	fabricFound := make(map[int]deviceList)
	for _, machine := range machineInfos.machines {
		if _, exists := fabricFound[machine.fabricID]; exists {
			continue
		}
		var deviceList deviceList
		for _, deviceInfo := range m.deviceInfos {
			availableNum, err := m.getAvailableNums(machine.machineUUID, deviceInfo.CDIModelName)
			if err != nil {
				return err
			}
			device := &device{
				modelName:            deviceInfo.CDIModelName,
				k8sDeviceName:        deviceInfo.K8sDeviceName,
				driverName:           deviceInfo.DriverName,
				availableDeviceCount: availableNum,
			}
			deviceList.devices = append(deviceList.devices, device)
		}
		fabricFound[machine.fabricID] = deviceList
	}

	// Copy device list per a fabric into all machines
	for fabricID, deviceList := range fabricFound {
		for _, machine := range machineInfos.machines {
			if machine.fabricID != fabricID {
				continue
			}
			machine.deviceList = deviceList.DeepCopy()
		}
	}

	// Get the minimum and maximum number of devices in the node group
	// and set them into device of every machine.
	err = m.setMinMaxNums(machineInfos)
	if err != nil {
		return err
	}

	// Print for debug
	for _, machine := range machineInfos.machines {
		fmt.Printf("machineUUID    : %s\n", machine.machineUUID)
		fmt.Printf("fabric id      : %d\n", machine.fabricID)
		for _, device := range machine.deviceList.devices {
			fmt.Printf("device name    : %s\n", device.modelName)
			fmt.Printf("device address : %p\n", device)
			fmt.Printf("available      : %d\n", device.availableDeviceCount)
			fmt.Printf("max            : %d\n", device.maxDeviceCount)
			fmt.Printf("min            : %d\n", device.minDeviceCount)
		}
		fmt.Println("-----")
	}

	// Update ResourceSlice using machineInfos
	err = m.manageCDIResourceSlices(ctx, machineInfos, controllers)
	if err != nil {
		return err
	}
	return nil
}

func (m *CDIManager) getMachineUUIDs() (map[string]string, error) {
	uuids := make(map[string]string)

	providerIDs, err := m.kubecontrollers.ListProviderIDs()
	if err != nil {
		return nil, err
	}
	for _, providerID := range providerIDs {
		nodeName, err := m.kubecontrollers.FindNodeNameByProviderID(providerID)
		if err != nil {
			slog.Error("failed to get node name", "error", err)
			return nil, err
		} else if nodeName == "" {
			slog.Warn("missing node for providerID", "providerID", providerID)
			continue
		}
		uuid, err := m.kubecontrollers.FindMachineUUIDByProviderID(providerID)
		if err != nil {
			slog.Error("failed to get machine uuid", "error", err)
			return nil, err
		} else if uuid == "" {
			slog.Warn("missing machine uuid for providerID", "providerID", providerID)
			continue
		}
		uuids[nodeName] = uuid
	}
	return uuids, nil
}

func (m *CDIManager) getFabricID(muuid string) (int, error) {
	_, err := m.cdiClient.GetFMAvailableReservedResources(muuid)
	if err != nil {
		return 0, err
	}
	// TODO: preliminarily return random fabric number
	fabricID := rand.Intn(2) + 1
	return fabricID, nil
}

func (m *CDIManager) getAvailableNums(muuid string, modelName string) (int, error) {
	// TODO: preliminarily return random available number
	num := rand.Intn(3) + 1
	return num, nil
}

func (m *CDIManager) setMinMaxNums(mInfos machineInfos) error {
	nodeGroups, err := m.cdiClient.GetCMNodeGroups()
	if err != nil {
		return err
	}
	var ngInfos []client.CMNodeGroupInfo
	for _, nodeGroup := range nodeGroups.NodeGroups {
		ngInfo, err := m.cdiClient.GetCMNodeGroupInfo(nodeGroup)
		if err != nil {
			return err
		}
		ngInfos = append(ngInfos, ngInfo)
	}
	for _, machine := range mInfos.machines {
		for _, ngInfo := range ngInfos {
			if slices.Contains(ngInfo.MachineIDs, machine.machineUUID) {
				for _, device := range machine.deviceList.devices {
					for _, resource := range ngInfo.Resources {
						if device.modelName == resource.ModelName {
							device.minDeviceCount = resource.MinResourceCount
							device.maxDeviceCount = resource.MaxResourceCount
						}
					}
				}
			}
		}
	}
	return nil
}

func (m *CDIManager) manageCDIResourceSlices(ctx context.Context, machineInfos machineInfos, controlles map[string]*resourceslice.Controller) error {
	for driverName := range m.namedDriverResources {
		fabricFound := make(map[int]bool)
		for _, machine := range machineInfos.machines {
			if !fabricFound[machine.fabricID] {
				for _, device := range machine.deviceList.devices {
					if device.driverName == driverName {
						poolName := device.k8sDeviceName + "-fabric" + strconv.Itoa(machine.fabricID)
						err := m.updatePool(driverName, poolName, device)
						if err != nil {
							return err
						}
					}
				}
				fabricFound[machine.fabricID] = true
			}
		}
	}
	for driverName, driverResources := range m.namedDriverResources {
		c := controlles[driverName]
		c.Update(driverResources)
	}

	return nil
}

func (m *CDIManager) updatePool(driverName string, poolName string, device *device) error {
	slog.Info("create ResourceSlice", "name", poolName, "driver", driverName)
	return nil
}

func getDeviceInfos(cm *corev1.ConfigMap) ([]DeviceInfo, error) {
	if cm.Data == nil {
		slog.Warn("configmap data is nil")
		return nil, nil
	}
	if devInfoStr, found := cm.Data["device-info"]; !found {
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

func initDriverResources(devInfos []DeviceInfo) map[string]*resourceslice.DriverResources {
	foundDriver := make(map[string]bool)
	result := make(map[string]*resourceslice.DriverResources)
	for _, devInfo := range devInfos {
		if foundDriver[devInfo.DriverName] {
			continue
		} else {
			foundDriver[devInfo.DriverName] = true
		}
		driverResources := &resourceslice.DriverResources{
			Pools: make(map[string]resourceslice.Pool),
		}
		result[devInfo.DriverName] = driverResources
	}
	return result
}

func (in deviceList) DeepCopy() (out deviceList) {
	if in.devices != nil {
		for _, inDevice := range in.devices {
			newDevice := new(device)
			*newDevice = *inDevice
			out.devices = append(out.devices, newDevice)
		}
	}
	return out
}
