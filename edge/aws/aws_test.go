package aws

import (
	"errors"
	"testing"
	"time"

	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/portainer/agent"
	retry "github.com/portainer/portainer/pkg/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	privateECRUrl = "123456789012.dkr.ecr.us-east-1.amazonaws.com"
	publicECRUrl  = "public.ecr.aws/my-repo"
	otherUrl      = "docker.io/library/ubuntu:latest"

	testRetrySettings = retry.Settings{
		MaxRetries: 3,
		RetryTimeFunc: func(failures int) time.Duration {
			return 1 * time.Nanosecond // instant retries for tests
		},
	}
)

func TestGetOrRefreshGlobalClient(t *testing.T) {
	// save original state
	originalClient := globalClient
	originalExpiry := globalClientExpiry
	originalRetrySettings := retrySettings
	defer func() {
		// restore original state
		globalClient = originalClient
		globalClientExpiry = originalExpiry
		retrySettings = originalRetrySettings
	}()

	// Override retry settings to make tests fail fast
	retrySettings = testRetrySettings

	t.Run("reuses valid client", func(t *testing.T) {
		// mock client not expired
		mockClient := &mockECRClient{}
		globalClient = mockClient
		globalClientExpiry = time.Now().Add(30 * time.Minute)

		client, err := getOrRefreshGlobalClient(&agent.AWSConfig{})

		// client will still be valid, so the same client is returned without error
		require.NoError(t, err)
		assert.Equal(t, mockClient, client)
	})

	t.Run("expired client triggers refresh", func(t *testing.T) {
		// Set up an expired client
		mockClient := &mockECRClient{}
		globalClient = mockClient
		globalClientExpiry = time.Time{}

		config := &agent.AWSConfig{
			RoleARN:        "role",
			TrustAnchorARN: "trust-anchor",
			ProfileARN:     "profile",
			Region:         "us-east-1",
			ClientCertPath: "/path/to/cert",
			ClientKeyPath:  "/path/to/key",
		}

		// this will attempt to trigger IAMRA auth and create a new client,
		// which will fail because we don't have real cert configuration in the test environment
		_, err := getOrRefreshGlobalClient(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch ECR credentials")
	})
}

func TestSessionDurationConstants(t *testing.T) {
	// require that session duration is less than the AWS hardcoded duration, this should never happen
	assert.Less(t, iamraSessionDurationSec, 12*60*60, "IAMRA session duration should not exceed 12 hours")
}

func TestECRLoginWithValidClient(t *testing.T) {
	// save original state
	originalClient := globalClient
	originalExpiry := globalClientExpiry
	defer func() {
		globalClient = originalClient
		globalClientExpiry = originalExpiry
	}()

	var cases = []struct {
		name          string
		shouldSucceed bool
	}{
		{
			name:          "successful credentials",
			shouldSucceed: true,
		},
		{
			name:          "failed credentials",
			shouldSucceed: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockECRClient{
				getCredentialsFunc: func(serverURL string) (*api.Auth, error) {
					if tt.shouldSucceed {
						return &api.Auth{
							Username: "AWS",
							Password: "test-token-12345",
						}, nil
					} else {
						return nil, errors.New("failed to get credentials")
					}
				},
			}

			globalClient = mockClient
			globalClientExpiry = time.Now().Add(1 * time.Hour)

			config := &agent.AWSConfig{
				RoleARN:        "role",
				TrustAnchorARN: "trust-anchor",
				ProfileARN:     "profile",
				Region:         "us-east-1",
			}

			creds, err := DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(privateECRUrl, config)

			if tt.shouldSucceed {
				require.NoError(t, err)
				require.NotNil(t, creds)
				assert.Equal(t, privateECRUrl, creds.ServerURL)
				assert.Equal(t, "AWS", creds.Username)
				assert.Equal(t, "test-token-12345", creds.Secret)
			} else {
				require.Error(t, err)
				assert.Nil(t, creds)
				assert.Contains(t, err.Error(), "failed to get credentials")
			}
		})
	}
}

