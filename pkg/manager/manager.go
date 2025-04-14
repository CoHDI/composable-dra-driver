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

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	"k8s.io/utils/strings/slices"
)

const (
	configMapName  = "composable-dra/composable-dra-dds"
	labelPrefixKey = "label-prefix"
	GpuDeviceType  = "gpu"
)

type CDIManager struct {
	coreClient           kube_client.Interface
	machineClient        dynamic.Interface
	discoveryClient      discovery.DiscoveryInterface
	namedDriverResources map[string]*resourceslice.DriverResources
	deviceInfos          []config.DeviceInfo
	labelPrefix          string
	cdiClient            *client.CDIClient
	kubecontrollers      *kube_utils.KubeControllers
}

type machine struct {
	nodeName    string
	machineUUID string
	fabricID    int
	deviceList  deviceList
}

type deviceList map[string]*device

type device struct {
	k8sDeviceName        string
	driverName           string
	draAttributes        map[string]string
	availableDeviceCount int
	minDeviceCount       int
	maxDeviceCount       int
}

func StartCDIManager(ctx context.Context, cfg *config.Config) error {
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
	cdiclient, err := client.BuildCDIClient(cfg, kc)
	if err != nil {
		return err
	}

	// Get DeviceInfo from ConfigMap
	cm, err := kc.GetConfigMap(configMapName)
	if err != nil {
		slog.Error("Cannot get config map for device config", "error", err)
		return err
	}
	var devInfos []config.DeviceInfo
	var labelPrefix string
	if cm != nil {
		devInfos, err = config.GetDeviceInfos(cm)
		if err != nil {
			return err
		}
		labelPrefix, err = getLabelPrefix(cm)
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
		labelPrefix:          labelPrefix,
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
		} else {
			slog.Info("Loop Successful")
		}
	}, cfg.ScanInterval, ctx.Done())
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
	// Get list of machine
	mList, err := m.getMachineList()
	if err != nil {
		return err
	}

	// Create machine which have information of node and devices
	var machines []*machine
	for nodeName, muuid := range muuids {
		// Get fabric id of every machine
		fabricID, found := getFabricID(mList, muuid)
		if !found {
			slog.Warn("not found fabric id for the machine", "machineUUID", muuid, "nodeName", nodeName)
			continue
		}
		machine := &machine{
			nodeName:    nodeName,
			machineUUID: muuid,
			fabricID:    fabricID,
		}
		machines = append(machines, machine)
	}

	// Get the number of free devices in a fabric pool
	// It is executed per a fabric for reducing API calls
	fabricFound := make(map[int]deviceList)
	for _, machine := range machines {
		if _, exists := fabricFound[machine.fabricID]; exists {
			continue
		}
		var deviceList deviceList = make(map[string]*device)
		for _, deviceInfo := range m.deviceInfos {
			availableNum, err := m.getAvailableNums(machine.machineUUID, deviceInfo.CDIModelName)
			if err != nil {
				return err
			}
			deviceList[deviceInfo.CDIModelName] = &device{
				k8sDeviceName:        deviceInfo.K8sDeviceName,
				driverName:           deviceInfo.DriverName,
				draAttributes:        deviceInfo.DRAAttributes,
				availableDeviceCount: availableNum,
			}
		}
		fabricFound[machine.fabricID] = deviceList
	}

	// Copy device list per a fabric into all machines
	for fabricID, deviceList := range fabricFound {
		for _, machine := range machines {
			if machine.fabricID != fabricID {
				continue
			}
			machine.deviceList = deviceList.DeepCopy()
		}
	}

	// Get the minimum and maximum number of devices in the node group
	// and set them into device of every machine.
	err = m.setMinMaxNums(machines)
	if err != nil {
		return err
	}

	for _, machine := range machines {
		slog.Debug("machine information", "nodeName", machine.nodeName, "machineUUID", machine.machineUUID, "fabricID", machine.fabricID)
		for modelName, device := range machine.deviceList {
			slog.Debug("device information", "nodeName", machine.nodeName, "modelName", modelName, "available", device.availableDeviceCount, "min", device.minDeviceCount, "max", device.maxDeviceCount)
		}
	}

	// Update ResourceSlice using machineInfos
	err = m.manageCDIResourceSlices(machines, controllers)
	if err != nil {
		return err
	}

	// Add labels to Node
	err = m.manageCDINodeLabel(ctx, machines)
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

func (m *CDIManager) getMachineList() (*client.FMMachineList, error) {
	mList, err := m.cdiClient.GetFMMachineList()
	if err != nil {
		return nil, err
	}
	return mList, nil
}

func (m *CDIManager) getAvailableNums(muuid string, modelName string) (int, error) {
	_, err := m.cdiClient.GetFMAvailableReservedResources(muuid)
	if err != nil {
		return 0, err
	}
	// TODO: preliminarily return random available number
	num := rand.Intn(2) + 1
	// num := 1
	return num, nil
}

func (m *CDIManager) setMinMaxNums(machines []*machine) error {
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
	for _, machine := range machines {
		for _, ngInfo := range ngInfos {
			if slices.Contains(ngInfo.MachineIDs, machine.machineUUID) {
				for _, resource := range ngInfo.Resources {
					device := machine.deviceList[resource.ModelName]
					device.minDeviceCount = resource.MinResourceCount
					device.maxDeviceCount = resource.MaxResourceCount
				}
			}
		}
	}
	return nil
}

