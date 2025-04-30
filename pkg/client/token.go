package client

import (
	"cdi_dra/pkg/kube_utils"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	secretKey = "composable-dra/composable-dra-secret"
)

type cachedIMTokenSource struct {
	newIMTokenSource oauth2.TokenSource
	mu               sync.Mutex
	marginTime       time.Duration
	token            *oauth2.Token
}

type accessToken struct {
	Expiry int64 `json:"exp"`
}

type idManagerSecret struct {
	username      string
	password      string
	realm         string
	client_id     string
	client_secret string
	certificate   string
}

func CachedIMTokenSource(client *CDIClient, controllers *kube_utils.KubeControllers) oauth2.TokenSource {
	return &cachedIMTokenSource{
		newIMTokenSource: &idManagerTokenSource{
			cdiclient:       client,
			kubecontrollers: controllers,
		},
		marginTime: 30 * time.Second,
	}

}

func (ts *cachedIMTokenSource) Token() (*oauth2.Token, error) {
	var token *oauth2.Token
	now := time.Now()
	ts.mu.Lock()
	token = ts.token
	ts.mu.Unlock()
	if token != nil && token.Expiry.Add(-ts.marginTime).After(now) {
		slog.Debug("Token executed: using cached token")
		return token, nil
	}
	slog.Info("Token executed: trying to issue new token")
	token, err := ts.newIMTokenSource.Token()
	if err != nil {
		if ts.token == nil {
			slog.Error("failed to issue new token")
			return nil, err
		}
		slog.Error("unable to rotate token", "error", err)
		return ts.token, nil
	}
	slog.Info("new token is successfully issued")
	ts.token = token
	return token, nil
}

type idManagerTokenSource struct {
	cdiclient       *CDIClient
	kubecontrollers *kube_utils.KubeControllers
}

func (ts *idManagerTokenSource) Token() (*oauth2.Token, error) {
	var token oauth2.Token
	secret, err := ts.getIdManagerSecret()
	if err != nil {
		return nil, err
	}
	ctx := context.WithValue(context.Background(), RequestIDKey{}, RandomString(6))
	slog.Info("trying API to get IM token", "requestID", ctx.Value(RequestIDKey{}).(string))
	imToken, err := ts.cdiclient.GetIMToken(ctx, secret)
	if err != nil {
		slog.Error("IM token API failed", "requestID", ctx.Value(RequestIDKey{}).(string))
		return nil, err
	}
	slog.Info("IM token API completed successfully", "requestID", ctx.Value(RequestIDKey{}).(string))

	token.AccessToken = imToken.AccessToken
	token.TokenType = imToken.TokenType
	token.RefreshToken = imToken.RefreshToken

	parts := strings.Split(imToken.AccessToken, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid access token: %s", imToken.AccessToken)
	}

	decodedBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %s", err)
	}

	var result accessToken
	err = json.Unmarshal(decodedBytes, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %s", err)
	}

	token.Expiry = time.Unix(result.Expiry, 0)

	return &token, nil
}

func (ts *idManagerTokenSource) getIdManagerSecret() (idManagerSecret, error) {
	var imSecret idManagerSecret
	secret, err := ts.kubecontrollers.GetSecret(secretKey)
	if err != nil {
		return imSecret, err
	}
	if secret != nil {
		if secret.Data != nil {
			imSecret.username = string(secret.Data["username"])
			imSecret.password = string(secret.Data["password"])
			imSecret.realm = string(secret.Data["realm"])
			imSecret.client_id = string(secret.Data["client_id"])
			imSecret.client_secret = string(secret.Data["client_secret"])
			imSecret.certificate = string(secret.Data["certificate"])
		}
	}
	return imSecret, nil
}