func TestIsValidAWSConfig(t *testing.T) {
	tests := []struct {
		name     string
		opts     *agent.Options
		expected bool
	}{
		{
			name:     "nil options",
			opts:     nil,
			expected: false,
		},
		{
			name:     "empty options",
			opts:     &agent.Options{},
			expected: false,
		},
		{
			name: "missing role ARN",
			opts: &agent.Options{
				AWSTrustAnchorARN: "trust-anchor",
				AWSProfileARN:     "profile",
				AWSRegion:         "us-east-1",
			},
			expected: false,
		},
		{
			name: "missing trust anchor ARN",
			opts: &agent.Options{
				AWSRoleARN:    "role",
				AWSProfileARN: "profile",
				AWSRegion:     "us-east-1",
			},
			expected: false,
		},
		{
			name: "missing profile ARN",
			opts: &agent.Options{
				AWSRoleARN:        "role",
				AWSTrustAnchorARN: "trust-anchor",
				AWSRegion:         "us-east-1",
			},
			expected: false,
		},
		{
			name: "missing region",
			opts: &agent.Options{
				AWSRoleARN:        "role",
				AWSTrustAnchorARN: "trust-anchor",
				AWSProfileARN:     "profile",
			},
			expected: false,
		},
		{
			name: "valid config",
			opts: &agent.Options{
				AWSRoleARN:        "role",
				AWSTrustAnchorARN: "trust-anchor",
				AWSProfileARN:     "profile",
				AWSRegion:         "us-east-1",
			},
			expected: true,
		},
		{
			name: "valid config with optional fields",
			opts: &agent.Options{
				AWSRoleARN:        "role",
				AWSTrustAnchorARN: "trust-anchor",
				AWSProfileARN:     "profile",
				AWSRegion:         "us-east-1",
				AWSClientCert:     "/path/to/cert",
				AWSClientKey:      "/path/to/key",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidAWSConfig(tt.opts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDoAWSIAMRolesAnywhereAuthAndGetECRCredentials_ValidationErrors(t *testing.T) {
	validConfig := &agent.AWSConfig{
		RoleARN:        "role",
		TrustAnchorARN: "trust-anchor",
		ProfileARN:     "profile",
		Region:         "us-east-1",
		ClientCertPath: "/path/to/cert",
		ClientKeyPath:  "/path/to/key",
	}

	tests := []struct {
		name        string
		serverURL   string
		awsConfig   *agent.AWSConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty server URL",
			serverURL:   "",
			awsConfig:   validConfig,
			expectError: true,
			errorMsg:    "invalid ecr configuration",
		},
		{
			name:        "nil aws config",
			serverURL:   privateECRUrl,
			awsConfig:   nil,
			expectError: true,
			errorMsg:    "invalid ecr configuration",
		},
		{
			name:        "invalid server URL format",
			serverURL:   "not-a-valid-url",
			awsConfig:   validConfig,
			expectError: true,
			errorMsg:    "not a private ECR repository",
		},
		{
			name:        "other server URL format",
			serverURL:   otherUrl,
			awsConfig:   validConfig,
			expectError: true,
			errorMsg:    "not a private ECR repository",
		},
		{
			name:        "public ECR repository",
			serverURL:   publicECRUrl,
			awsConfig:   validConfig,
			expectError: true,
			errorMsg:    "not a private ECR repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DoAWSIAMRolesAnywhereAuthAndGetECRCredentials(tt.serverURL, tt.awsConfig)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type mockECRClient struct {
	getCredentialsFunc func(serverURL string) (*api.Auth, error)
}

func (m *mockECRClient) GetCredentials(serverURL string) (*api.Auth, error) {
	if m.getCredentialsFunc != nil {
		return m.getCredentialsFunc(serverURL)
	}
	return nil, errors.New("not implemented")
}

func (m *mockECRClient) GetCredentialsByRegistryID(registryID string) (*api.Auth, error) {
	return nil, errors.New("not implemented")
}

func (m *mockECRClient) ListCredentials() ([]*api.Auth, error) {
	return nil, errors.New("not implemented")
}
