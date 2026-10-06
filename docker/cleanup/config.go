package cleanup

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Duration is a time.Duration that serialises to/from a JSON millisecond integer.
// The server always sends durations as int64 milliseconds in PolicyDesiredState.Config;
// using the same format here keeps the wire contract unambiguous.
type Duration time.Duration

// UnmarshalJSON parses a millisecond integer (e.g. 86400000 for 24h).
func (d *Duration) UnmarshalJSON(b []byte) error {
	var milliseconds int64
	if err := json.Unmarshal(b, &milliseconds); err != nil {
		return errors.New("duration must be a JSON millisecond integer")
	}
	*d = Duration(time.Duration(milliseconds) * time.Millisecond)
	return nil
}

// MarshalJSON serialises the duration as a millisecond integer.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(time.Duration(d) / time.Millisecond))
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Config holds all cleanup service configuration.
type Config struct {
	// CleanupInterval is required: the cleanup service does not start if this
	// is non-positive.
	CleanupInterval Duration `json:"cleanupInterval"`

	// CleanOldImages enables removal of images older than ImageMaximumAgeForGC.
	CleanOldImages       bool     `json:"cleanOldImages"`
	ImageMaximumAgeForGC Duration `json:"imageMaximumAgeForGC"`

	// CleanImagesAtStorageLimit enables disk-threshold-driven image removal.
	CleanImagesAtStorageLimit bool `json:"cleanImagesAtStorageLimit"`
	// CleanupStartHostThresholdPercent must be in (0, 100] and
	// strictly greater than CleanupEndHostThresholdPercent when CleanImagesAtStorageLimit is true.
	CleanupStartHostThresholdPercent float64 `json:"cleanupStartHostThresholdPercent"`
	// CleanupEndHostThresholdPercent must be in (0, 100) and
	// strictly less than CleanupStartHostThresholdPercent when CleanImagesAtStorageLimit is true.
	CleanupEndHostThresholdPercent float64 `json:"cleanupEndHostThresholdPercent"`

	// MinAge is the minimum image age for phase-2 candidates. 0 = any age.
	MinAge Duration `json:"minAge"`

	// MaximumImagesPerInterval caps phase-2 removals per cycle. 0 = unlimited.
	// This cap does NOT apply to phase 1.
	MaximumImagesPerInterval int `json:"maximumImagesPerInterval"`

	// ExcludedImages lists image references never removed by either phase.
	// Each entry is matched against the image's "repository:tag" reference:
	//   - An entry with ":" is an exact match (e.g. "nginx:1.25.3").
	//   - An entry without ":" matches any tag of that repository (e.g. "nginx").
	//
	// All entries are normalised before matching, so "docker.io/library/nginx"
	// and "nginx" are equivalent. To protect an image from a private registry,
	// include the registry hostname: "harbor.example.com/myorg/myapp".
	// A bare name like "nginx" does NOT protect "harbor.example.com/nginx:latest".
	//
	// Entries containing "*" or "?" are glob patterns matched like
	// `docker images --filter reference=<pattern>` (e.g. "my-app-*", "my-app:v1.*").
	ExcludedImages []string `json:"excludedImages"`

	// ClearBuildCache controls whether the Docker build cache is pruned during
	// the storage-limit cleanup phase when disk usage exceeds the start threshold.
	// Build cache pruning runs before image removal; if it brings disk usage below
	// the threshold, image removal is skipped entirely for that cycle.
	ClearBuildCache bool `json:"clearBuildCache"`
}

// minCleanupInterval is the minimum allowed cleanup interval. Running GC cycles
// more frequently than this is unlikely to be useful and risks overlapping runs.
const minCleanupInterval = 5 * time.Minute

// Validate returns an error if the config contains invalid field values.
func (c Config) Validate() error {
	if c.CleanupInterval.Std() < minCleanupInterval {
		return errors.New("cleanupInterval must be at least 5 minutes")
	}
	if c.CleanOldImages && c.ImageMaximumAgeForGC.Std() <= 0 {
		return errors.New("imageMaximumAgeForGC must be a positive duration when cleanOldImages is enabled")
	}
	if c.ImageMaximumAgeForGC.Std() < 0 {
		return errors.New("imageMaximumAgeForGC must not be negative")
	}
	if c.CleanImagesAtStorageLimit {
		if c.CleanupStartHostThresholdPercent <= 0 || c.CleanupStartHostThresholdPercent > 100 {
			return errors.New("cleanupStartHostThresholdPercent must be between 1 and 100 when cleanImagesAtStorageLimit is enabled")
		}
		if c.CleanupEndHostThresholdPercent <= 0 || c.CleanupEndHostThresholdPercent > 100 {
			return errors.New("cleanupEndHostThresholdPercent must be between 1 and 100 when cleanImagesAtStorageLimit is enabled")
		}
		if c.CleanupEndHostThresholdPercent >= c.CleanupStartHostThresholdPercent {
			return errors.New("cleanupEndHostThresholdPercent must be less than cleanupStartHostThresholdPercent when cleanImagesAtStorageLimit is enabled")
		}
	}
	if c.MinAge.Std() < 0 {
		return errors.New("minAge must not be negative")
	}
	if c.MaximumImagesPerInterval < 0 {
		return errors.New("maximumImagesPerInterval must not be negative")
	}
	return nil
}

// DecodeConfig deserialises a JSON blob into a Config and validates the result.
// Duration fields must be millisecond integers (e.g. 86400000 for 24h).
// Fields absent from the JSON are left at their zero values.
func DecodeConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid cleanup config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid cleanup config: %w", err)
	}
	return cfg, nil
}
