package cleanup

import (
	"sort"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types/image"
	"github.com/rs/zerolog/log"
)

// wildcardChars are the glob characters that switch a pattern to wildcard
// matching, using the same rules as `docker images --filter reference=<pattern>`.
const wildcardChars = "*?"

// dockerHubPrefixes are stripped from wildcard patterns so they compare against
// the familiar form Docker shows, matching how exact patterns are normalised.
var dockerHubPrefixes = []string{"docker.io/library/", "docker.io/", "library/"}

func isWildcardPattern(pattern string) bool {
	return strings.ContainsAny(pattern, wildcardChars)
}

// parseExclusionPatterns normalises each pattern via reference.ParseNormalizedNamed
// so that matching is done against canonical image references. Wildcard patterns
// only have their Docker Hub prefix removed. Patterns that cannot be parsed
// (e.g. bare repo names without a tag) are kept as-is.
func parseExclusionPatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if isWildcardPattern(p) {
			out = append(out, trimDockerHubPrefix(p))
			continue
		}

		named, err := reference.ParseNormalizedNamed(p)
		if err != nil {
			log.Warn().
				Str("context", "DockerCleanupParseExclusionPatterns").
				Str("pattern", p).
				Msg("Could not normalize Docker cleanup exclusion pattern, using the raw value")
			out = append(out, p)
			continue
		}
		out = append(out, reference.FamiliarString(named))
	}
	return out
}

// isExcluded reports whether img should be skipped by the cleanup service
// because one of its tags or digests matches an ExcludedImages pattern.
//
// Matching rules (applied after normalisation via reference.ParseNormalizedNamed):
//   - Pattern with a digest ("@sha256:…"): exact match on any entry in
//     img.RepoDigests (e.g. "nginx@sha256:abc" protects only that specific
//     content-addressed image).
//   - Pattern with a tag but no digest: exact match on the full
//     "repository:tag" reference in img.RepoTags
//     (e.g. "nginx:1.25.3" protects only that specific tag).
//   - Pattern with no tag and no digest: exact match on the repository name
//     alone, any tag (e.g. "nginx" or "myregistry:9000/myapp" protects all
//     tags of that repository).
//   - Pattern with "*" or "?": glob match via reference.FamiliarMatch against
//     each tag, first as "repository:tag" then as the repository alone
//     (e.g. "my-app-*" or "my-app:v1.*"). "*" does not match "/".
//
// Dangling images (no RepoTags) are never excluded via tag/name patterns but
// can be excluded by a matching digest pattern in RepoDigests.
func isExcluded(img image.Summary, patterns []string) bool {
	for _, p := range patterns {
		if isWildcardPattern(p) {
			if matchesWildcard(img, p) {
				return true
			}
			continue
		}

		patternNamed, err := reference.ParseNormalizedNamed(p)
		if err != nil {
			// Unparseable pattern (kept raw by parseExclusionPatterns):
			// fall back to colon-heuristic for backward compatibility.
			for _, tag := range img.RepoTags {
				tagNamed, err := reference.ParseNormalizedNamed(tag)
				if err != nil {
					continue
				}
				if strings.Contains(p, ":") {
					if reference.FamiliarString(tagNamed) == p {
						return true
					}
				} else if reference.FamiliarName(tagNamed) == p {
					return true
				}
			}
			continue
		}

		if _, ok := patternNamed.(reference.Canonical); ok {
			// Digest reference: match against RepoDigests for an exact
			// content-address match.
			for _, d := range img.RepoDigests {
				digestNamed, err := reference.ParseNormalizedNamed(d)
				if err != nil {
					continue
				}
				if reference.FamiliarString(digestNamed) == p {
					return true
				}
			}
		} else if _, ok := patternNamed.(reference.Tagged); ok {
			// Pattern has an explicit tag: require exact full-reference match.
			for _, tag := range img.RepoTags {
				tagNamed, err := reference.ParseNormalizedNamed(tag)
				if err != nil {
					continue
				}
				if reference.FamiliarString(tagNamed) == p {
					return true
				}
			}
		} else {
			// Pattern has no tag and no digest: match any tag of the same
			// repository.
			repo := reference.FamiliarName(patternNamed)
			for _, tag := range img.RepoTags {
				tagNamed, err := reference.ParseNormalizedNamed(tag)
				if err != nil {
					continue
				}
				if reference.FamiliarName(tagNamed) == repo {
					return true
				}
			}
		}
	}
	return false
}

func matchesWildcard(img image.Summary, pattern string) bool {
	for _, tag := range img.RepoTags {
		tagNamed, err := reference.ParseNormalizedNamed(tag)
		if err != nil {
			continue
		}
		if matched, err := reference.FamiliarMatch(pattern, tagNamed); err == nil && matched {
			return true
		}
	}
	return false
}

func trimDockerHubPrefix(pattern string) string {
	for _, prefix := range dockerHubPrefixes {
		if trimmed, found := strings.CutPrefix(pattern, prefix); found {
			return trimmed
		}
	}
	return pattern
}

// oldImageCandidates returns images eligible for forced age-based removal.
//
// An image is a candidate if ALL of the following hold:
//   - it is not in use by any container (running or stopped)
//   - it is not excluded by imagePatterns or repoPatterns
//   - its Created timestamp is older than maxAge relative to now
//
// Note: image.Summary.Created reflects the image build time, not the pull
// time. A freshly pulled old image may therefore be removed sooner than
// expected. This is documented and accepted for v1.
func oldImageCandidates(
	now time.Time,
	imgs []image.Summary,
	inUse map[string]bool,
	patterns []string,
	maxAge time.Duration,
) []image.Summary {
	cutoff := now.Add(-maxAge).Unix()
	var out []image.Summary
	for _, img := range imgs {
		if inUse[img.ID] {
			continue
		}
		if isExcluded(img, patterns) {
			continue
		}
		if img.Created >= cutoff {
			continue
		}
		out = append(out, img)
	}
	return out
}

// clearSpaceCandidates returns images eligible for threshold-driven removal.
//
// An image is a candidate if ALL of the following hold:
//   - it was not already removed by old-image cleanup (alreadyRemoved)
//   - it is not in use by any container (running or stopped)
//   - it is not excluded by imagePatterns or repoPatterns
//   - its Created timestamp is older than minAge relative to now
//
// Results are sorted oldest-Created first so that the stalest images are
// removed first when MaximumImagesPerInterval truncates the list.
func clearSpaceCandidates(
	now time.Time,
	imgs []image.Summary,
	inUse map[string]bool,
	patterns []string,
	minAge time.Duration,
	alreadyRemoved map[string]bool,
) []image.Summary {
	cutoff := now.Add(-minAge).Unix()
	var out []image.Summary
	for _, img := range imgs {
		if alreadyRemoved[img.ID] {
			continue
		}
		if inUse[img.ID] {
			continue
		}
		if isExcluded(img, patterns) {
			continue
		}
		if img.Created >= cutoff {
			continue
		}
		out = append(out, img)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Created < out[j].Created
	})
	return out
}
