package client

import (
	"cdi_dra/pkg/kube_utils"
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
	ts.newIMTokenSource.Token()
	return token, nil
}

type idManagerTokenSource struct {
	cdiclient       *CDIClient
	kubecontrollers *kube_utils.KubeControllers
}

func (ts *idManagerTokenSource) Token() (*oauth2.Token, error) {
	var token *oauth2.Token
	_, err := ts.getIdManagerSecret()
	if err != nil {
		return nil, err
	}
	return token, nil
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
			imSecret.client_id = string(secret.Data["cleint_id"])
			imSecret.client_secret = string(secret.Data["client_secret"])
			imSecret.certificate = string(secret.Data["certificate"])
		}
	}
	return imSecret, nil
}
