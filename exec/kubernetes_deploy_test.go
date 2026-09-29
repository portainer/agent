package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperation_InClusterBuildsClientWithVerifiedCA(t *testing.T) {
	t.Setenv("DEV_KUBECONFIG_PATH", "")

	tokenFile := filesystem.JoinPaths(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("a-token"), 0o600))

	original := serviceAccountTokenFile
	t.Cleanup(func() { serviceAccountTokenFile = original })
	serviceAccountTokenFile = tokenFile

	deployer := &KubernetesDeployer{}

	err := deployer.operation(t.Context(), "stack", []string{"manifest.yaml"}, "delete", "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute kubectl delete command")
}

func TestDeployRawConfig_MissingServiceHostEnvVar(t *testing.T) {
	t.Parallel()

	deployer := &KubernetesDeployer{}

	_, err := deployer.DeployRawConfig("a-token", "", "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), agent.KubernetesServiceHost)
}

func TestDeployRawConfig_MissingServicePortEnvVar(t *testing.T) {
	t.Setenv(agent.KubernetesServiceHost, "10.0.0.1")

	deployer := &KubernetesDeployer{}

	_, err := deployer.DeployRawConfig("a-token", "", "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), agent.KubernetesServicePortHttps)
}

// DeployRawConfig always talks to the in-cluster API server, so it always sets
// CAFile to verify against the service account CA bundle, regardless of the
// literal host:port built from the env vars.
func TestDeployRawConfig_BuildsClientAndReachesTheServer(t *testing.T) {
	t.Setenv(agent.KubernetesServiceHost, "127.0.0.1")
	t.Setenv(agent.KubernetesServicePortHttps, "0")

	deployer := &KubernetesDeployer{}

	_, err := deployer.DeployRawConfig("a-token", "apiVersion: v1\nkind: Namespace", "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute kubectl apply command")
}

type mockKubectlClient struct {
	applyFunc          func(ctx context.Context, files []string) error
	deleteFunc         func(ctx context.Context, files []string) error
	rolloutRestartFunc func(ctx context.Context, resources []string) error
}

func (m *mockKubectlClient) ApplyDynamic(ctx context.Context, files []string) error {
	if m.applyFunc != nil {
		return m.applyFunc(ctx, files)
	}
	return nil
}

func (m *mockKubectlClient) DeleteDynamic(ctx context.Context, files []string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, files)
	}
	return nil
}

func (m *mockKubectlClient) RolloutRestart(ctx context.Context, resources []string) error {
	if m.rolloutRestartFunc != nil {
		return m.rolloutRestartFunc(ctx, resources)
	}
	return nil
}

func testExecuteKubectlOperation(ctx context.Context, client *mockKubectlClient, operation string, manifestFiles []string) error {
	operations := map[string]func(context.Context, []string) error{
		"apply":           client.ApplyDynamic,
		"delete":          client.DeleteDynamic,
		"rollout-restart": client.RolloutRestart,
	}

	operationFunc, ok := operations[operation]
	if !ok {
		return fmt.Errorf("unsupported operation: %s", operation)
	}

	if err := operationFunc(ctx, manifestFiles); err != nil {
		return fmt.Errorf("failed to execute kubectl %s command: %w", operation, err)
	}

	return nil
}

func TestExecuteKubectlOperation_Apply_Success(t *testing.T) {
	t.Parallel()
	called := false
	mockClient := &mockKubectlClient{
		applyFunc: func(ctx context.Context, files []string) error {
			called = true
			assert.Equal(t, []string{"manifest1.yaml", "manifest2.yaml"}, files)
			return nil
		},
	}

	manifests := []string{"manifest1.yaml", "manifest2.yaml"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "apply", manifests)

	require.NoError(t, err)
	assert.True(t, called)
}

func TestExecuteKubectlOperation_Apply_Error(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("kubectl apply failed")
	called := false
	mockClient := &mockKubectlClient{
		applyFunc: func(ctx context.Context, files []string) error {
			called = true
			assert.Equal(t, []string{"error.yaml"}, files)
			return expectedErr
		},
	}

	manifests := []string{"error.yaml"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "apply", manifests)

	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
	assert.True(t, called)
}

func TestExecuteKubectlOperation_Delete_Success(t *testing.T) {
	t.Parallel()
	called := false
	mockClient := &mockKubectlClient{
		deleteFunc: func(ctx context.Context, files []string) error {
			called = true
			assert.Equal(t, []string{"manifest1.yaml"}, files)
			return nil
		},
	}

	manifests := []string{"manifest1.yaml"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "delete", manifests)

	require.NoError(t, err)
	assert.True(t, called)
}

func TestExecuteKubectlOperation_Delete_Error(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("kubectl delete failed")
	called := false
	mockClient := &mockKubectlClient{
		deleteFunc: func(ctx context.Context, files []string) error {
			called = true
			assert.Equal(t, []string{"error.yaml"}, files)
			return expectedErr
		},
	}

	manifests := []string{"error.yaml"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "delete", manifests)

	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
	assert.True(t, called)
}

func TestExecuteKubectlOperation_RolloutRestart_Success(t *testing.T) {
	t.Parallel()
	called := false
	mockClient := &mockKubectlClient{
		rolloutRestartFunc: func(ctx context.Context, resources []string) error {
			called = true
			assert.Equal(t, []string{"deployment/nginx"}, resources)
			return nil
		},
	}

	resources := []string{"deployment/nginx"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "rollout-restart", resources)

	require.NoError(t, err)
	assert.True(t, called)
}

func TestExecuteKubectlOperation_RolloutRestart_Error(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("kubectl rollout restart failed")
	called := false
	mockClient := &mockKubectlClient{
		rolloutRestartFunc: func(ctx context.Context, resources []string) error {
			called = true
			assert.Equal(t, []string{"deployment/error"}, resources)
			return expectedErr
		},
	}

	resources := []string{"deployment/error"}
	err := testExecuteKubectlOperation(t.Context(), mockClient, "rollout-restart", resources)

	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
	assert.True(t, called)
}

func TestExecuteKubectlOperation_UnsupportedOperation(t *testing.T) {
	t.Parallel()
	mockClient := &mockKubectlClient{}

	err := testExecuteKubectlOperation(t.Context(), mockClient, "unsupported", []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}
