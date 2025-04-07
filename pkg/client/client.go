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

type FMAvailableReservedResources struct {
	FabricUUID          string `json:"fabric_uuid"`
	FabricID            int    `json:"fabric_id"`
	ReservedResourceNum int    `json:"reserved_res_num_per_fabric"`
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

func (c *CDIClient) GetFMAvailableReservedResources(muuid string) (FMAvailableReservedResources, error) {
	var fmResources FMAvailableReservedResources
	// req := newRequest(http.MethodGet)
	c.TokenSource.Token()
	return fmResources, nil
}
