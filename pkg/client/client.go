package client

import (
	"cdi_dra/pkg/config"
	"cdi_dra/pkg/kube_utils"
	"net/http"

	"golang.org/x/oauth2"
)

type CDIClient struct {
	Host        string
	TenantId    string
	Client      *http.Client
	TokenSource oauth2.TokenSource
}

type FMMachineList struct {
	Data Machines `json:"data"`
}

type Machines struct {
	Machines []Machine `json:"machines"`
}

type Machine struct {
	FabricUUID   string            `json:"fabric_uuid"`
	FabricID     int               `json:"fabric_id"`
	MachineUUID  string            `json:"mach_uuid"`
	MachineID    int               `json:"mach_id"`
	MachineName  string            `json:"mach_name"`
	MachineOwner string            `json:"mach_owner"`
	Resources    []MachineResource `json:"resources"`
}

type MachineResource struct {
	ResourceUUID     string `json:"res_uuid"`
	ResourceName     string `json:"res_name"`
	ResourceType     string `json:"res_type"`
	ResourceStatus   int    `json:"res_status"`
	ResourceOPStatus string `json:"res_op_status"`
}

type FMAvailableReservedResources struct {
	FabricUUID          string `json:"fabric_uuid"`
	FabricID            int    `json:"fabric_id"`
	ReservedResourceNum int    `json:"reserved_res_num_per_fabric"`
}

type CMNodeGroups struct {
	NodeGroups []NodeGroup `json:"nodegroups"`
}

type NodeGroup struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	NodeCount int    `json:"node_count"`
	Role      string `json:"role"`
}

type CMNodeGroupInfo struct {
	UUID         string     `json:"uuid"`
	Name         string     `json:"name"`
	Composable   bool       `json:"composable"`
	NodeCount    int        `json:"node_count"`
	Role         string     `json:"role"`
	MinNodeCount int        `json:"min_node_count"`
	MaxNodeCount int        `json:"max_node_count"`
	Status       string     `json:"status"`
	StatusReason string     `json:"status_reason"`
	Resources    []Resource `json:"resources"`
	NodeIDs      []string   `json:"node_ids"`
	MachineIDs   []string   `json:"mach_ids"`
}

type Resource struct {
	ResourceName     string `json:"resource_name"`
	ResourceType     string `json:"resource_type"`
	ModelName        string `json:"mode_name"`
	MinResourceCount int    `json:"min_resource_count"`
	MaxResourceCount int    `json:"max_resource_count"`
}

func BuildCDIClient(config *config.Config, controllers *kube_utils.KubeControllers) (*CDIClient, error) {
	var standardClient = http.DefaultClient

	client := &CDIClient{
		Host:     config.CDIEndpoint,
		TenantId: config.TenantID,
		Client:   standardClient,
	}

	client.TokenSource = CachedIMTokenSource(client, controllers)

	return client, nil
}

func (c *CDIClient) GetFMMachineList() (*FMMachineList, error) {
	return &FMMachineList{}, nil
}

func (c *CDIClient) GetFMAvailableReservedResources(muuid string) (FMAvailableReservedResources, error) {
	var fmResources FMAvailableReservedResources
	// req := newRequest(http.MethodGet)
	c.TokenSource.Token()
	return fmResources, nil
}

func (c *CDIClient) GetCMNodeGroups() (CMNodeGroups, error) {
	return CMNodeGroups{
		NodeGroups: []NodeGroup{
			{
				UUID: "1",
			},
		},
	}, nil
}

func (c *CDIClient) GetCMNodeGroupInfo(ng NodeGroup) (CMNodeGroupInfo, error) {
	return CMNodeGroupInfo{
		MachineIDs: []string{
			"cdi-control-plane",
		},
		Resources: []Resource{
			{
				ModelName:        "A100 40G",
				MinResourceCount: 1,
				MaxResourceCount: 3,
			},
		},
	}, nil
}
