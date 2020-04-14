package exec

import (
	"testing"
)

const dockerPath = ""

func TestNewEdgeStackManager(t *testing.T) {
	_, err := NewEdgeStackManager(dockerPath)
	if err != nil {
		t.Errorf("Failed creating manager: %v", err)
	}
}

func TestLogin(t *testing.T) {
	manager, err := NewEdgeStackManager(dockerPath)
	if err != nil {
		t.Errorf("Failed creating manager: %v", err)
	}

	err = manager.Login()
	if err != nil {
		t.Errorf("Failed login: %v", err)
	}
}

func TestLogout(t *testing.T) {
	manager, err := NewEdgeStackManager(dockerPath)
	if err != nil {
		t.Errorf("Failed creating manager: %v", err)
	}

	err = manager.Login()
	if err != nil {
		t.Errorf("Failed login: %v", err)
	}

	err = manager.Logout()
	if err != nil {
		t.Errorf("Failed logout: %v", err)
	}
}

func TestDeploy(t *testing.T) {
	// Deploy/Remove are harder to test because we need:
	// 1. create manager
	// 2. create stack with file (os)
	// 3. run deploy

}

func TestRemove(t *testing.T) {
	// Deploy/Remove are harder to test because we need:
	// 1. create manager
	// 2. create stack with file (os)
	// 3. run deploy
	// 4. remove
}