func (m *CDIManager) manageCDIResourceSlices(machines []*machine, controlles map[string]*resourceslice.Controller) error {
	needUpdate := make(map[string]bool)
	for driverName := range m.namedDriverResources {
		fabricFound := make(map[int]bool)
		for _, machine := range machines {
			if !fabricFound[machine.fabricID] {
				for _, device := range machine.deviceList {
					if device.driverName == driverName {
						poolName := device.k8sDeviceName + "-fabric" + strconv.Itoa(machine.fabricID)
						updated := m.updatePool(driverName, poolName, device, machine.fabricID)
						if updated {
							needUpdate[driverName] = true
						}
					}
				}
				fabricFound[machine.fabricID] = true
			}
		}
	}
	for driverName, driverResources := range m.namedDriverResources {
		if needUpdate[driverName] {
			for poolName, pool := range driverResources.Pools {
				slog.Info("pool update to renew ResourceSlice", "poolName", poolName, "generation", pool.Generation)
			}
			c := controlles[driverName]
			c.Update(driverResources)
		}
	}

	return nil
}

func (m *CDIManager) updatePool(driverName string, poolName string, device *device, fabricID int) (updated bool) {
	// m.namedDriverResources[driverName].Pools[poolName] = generatePool(device, fabricID)
	var generation int64 = 1
	pool := m.namedDriverResources[driverName].Pools[poolName]
	if len(pool.Slices) == 0 {
		m.namedDriverResources[driverName].Pools[poolName] = generatePool(device, fabricID, generation)
		return true
	} else {
		if len(pool.Slices[0].Devices) != device.availableDeviceCount {
			generation = pool.Generation
			generation++
			m.namedDriverResources[driverName].Pools[poolName] = generatePool(device, fabricID, generation)
			return true
		}
	}
	return false
}

func (m *CDIManager) manageCDINodeLabel(ctx context.Context, machines []*machine) error {
	slog.Info("set labels to node")
	for _, machine := range machines {
		node, err := m.kubecontrollers.GetNode(machine.nodeName)
		if err != nil {
			slog.Error("failed to get node", "nodeName", machine.nodeName)
			return err
		}
		// Label for fabric
		fabricLabelKey := m.labelPrefix + "/" + "fabric"
		node.Labels[fabricLabelKey] = strconv.Itoa(machine.fabricID)
		slog.Debug("set labels for fabric", "nodeName", machine.nodeName, "label", fabricLabelKey+"="+strconv.Itoa(machine.fabricID))
		// Label for the min and max number of devices
		for _, device := range machine.deviceList {
			maxLabelKey := m.labelPrefix + "/" + device.k8sDeviceName + "-size-max"
			max := strconv.Itoa(device.maxDeviceCount)
			node.Labels[maxLabelKey] = max

			minLabelKey := m.labelPrefix + "/" + device.k8sDeviceName + "-size-min"
			min := strconv.Itoa(device.minDeviceCount)
			node.Labels[minLabelKey] = min

			slog.Debug("set labels for min and max of devices", "nodeName", machine.nodeName, "label", minLabelKey+"="+min, "label", maxLabelKey+"="+max)
		}

		_, err = m.coreClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			slog.Error("failed to update node label", "nodeName", machine.nodeName)
		}
	}

	return nil
}

func getLabelPrefix(cm *corev1.ConfigMap) (string, error) {
	if cm.Data == nil {
		slog.Warn("configmap data is nil")
		return "", nil
	}
	if labelPrefix, found := cm.Data[labelPrefixKey]; !found {
		slog.Warn("configmap label-prefix is nil")
		return "", nil
	} else {
		return labelPrefix, nil
	}
}

func initDriverResources(devInfos []config.DeviceInfo) map[string]*resourceslice.DriverResources {
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

func getFabricID(mList *client.FMMachineList, muuid string) (fabricID int, found bool) {
	for _, machine := range mList.Data.Machines {
		if machine.MachineUUID == muuid {
			return machine.FabricID, true
		} else {
			return fabricID, false
		}
	}
	// TODO: preliminarily return random fabric number
	// fabricID := rand.Intn(2) + 1
	fabricID = 1
	return fabricID, true
}

func generatePool(device *device, fabricID int, generation int64) resourceslice.Pool {
	var devices []resourceapi.Device
	for i := 0; i < device.availableDeviceCount; i++ {
		d := resourceapi.Device{
			Name: fmt.Sprintf("%s-gpu%d", device.k8sDeviceName, i),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"type": {
						StringValue: ptr.To(GpuDeviceType),
					},
				},
			},
		}
		for key, value := range device.draAttributes {
			d.Basic.Attributes[resourceapi.QualifiedName(key)] = resourceapi.DeviceAttribute{StringValue: ptr.To(value)}
		}
		devices = append(devices, d)
	}
	pool := resourceslice.Pool{
		NodeSelector: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      device.k8sDeviceName,
							Operator: corev1.NodeSelectorOpIn,
							Values: []string{
								"true",
							},
						},
						{
							Key:      "fabric",
							Operator: corev1.NodeSelectorOpIn,
							Values: []string{
								strconv.Itoa(fabricID),
							},
						},
					},
				},
			},
		},
		Slices: []resourceslice.Slice{
			{
				Devices: devices,
			},
		},
		Generation: generation,
	}
	return pool
}

func (in deviceList) DeepCopy() (out deviceList) {
	out = make(map[string]*device)
	for modelName, inDevice := range in {
		newDevice := new(device)
		*newDevice = *inDevice
		out[modelName] = newDevice
	}
	return out
}
