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
	ku "cdi_dra/pkg/kube_utils"
	"encoding/base64"
	"regexp"
	"testing"
	"time"
)

func TestCachedIMTokenSourceToken(t *testing.T) {
	caData, err := config.CreateTestCACertificate()
	if err != nil {
		t.Fatalf("failed to create ca certificate: %v", err)
	}
	testCases := []struct {
		name                 string
		secretCase           int
		certPem              string
		sleepTime            time.Duration
		expectedErr          bool
		expectedTokenUpdate  bool
		expectedAccessToken  string
		expectedTokenType    string
		expectedRefreshToken string
	}{
		{
			name:                 "When token is cached",
			secretCase:           8,
			sleepTime:            1,
			certPem:              caData.CertPem,
			expectedErr:          false,
			expectedTokenUpdate:  false,
			expectedAccessToken:  `^token1`,
			expectedTokenType:    "Bearer",
			expectedRefreshToken: "token2",
		},
		{
			name:                 "When token is newly issued",
			secretCase:           8,
			sleepTime:            5,
			certPem:              caData.CertPem,
			expectedErr:          false,
			expectedTokenUpdate:  true,
			expectedAccessToken:  `^token1`,
			expectedTokenType:    "Bearer",
			expectedRefreshToken: "token2",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := "0001"
			clusterID := "0001"
			client, server, stopServer := buildTestCDIClient(t, tenantID, clusterID)
			defer stopServer()
			defer server.Close()
			secret := config.CreateSecret(tc.certPem, tc.secretCase)
			testConfig := &config.TestConfig{
				Secret: secret,
			}
			kubeclient, dynamicclient := ku.CreateTestClient(t, testConfig)
			controllers, stopController := ku.CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stopController()

			tokenSource := CachedIMTokenSource(client, controllers)

			now := time.Now()
			token1, _ := tokenSource.Token()
			expiry1 := token1.Expiry

			time.Sleep(tc.sleepTime * time.Second)

			token2, err := tokenSource.Token()

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				re, err := regexp.Compile(tc.expectedAccessToken)
				if err != nil {
					t.Fatalf("Error compiling regex: %v", err)
				}
				if !re.MatchString(token2.AccessToken) {
					t.Errorf("unexpected AccessToken, expected %s but got %s", tc.expectedAccessToken, token2.AccessToken)
				}
				if token2.TokenType != tc.expectedTokenType {
					t.Errorf("unexpected TokenType, expected %s but got %s", tc.expectedTokenType, token2.TokenType)
				}
				if token2.RefreshToken != tc.expectedRefreshToken {
					t.Errorf("unexpected RefreshToken, expected %s but got %s", tc.expectedRefreshToken, token2.RefreshToken)
				}
				if tc.expectedTokenUpdate {
					if token2.Expiry.Equal(expiry1) || token2.Expiry.Before(now.Add(35*time.Second)) {
						t.Error("unexpected expiry of token, expected update but not done")
					}
				} else if !tc.expectedTokenUpdate {
					if !token2.Expiry.Equal(expiry1) {
						t.Error("unexpected expiry of token, expected cached but updated")
					}
				}
			}
		})
	}
}

func TestIdManagerTokenSourceToken(t *testing.T) {
	caData, err := config.CreateTestCACertificate()
	if err != nil {
		t.Fatalf("failed to create ca certificate: %v", err)
	}
	testCases := []struct {
		name                 string
		secretCase           int
		certPem              string
		expectedErr          bool
		expectedAccessToken  string
		expectedTokenType    string
		expectedRefreshToken string
		expectedExpiry       time.Time
	}{
		{
			name:                 "When correct IMToken is obtained",
			secretCase:           1,
			certPem:              caData.CertPem,
			expectedErr:          false,
			expectedAccessToken:  "token1" + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":775710000}`)),
			expectedTokenType:    "Bearer",
			expectedRefreshToken: "token2",
			expectedExpiry:       time.Unix(775710000, 0),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := "0001"
			clusterID := "0001"
			client, server, stopServer := buildTestCDIClient(t, tenantID, clusterID)
			defer stopServer()
			defer server.Close()
			secret := config.CreateSecret(tc.certPem, tc.secretCase)
			testConfig := &config.TestConfig{
				Secret: secret,
			}
			kubeclient, dynamicclient := ku.CreateTestClient(t, testConfig)
			controllers, stopController := ku.CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stopController()
			imTokenSource := &idManagerTokenSource{
				cdiclient:       client,
				kubecontrollers: controllers,
			}

			token, err := imTokenSource.Token()

			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if token.AccessToken != tc.expectedAccessToken {
					t.Errorf("unexpected AccessToken, expected %s but got %s", tc.expectedAccessToken, token.AccessToken)
				}
				if token.TokenType != tc.expectedTokenType {
					t.Errorf("unexpected TokenType, expected %s but got %s", tc.expectedTokenType, token.TokenType)
				}
				if token.RefreshToken != tc.expectedRefreshToken {
					t.Errorf("unexpected RefreshToken, expected %s but got %s", tc.expectedRefreshToken, token.RefreshToken)
				}
				if token.Expiry != tc.expectedExpiry {
					t.Errorf("unexpected token Expiry, expected %s but got %s", tc.expectedExpiry, token.Expiry)
				}
			}
		})
	}
}

