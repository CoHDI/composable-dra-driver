package client

import (
	"cdi_dra/pkg/config"
	"cdi_dra/pkg/kube_utils"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

type CDIClient struct {
	Host        string
	TenantId    string
	ClusterId   string
	Client      *http.Client
	TokenSource oauth2.TokenSource
}

type IMToken struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	IDToken          string `json:"id_token"`
	NotBeforePolicy  int64  `json:"not-before-policy"`
	SessionState     string `json:"session_state"`
	Scope            string `json:"scope"`
}

type FMMachineList struct {
	Data FMMachines `json:"data"`
}

type FMMachines struct {
	Machines []FMMachine `json:"machines"`
}

type FMMachine struct {
	FabricUUID   *string             `json:"fabric_uuid"`
	FabricID     *int                `json:"fabric_id"`
	MachineUUID  string              `json:"mach_uuid"`
	MachineID    int                 `json:"mach_id"`
	MachineName  string              `json:"mach_name"`
	MachineOwner string              `json:"mach_owner"`
	Resources    []FMMachineResource `json:"resources"`
}

type FMMachineResource struct {
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

type Condition struct {
	Column   string `json:"column"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type CMNodeGroups struct {
	NodeGroups []CMNodeGroup `json:"nodegroups"`
}

type CMNodeGroup struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	NodeCount int    `json:"node_count"`
	Role      string `json:"role"`
}

type CMNodeGroupInfo struct {
	UUID         string                `json:"uuid"`
	Name         string                `json:"name"`
	Composable   bool                  `json:"composable"`
	NodeCount    int                   `json:"node_count"`
	Role         string                `json:"role"`
	MinNodeCount int                   `json:"min_node_count"`
	MaxNodeCount int                   `json:"max_node_count"`
	Status       string                `json:"status"`
	StatusReason string                `json:"status_reason"`
	Resources    []CMNodeGroupResource `json:"resources"`
	NodeIDs      []string              `json:"node_ids"`
	MachineIDs   []string              `json:"mach_ids"`
}

type CMNodeGroupResource struct {
	ResourceName     string `json:"resource_name"`
	ResourceType     string `json:"resource_type"`
	ModelName        string `json:"model_name"`
	MinResourceCount *int   `json:"min_resource_count"`
	MaxResourceCount *int   `json:"max_resource_count"`
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
	SpecUUID        string     `json:"spec_uuid"`
	Type            string     `json:"type"`
	Selector        CMSelector `json:"selector"`
	MinResSpecCount *int       `json:"min_resspec_count"`
	MaxResSpecCount *int       `json:"max_resspec_count"`
	DeviceCount     int        `json:"device_count"`
}

type CMSelector struct {
	Version    string       `json:"version"`
	Expression CMExpression `json:"expression"`
}

type CMExpression struct {
	Conditions []Condition `json:"conditions"`
}

type RequestIDKey struct{}

func BuildCDIClient(config *config.Config, kc *kube_utils.KubeControllers) (*CDIClient, error) {
	secret, err := kc.GetSecret(secretKey)
	if err != nil {
		return nil, err
	}
	var cert []byte
	if secret.Data != nil {
		cert = secret.Data["certificate"]
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(cert)

	tlsConfig := &tls.Config{
		RootCAs: caCertPool,
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	httpClient := &http.Client{
		Transport: transport,
	}

	client := &CDIClient{
		Host:      config.CDIEndpoint,
		TenantId:  config.TenantID,
		ClusterId: config.ClusterID,
		Client:    httpClient,
	}

	client.TokenSource = CachedIMTokenSource(client, kc)

	return client, nil
}

func (c *CDIClient) GetIMToken(ctx context.Context, secret idManagerSecret) (*IMToken, error) {
	imToken := &IMToken{}
	r := newRequest(http.MethodPost)
	path := fmt.Sprintf("id_manager/realms/%s/protocol/openid-connect/token", secret.realm)

	bodyStr := "client_id=%s&client_secret=%s&username=%s&password=%s&scope=openid&response=id_token token&grant_type=password"
	data := fmt.Sprintf(bodyStr, secret.client_id, secret.client_secret, secret.username, secret.password)

	req := r.setHost(c.Host).setPath(path).setBody(data).setHeader("Content-Type", "application/x-www-form-urlencoded")

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, imToken)
	if err != nil {
		slog.Error(err.Error(), "response", string(resp.body))
		return nil, err
	}
	return imToken, nil
}

func (c *CDIClient) GetFMMachineList(ctx context.Context) (*FMMachineList, error) {
	fmMachineList := &FMMachineList{}
	r := newRequest(http.MethodGet)
	path := "fabric_manager/api/v1/machines"
	query := map[string]string{
		"tenant_uuid": c.TenantId,
	}
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	req := r.setHost(c.Host).setPath(path).setQuery(query).setHeader("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, fmMachineList)
	if err != nil {
		slog.Error(err.Error(), "response", string(resp.body))
		return nil, err
	}
	return fmMachineList, nil
}

func (c *CDIClient) GetFMAvailableReservedResources(ctx context.Context, muuid string, modelName string) (*FMAvailableReservedResources, error) {
	fmAvailables := &FMAvailableReservedResources{}
	r := newRequest(http.MethodGet)
	path := "fabric_manager/api/v1/machines/" + muuid + "/available-reserved-resources"
	condition := Condition{
		Column:   "model",
		Operator: "eq",
		Value:    modelName,
	}
	jsonData, err := json.Marshal(condition)
	if err != nil {
		slog.Error("failed to marshal json", "error", err)
	}
	query := map[string]string{
		"tenant_uuid": c.TenantId,
		"res_type":    "gpu",
		"condition":   string(jsonData),
	}
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	req := r.setHost(c.Host).setPath(path).setQuery(query).setHeader("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, fmAvailables)
	if err != nil {
		slog.Error(err.Error(), "response", string(resp.body))
		return nil, err
	}
	return fmAvailables, nil
}

func (c *CDIClient) GetCMNodeGroups(ctx context.Context) (*CMNodeGroups, error) {
	cmNodeGroups := &CMNodeGroups{}
	r := newRequest(http.MethodGet)
	path := "cluster_manager/cluster_autoscaler/v2/tenants/" + c.TenantId + "/clusters/" + c.ClusterId + "/nodegroups"
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	req := r.setHost(c.Host).setPath(path).setHeader("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, cmNodeGroups)
	if err != nil {
		slog.Error(err.Error(), "response", string(resp.body))
		return nil, err
	}
	return cmNodeGroups, nil
}

func (c *CDIClient) GetCMNodeGroupInfo(ctx context.Context, ng CMNodeGroup) (*CMNodeGroupInfo, error) {
	cmNodeGroupInfo := &CMNodeGroupInfo{}
	r := newRequest(http.MethodGet)
	path := "cluster_manager/cluster_autoscaler/v2/tenants/" + c.TenantId + "/clusters/" + c.ClusterId + "/nodegroups/" + ng.UUID
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	req := r.setHost(c.Host).setPath(path).setHeader("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, cmNodeGroupInfo)
	if err != nil {
		return nil, err
	}
	return cmNodeGroupInfo, nil
}

func (c *CDIClient) GetCMNodeDetails(ctx context.Context, muuid string) (*CMNodeDetails, error) {
	cmNodeDetails := &CMNodeDetails{}
	r := newRequest(http.MethodGet)
	path := "cluster_manager/cluster_autoscaler/v3/tenants/" + c.TenantId + "/clusters/" + c.ClusterId + "/machines/" + muuid
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	req := r.setHost(c.Host).setPath(path).setHeader("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(ctx, cmNodeDetails)
	if err != nil {
		return nil, err
	}
	return cmNodeDetails, nil
}

type result struct {
	body       []byte
	statusCode int
}

func (c *CDIClient) do(ctx context.Context, req *http.Request) (*result, error) {
	var result result
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := c.Client.Do(req)
	if err != nil {
		slog.Error("faild to Do http request", "error", err, "requestID", ctx.Value(RequestIDKey{}).(string))
		return &result, err
	}
	defer resp.Body.Close()

	if resp.Body != nil {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("unexpected error occured when reading response body", "error", err, "requestID", ctx.Value(RequestIDKey{}).(string))
			return &result, err
		}
		result.body = data
	}
	result.statusCode = resp.StatusCode
	return &result, nil
}

func (r *result) into(ctx context.Context, v any) error {
	if r.statusCode < 200 || r.statusCode >= 300 {
		res := &unsuccessfulResponse{}
		if err := json.Unmarshal(r.body, res); err != nil || res.Detail.Message == "" {
			res.Detail.Message = string(r.body)
		}
		err := fmt.Errorf("received unsuccessful response")
		slog.Error(err.Error(), "code", r.statusCode, "requestID", ctx.Value(RequestIDKey{}).(string))
		slog.Error("Detail:", "msg", res.Detail.Message, "requestID", ctx.Value(RequestIDKey{}).(string))
		return err
	}
	if err := json.Unmarshal(r.body, v); err != nil {
		return fmt.Errorf("failed to read response data into %T", v)
	}
	return nil
}

type unsuccessfulResponse struct {
	Status string         `json:"status"`
	Detail responseDetail `json:"detail"`
}

type responseDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const CharSet = "123456789"

func RandomString(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = CharSet[rand.Intn(len(CharSet))]
	}
	return string(result)
}
