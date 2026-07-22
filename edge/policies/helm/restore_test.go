package helm_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/portainer/agent/edge/policies/helm"
	"github.com/portainer/agent/policyreconcile"
)

var successApplier helm.ApplierFunc = func(_ context.Context, _ string) error { return nil }

func failApplier(err error) helm.ApplierFunc {
	return func(_ context.Context, _ string) error { return err }
}

func TestRestoreCoordinator_Enqueue_NewEntry(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(successApplier)
	c.Enqueue(1, "manifest-a")
	// Drain should apply and remove it.
	statuses := c.Tick(context.Background(), nil)
	assert.Empty(t, statuses)
	// Second Tick has nothing to do.
	statuses = c.Tick(context.Background(), nil)
	assert.Empty(t, statuses)
}

func TestRestoreCoordinator_Enqueue_SameManifest_AttemptsNotReset(t *testing.T) {
	t.Parallel()
	calls := 0
	applyFn := func(_ context.Context, _ string) error {
		calls++
		return errors.New("transient")
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "manifest-a")
	c.Tick(context.Background(), nil) // attempts = 1
	c.Enqueue(1, "manifest-a")        // same manifest — attempts stays 1
	c.Tick(context.Background(), nil) // attempts = 2
	assert.Equal(t, 2, calls)
}

func TestRestoreCoordinator_Enqueue_DifferentManifest_AttemptsReset(t *testing.T) {
	t.Parallel()
	calls := 0
	applyFn := func(_ context.Context, _ string) error {
		calls++
		return errors.New("transient")
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "manifest-a")
	c.Tick(context.Background(), nil) // attempts = 1
	c.Enqueue(1, "manifest-b")        // different manifest — attempts resets to 0
	c.Tick(context.Background(), nil) // attempts = 1 (not 2)
	assert.Equal(t, 2, calls, "two Tick calls = 2 applier calls")
}

func TestRestoreCoordinator_Tick_CancelsReentered(t *testing.T) {
	t.Parallel()
	applied := false
	applyFn := func(_ context.Context, _ string) error {
		applied = true
		return nil
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "manifest-a")
	// Policy 1 is back in desired — Tick should drop it, not apply.
	c.Tick(context.Background(), []portainer.PolicyID{1})
	assert.False(t, applied, "applier must not be called when policy re-entered desired set")
}

func TestRestoreCoordinator_Tick_SuccessRemovesEntry(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(successApplier)
	c.Enqueue(1, "manifest-a")
	c.Tick(context.Background(), nil)
	// No statuses after success.
	statuses := c.Tick(context.Background(), nil)
	assert.Empty(t, statuses)
}

func TestRestoreCoordinator_Tick_FailureIncrementsAttempts(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(failApplier(errors.New("boom")))
	c.Enqueue(1, "manifest-a")
	// Three calls — at attempt 3 it becomes visible.
	for i := range 3 {
		statuses := c.Tick(context.Background(), nil)
		if i < 2 {
			assert.Empty(t, statuses, "should be silent before visibleAfterAttempts")
		} else {
			require.Len(t, statuses, 1)
			assert.Equal(t, policyreconcile.StatusFailed, statuses[0].Status)
			assert.Contains(t, statuses[0].Message, "3 attempts")
		}
	}
}

func TestRestoreCoordinator_Tick_ContextCancelled_AttemptsNotIncremented(t *testing.T) {
	t.Parallel()
	calls := 0
	applyFn := func(ctx context.Context, _ string) error {
		calls++
		return ctx.Err()
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "manifest-a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Tick(ctx, nil)
	// Entry should still be present with 0 attempts (cancelled context).
	// Second Tick with fresh context should apply.
	c.Tick(context.Background(), nil)
	assert.Equal(t, 2, calls)
}

func TestRestoreCoordinator_Tick_ContextCancelled_ProcessesRemainingRestores(t *testing.T) {
	t.Parallel()
	calls := 0
	applyFn := func(ctx context.Context, _ string) error {
		calls++
		return ctx.Err()
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "manifest-a")
	c.Enqueue(2, "manifest-b")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.Tick(ctx, nil)

	assert.Equal(t, 2, calls)
}

func TestRestoreCoordinator_BelowVisibleThreshold_NoStatuses(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(failApplier(errors.New("fail")))
	c.Enqueue(1, "m")
	for range 2 {
		statuses := c.Tick(context.Background(), nil)
		assert.Empty(t, statuses, "should be silent below visibleAfterAttempts")
	}
}

func TestRestoreCoordinator_AtVisibleThreshold_StatusFailed(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(failApplier(errors.New("fail")))
	c.Enqueue(1, "m")
	var statuses []policyreconcile.ActualState
	for range 3 {
		statuses = c.Tick(context.Background(), nil)
	}
	require.Len(t, statuses, 1)
	assert.Equal(t, policyreconcile.StatusFailed, statuses[0].Status)
	assert.Contains(t, statuses[0].Message, "attempts")
	assert.Contains(t, statuses[0].Message, "fail")
}

func TestRestoreCoordinator_NoHardCap(t *testing.T) {
	t.Parallel()
	calls := 0
	applyFn := func(_ context.Context, _ string) error {
		calls++
		return errors.New("always fails")
	}
	c := helm.NewRestoreCoordinator(applyFn)
	c.Enqueue(1, "m")
	for range 10 {
		c.Tick(context.Background(), nil)
	}
	assert.Equal(t, 10, calls, "applier called every Tick regardless of attempt count")
}

func TestRestoreCoordinator_ConcurrentEnqueueAndTick_NoRace(t *testing.T) {
	t.Parallel()
	c := helm.NewRestoreCoordinator(successApplier)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 50 {
			c.Enqueue(portainer.PolicyID(i), "m")
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			c.Tick(context.Background(), nil)
		}
	}()
	wg.Wait()
}