func TestGetIdManagerSecret(t *testing.T) {
	testCases := []struct {
		name                 string
		secretCase           int
		expectedErr          bool
		expectedErrMessage   string
		expectedUserName     string
		expectedPassword     string
		expectedRealm        string
		expectedClientId     string
		expectedClientSecret string
	}{
		{
			name:                 "When correct secret is created",
			secretCase:           1,
			expectedErr:          false,
			expectedUserName:     "user",
			expectedPassword:     "pass",
			expectedRealm:        "CDI_DRA_Test",
			expectedClientId:     "0001",
			expectedClientSecret: "secret",
		},
		{
			name:               "When username in secret exceeds limits of character count",
			secretCase:         2,
			expectedErr:        true,
			expectedErrMessage: "username length exceeds the limitation",
		},
		{
			name:               "When password in secret exceeds limits of character count",
			secretCase:         3,
			expectedErr:        true,
			expectedErrMessage: "password length exceeds the limitation",
		},
		{
			name:               "When realm in secret exceeds limits of character count",
			secretCase:         4,
			expectedErr:        true,
			expectedErrMessage: "realm length exceeds the limitation",
		},
		{
			name:               "When client_id in secret exceeds limits of character count",
			secretCase:         5,
			expectedErr:        true,
			expectedErrMessage: "client_id length exceeds the limitation",
		},
		{
			name:               "When client_secret in secret exceeds limits of character count",
			secretCase:         6,
			expectedErr:        true,
			expectedErrMessage: "client_secret length exceeds the limitation",
		},
		{
			name:             "When username in secret doesn't exceed limits of character count",
			secretCase:       7,
			expectedErr:      false,
			expectedUserName: config.UnExceededSecretInfo,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			caData, err := config.CreateTestCACertificate()
			if err != nil {
				t.Fatalf("failed to create ca certificate: %v", err)
			}
			secret := config.CreateSecret(caData.CertPem, tc.secretCase)
			testConfig := &config.TestConfig{
				Secret: secret,
			}
			kubeclient, dynamicclient := ku.CreateTestClient(t, testConfig)
			controllers, stop := ku.CreateTestKubeControllers(t, testConfig, kubeclient, dynamicclient)
			defer stop()

			imTokenSource := idManagerTokenSource{
				kubecontrollers: controllers,
			}

			imSecret, err := imTokenSource.getIdManagerSecret()
			if tc.expectedErr {
				if err == nil {
					t.Error("expected error, but got none")
				}
				if err.Error() != tc.expectedErrMessage {
					t.Errorf("unexpected error got, expected error message %s but got %s", tc.expectedErrMessage, err.Error())
				}
			} else if !tc.expectedErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if imSecret.username != tc.expectedUserName {
					t.Errorf("unexpected username of IdManagerSecret, expected %s but got %s", tc.expectedUserName, imSecret.username)
				}
				if imSecret.password != tc.expectedPassword {
					t.Errorf("unexpected password of IdManagerSecret, expected %s but got %s", tc.expectedPassword, imSecret.password)
				}
				if imSecret.realm != tc.expectedRealm {
					t.Errorf("unexpected realm of IdManagerSecret, expected %s but got %s", tc.expectedRealm, imSecret.realm)
				}
				if imSecret.client_id != tc.expectedClientId {
					t.Errorf("unexpected client_id of IdManagerSecret, expected %s but got %s", tc.expectedClientId, imSecret.client_id)
				}
				if imSecret.client_secret != tc.expectedClientSecret {
					t.Errorf("unexpected client_secret of IdManagerSecret, expected %s but got %s", tc.expectedClientSecret, imSecret.client_secret)
				}
			}
		})
	}
}
