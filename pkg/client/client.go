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
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const (
	CDIAPITimeOut = 60 * time.Second
)

type CDIClient struct {
	Host        string
	TenantId    string
	ClusterId   string
	Client      *http.Client
	TokenSource oauth2.TokenSource
}

type RequestIDKey struct{}

func BuildCDIClient(config *config.Config, kc *kube_utils.KubeControllers) (*CDIClient, error) {
	secret, err := kc.GetSecret(secretKey)
	if err != nil {
		return nil, err
	}
	var cert []byte
	if secret.Data != nil {
		certificate := secret.Data["certificate"]
		if len(certificate) < secretCertificateLength {
			cert = secret.Data["certificate"]
		} else {
			return nil, fmt.Errorf("certificate length exceeds the limitation")
		}
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(imToken)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(fmMachineList)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(fmAvailables)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(cmNodeGroups)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(cmNodeGroupInfo)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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

	slog.Debug("connecting", "url", req.url().String())
	httpReq, err := newHTTPRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	err = resp.into(cmNodeDetails)
	if err != nil {
		slog.Error(err.Error(), "code", resp.statusCode, "requestID", GetRequestIdFromContext(ctx), "response", string(resp.body))
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
	ctx, cancel := context.WithTimeout(ctx, CDIAPITimeOut)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := c.Client.Do(req)
	if err != nil {
		slog.Error("failed to Do http request", "error", err, "requestID", GetRequestIdFromContext(ctx))
		return &result, err
	}
	defer resp.Body.Close()

	if resp.Body != nil {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("unexpected error occurred when reading response body", "error", err, "requestID", GetRequestIdFromContext(ctx))
			return &result, err
		}
		result.body = data
	}
	result.statusCode = resp.StatusCode
	return &result, nil
}

func (r *result) into(v any) error {
	if r.statusCode < 200 || r.statusCode >= 300 {
		res := &unsuccessfulResponse{}
		if err := json.Unmarshal(r.body, res); err != nil || res.Detail.Message == "" {
			res.Detail.Message = string(r.body)
		}
		return fmt.Errorf("received unsuccessful response: %s", res.Detail.Message)
	}
	if err := json.Unmarshal(r.body, v); err != nil {
		return fmt.Errorf("failed to read response data into %T: %v", v, err)
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

func GetRequestIdFromContext(ctx context.Context) string {
	val, ok := ctx.Value(RequestIDKey{}).(string)
	if !ok {
		return "NotFound"
	}
	return val
}
