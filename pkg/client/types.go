package client

type IMToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

type FMMachineList struct {
	Data FMMachines `json:"data"`
}

type FMMachines struct {
	Machines []FMMachine `json:"machines"`
}

type FMMachine struct {
	FabricID    *int   `json:"fabric_id"`
	MachineUUID string `json:"mach_uuid"`
}

type FMAvailableReservedResources struct {
	FabricID            int `json:"fabric_id"`
	ReservedResourceNum int `json:"reserved_res_num_per_fabric"`
}

type CMNodeGroups struct {
	NodeGroups []CMNodeGroup `json:"nodegroups"`
}

type CMNodeGroup struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type CMNodeGroupInfo struct {
	UUID       string   `json:"uuid"`
	Name       string   `json:"name"`
	MachineIDs []string `json:"mach_ids"`
}

type CMNodeDetails struct {
	Data CMTenant `json:"data"`
}

type CMTenant struct {
	TenantUUID string    `json:"tenant_uuid"`
	Cluster    CMCluster `json:"cluster"`
}

type CMCluster struct {
	ClusterUUID string    `json:"cluster_uuid"`
	Machine     CMMachine `json:"machine"`
}

type CMMachine struct {
	UUID     string      `json:"uuid"`
	Name     string      `json:"name"`
	ResSpecs []CMResSpec `json:"resspecs"`
}

type CMResSpec struct {
	Type            string     `json:"type"`
	Selector        CMSelector `json:"selector"`
	MinResSpecCount *int       `json:"min_resspec_count"`
	MaxResSpecCount *int       `json:"max_resspec_count"`
}

type CMSelector struct {
	Expression CMExpression `json:"expression"`
}

type CMExpression struct {
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	Column   string `json:"column"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}
