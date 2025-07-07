package client

import (
	"cdi_dra/pkg/config"
	ku "cdi_dra/pkg/kube_utils"
	"testing"
)

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
			name:                 "When correctly secret created",
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
