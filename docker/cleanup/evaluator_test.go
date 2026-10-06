package cleanup

import (
	"slices"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/assert"
)

// ---- parseExclusionPatterns ----

func TestParseExclusionPatterns_Normalises(t *testing.T) {
	patterns := []string{
		"nginx:latest",
		"docker.io/library/redis",
		"myregistry.example.com/myapp",
	}
	got := parseExclusionPatterns(patterns)
	// All valid — each should be in normalised familiar form
	assert.Len(t, got, 3)
	assert.Equal(t, "nginx:latest", got[0])
	// reference.FamiliarString does not add :latest to tag-less refs; repo matching
	// (no colon) will check exact repository name equality.
	assert.Equal(t, "redis", got[1])
	assert.Equal(t, "myregistry.example.com/myapp", got[2])
}

func TestParseExclusionPatterns_KeepsInvalidAsIs(t *testing.T) {
	// An invalid reference (e.g. containing uppercase) should be kept as-is.
	patterns := []string{"UPPERCASE_INVALID", "nginx"}
	got := parseExclusionPatterns(patterns)
	assert.Len(t, got, 2)
	assert.Equal(t, "UPPERCASE_INVALID", got[0])
	// "nginx" without a tag normalises to "nginx" (no :latest appended)
	assert.Equal(t, "nginx", got[1])
}

func TestParseExclusionPatterns_Empty(t *testing.T) {
	got := parseExclusionPatterns(nil)
	assert.Empty(t, got)
}

// ---- isExcluded ----

func TestIsExcluded_ExactImageMatch(t *testing.T) {
	img := image.Summary{RepoTags: []string{"nginx:latest"}}
	patterns := parseExclusionPatterns([]string{"nginx:latest"})
	assert.True(t, isExcluded(img, patterns))
}

func TestIsExcluded_RepoMatchAnyTag(t *testing.T) {
	img := image.Summary{RepoTags: []string{"myregistry.example.com/myapp:v1.2.3"}}
	patterns := parseExclusionPatterns([]string{"myregistry.example.com/myapp"})
	assert.True(t, isExcluded(img, patterns))
}

func TestIsExcluded_NoImageMatch(t *testing.T) {
	img := image.Summary{RepoTags: []string{"redis:7"}}
	patterns := parseExclusionPatterns([]string{"nginx:latest"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_BareNameMatchesAllTags(t *testing.T) {
	img := image.Summary{RepoTags: []string{"nginx:1.25"}}
	patterns := parseExclusionPatterns([]string{"nginx"})
	assert.True(t, isExcluded(img, patterns))
}

func TestIsExcluded_BareNameDoesNotMatchDifferentRepo(t *testing.T) {
	// "nginx" must not match "nginx-proxy" — exact repo name, no org-level prefix
	img := image.Summary{RepoTags: []string{"myregistry.example.com/team/app:latest"}}
	patterns := parseExclusionPatterns([]string{"myregistry.example.com/team"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_BareNameDoesNotMatchSimilarlyNamedRepo(t *testing.T) {
	// "nginx" must not match "nginx-proxy:latest" — the key safety guarantee:
	// a bare repo name matches only that exact repository, not others that share its prefix.
	img := image.Summary{RepoTags: []string{"nginx-proxy:latest"}}
	patterns := parseExclusionPatterns([]string{"nginx"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_ExactTagDoesNotProtectOtherTags(t *testing.T) {
	// "nginx:1.25.3" must not protect "nginx:latest" — exact tag match is exclusive.
	img := image.Summary{RepoTags: []string{"nginx:latest"}}
	patterns := parseExclusionPatterns([]string{"nginx:1.25.3"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_MultipleRepoTagsOneMatches(t *testing.T) {
	// An image carrying multiple tags is excluded if any one of its tags matches.
	img := image.Summary{RepoTags: []string{"nginx:1.25.3", "nginx:stable"}}
	patterns := parseExclusionPatterns([]string{"nginx:stable"})
	assert.True(t, isExcluded(img, patterns))
}

func TestIsExcluded_NoRepoMatch(t *testing.T) {
	img := image.Summary{RepoTags: []string{"redis:7"}}
	patterns := parseExclusionPatterns([]string{"nginx"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_DanglingImage(t *testing.T) {
	// Dangling images have no RepoTags — they must never be excluded.
	// Without a tag to match against, exclusion patterns cannot apply.
	img := image.Summary{RepoTags: []string{}}
	patterns := parseExclusionPatterns([]string{"nginx:latest", "nginx"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_BareNameDoesNotMatchDifferentRegistry(t *testing.T) {
	// "nginx" resolves to the Docker Hub official library image.
	// It must NOT protect an image with the same leaf name from another registry.
	img := image.Summary{RepoTags: []string{"harbor.example.com/library/nginx:latest"}}
	patterns := parseExclusionPatterns([]string{"nginx"})
	assert.False(t, isExcluded(img, patterns),
		"bare 'nginx' should only match docker.io/library/nginx, not harbor.example.com/library/nginx")
}

func TestIsExcluded_FullRegistryPathProtectsAllTags(t *testing.T) {
	// A full registry path without a tag protects all tags of that repository.
	latest := image.Summary{RepoTags: []string{"harbor.example.com/myorg/nginx:latest"}}
	v1 := image.Summary{RepoTags: []string{"harbor.example.com/myorg/nginx:1.25"}}
	other := image.Summary{RepoTags: []string{"harbor.example.com/myorg/redis:7"}}
	patterns := parseExclusionPatterns([]string{"harbor.example.com/myorg/nginx"})
	assert.True(t, isExcluded(latest, patterns))
	assert.True(t, isExcluded(v1, patterns))
	assert.False(t, isExcluded(other, patterns))
}

func TestIsExcluded_RegistryWithPortNoTagMatchesAllTags(t *testing.T) {
	// "myregistry:9000/myapp" contains ":" due to the port, not a tag.
	// It should protect all tags of that repository, not fail to match.
	v1 := image.Summary{RepoTags: []string{"myregistry:9000/myapp:v1.0"}}
	latest := image.Summary{RepoTags: []string{"myregistry:9000/myapp:latest"}}
	other := image.Summary{RepoTags: []string{"myregistry:9000/otherapp:latest"}}
	patterns := parseExclusionPatterns([]string{"myregistry:9000/myapp"})
	assert.True(t, isExcluded(v1, patterns), "registry:port/repo without tag should match any tag")
	assert.True(t, isExcluded(latest, patterns), "registry:port/repo without tag should match :latest")
	assert.False(t, isExcluded(other, patterns), "registry:port/repo should not match a different repo")
}

func TestIsExcluded_RegistryWithPortAndTagExactMatch(t *testing.T) {
	// "myregistry:9000/myapp:v1.0" specifies both a port and a tag.
	// Only the exact tag should be protected.
	v1 := image.Summary{RepoTags: []string{"myregistry:9000/myapp:v1.0"}}
	latest := image.Summary{RepoTags: []string{"myregistry:9000/myapp:latest"}}
	patterns := parseExclusionPatterns([]string{"myregistry:9000/myapp:v1.0"})
	assert.True(t, isExcluded(v1, patterns), "exact registry:port/repo:tag should match")
	assert.False(t, isExcluded(latest, patterns), "exact registry:port/repo:tag should not match other tags")
}

func TestIsExcluded_DigestPatternMatchesExactDigest(t *testing.T) {
	// A digest pattern must match against RepoDigests, not RepoTags.
	img := image.Summary{
		RepoTags:    []string{"nginx:latest"},
		RepoDigests: []string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	patterns := parseExclusionPatterns([]string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	assert.True(t, isExcluded(img, patterns))
}

func TestIsExcluded_DigestPatternDoesNotMatchDifferentDigest(t *testing.T) {
	img := image.Summary{
		RepoTags:    []string{"nginx:latest"},
		RepoDigests: []string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	patterns := parseExclusionPatterns([]string{"nginx@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	assert.False(t, isExcluded(img, patterns))
}

func TestIsExcluded_DigestPatternDoesNotMatchAllTagsOfRepo(t *testing.T) {
	// A digest pattern must NOT fall through to bare-repo matching.
	// Only the image with the matching digest is protected, not all nginx images.
	imgWithDigest := image.Summary{
		RepoTags:    []string{"nginx:1.25"},
		RepoDigests: []string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	imgDifferentDigest := image.Summary{
		RepoTags:    []string{"nginx:latest"},
		RepoDigests: []string{"nginx@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	patterns := parseExclusionPatterns([]string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	assert.True(t, isExcluded(imgWithDigest, patterns))
	assert.False(t, isExcluded(imgDifferentDigest, patterns),
		"digest pattern must not protect a different image that shares the repo name")
}

func TestIsExcluded_DigestPatternMatchesDanglingImage(t *testing.T) {
	// A dangling image (no RepoTags) can still be excluded via a digest pattern.
	dangling := image.Summary{
		RepoTags:    []string{},
		RepoDigests: []string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	patterns := parseExclusionPatterns([]string{"nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	assert.True(t, isExcluded(dangling, patterns))
}

func TestIsExcluded_ExplicitDockerHubPathNormalisesToFamiliarForm(t *testing.T) {
	// "docker.io/library/nginx" is the canonical form of the Docker Hub official nginx image.
	// After normalisation it becomes the familiar "nginx", so it is equivalent to specifying
	// just "nginx" as the exclusion pattern.
	img := image.Summary{RepoTags: []string{"nginx:latest"}}
	patterns := parseExclusionPatterns([]string{"docker.io/library/nginx"})
	assert.True(t, isExcluded(img, patterns),
		"docker.io/library/nginx should normalise to nginx and match nginx:latest")
}

// ---- existing exclusion patterns (regression) ----

const regressionDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// TestIsExcluded_ExistingPatternsUnchanged pins the result of every
// pattern/image pair below to what the matcher returned before wildcard
// support was added, so non-wildcard exclusions keep behaving exactly as before.
func TestIsExcluded_ExistingPatternsUnchanged(t *testing.T) {
	images := map[string]image.Summary{
		"nginx:latest":         {RepoTags: []string{"nginx:latest"}},
		"nginx:1.25.3":         {RepoTags: []string{"nginx:1.25.3"}},
		"nginx-proxy:latest":   {RepoTags: []string{"nginx-proxy:latest"}},
		"myorg/app:v1":         {RepoTags: []string{"myorg/app:v1"}},
		"myorg/app:v2":         {RepoTags: []string{"myorg/app:v2"}},
		"harbor/myorg/nginx":   {RepoTags: []string{"harbor.example.com/myorg/nginx:1.25"}},
		"harbor/library/nginx": {RepoTags: []string{"harbor.example.com/library/nginx:latest"}},
		"myregistry:5000/app":  {RepoTags: []string{"myregistry:5000/app:v1"}},
		"localhost/app":        {RepoTags: []string{"localhost/app:latest"}},
		"localhost:5000/app":   {RepoTags: []string{"localhost:5000/app:dev"}},
		"my-custom-image":      {RepoTags: []string{"my-custom-image:v1.2"}},
		"nginx-digest":         {RepoTags: []string{"nginx:stable"}, RepoDigests: []string{"nginx@" + regressionDigest}},
		"dangling-digest":      {RepoDigests: []string{"nginx@" + regressionDigest}},
		"dangling":             {},
	}

	expected := map[string][]string{
		"nginx":                               {"nginx-digest", "nginx:1.25.3", "nginx:latest"},
		"nginx:latest":                        {"nginx:latest"},
		"nginx:1.25.3":                        {"nginx:1.25.3"},
		"docker.io/library/nginx":             {"nginx-digest", "nginx:1.25.3", "nginx:latest"},
		"docker.io/library/nginx:latest":      {"nginx:latest"},
		"library/nginx":                       {"nginx-digest", "nginx:1.25.3", "nginx:latest"},
		"myorg/app":                           {"myorg/app:v1", "myorg/app:v2"},
		"myorg/app:v1":                        {"myorg/app:v1"},
		"docker.io/myorg/app":                 {"myorg/app:v1", "myorg/app:v2"},
		"harbor.example.com/myorg/nginx":      {"harbor/myorg/nginx"},
		"harbor.example.com/myorg/nginx:1.25": {"harbor/myorg/nginx"},
		"harbor.example.com/myorg":            {},
		"myregistry:5000/app":                 {"myregistry:5000/app"},
		"myregistry:5000/app:v1":              {"myregistry:5000/app"},
		"localhost/app":                       {"localhost/app"},
		"localhost:5000/app":                  {"localhost:5000/app"},
		"nginx@" + regressionDigest:           {"dangling-digest", "nginx-digest"},
		"my-custom-image":                     {"my-custom-image"},
		"my-custom-image:v1.2":                {"my-custom-image"},
		"UPPERCASE":                           {},
	}

	for pattern, excludedImages := range expected {
		t.Run(pattern, func(t *testing.T) {
			patterns := parseExclusionPatterns([]string{pattern})
			for name, img := range images {
				assert.Equal(t, slices.Contains(excludedImages, name), isExcluded(img, patterns), "image %s", name)
			}
		})
	}
}

// ---- wildcard exclusion patterns ----

func TestParseExclusionPatterns_WildcardStripsDockerHubPrefix(t *testing.T) {
	got := parseExclusionPatterns([]string{
		"my-app-*",
		"docker.io/library/ngin?",
		"docker.io/myorg/*",
		"library/ngin?",
		"harbor.example.com/myorg/*",
		"harbor.example.com/library/*",
	})
	assert.Equal(t, []string{"my-app-*", "ngin?", "myorg/*", "ngin?", "harbor.example.com/myorg/*", "harbor.example.com/library/*"}, got)
}

func TestIsExcluded_WildcardPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		tags     []string
		excluded bool
	}{
		{"prefix matches repository", "my-custom-*", []string{"my-custom-image:v1.2"}, true},
		{"prefix does not match other repository", "my-custom-*", []string{"other-image:v1.2"}, false},
		{"tag glob matches tag", "my-custom-image:v1.*", []string{"my-custom-image:v1.2"}, true},
		{"tag glob does not match other tag", "my-custom-image:v1.*", []string{"my-custom-image:v2.0"}, false},
		{"tag glob does not match other repository", "my-custom-image:v1.*", []string{"my-other-image:v1.2"}, false},
		{"any tag of a repository", "my-custom-image:*", []string{"my-custom-image:latest"}, true},
		{"star crosses the tag separator", "nginx*", []string{"nginx-proxy:latest"}, true},
		{"single character wildcard", "app-v?", []string{"app-v1:latest"}, true},
		{"single character wildcard does not match two characters", "app-v?", []string{"app-v10:latest"}, false},
		{"star does not cross a slash", "*", []string{"myorg/app:v1"}, false},
		{"star matches a single segment name", "*", []string{"nginx:latest"}, true},
		{"namespace glob", "myorg/*", []string{"myorg/app:v1"}, true},
		{"namespace glob does not match another registry", "myorg/*", []string{"harbor.example.com/myorg/app:v1"}, false},
		{"registry glob", "harbor.example.com/myorg/*", []string{"harbor.example.com/myorg/nginx:1.25"}, true},
		{"registry glob does not match Docker Hub", "harbor.example.com/myorg/*", []string{"myorg/nginx:1.25"}, false},
		{"Docker Hub library prefix is ignored", "docker.io/library/ngin?", []string{"nginx:latest"}, true},
		{"Docker Hub prefix is ignored", "docker.io/myorg/*", []string{"myorg/app:v1"}, true},
		{"Docker Hub short prefix is ignored", "docker.io/ngin?", []string{"nginx:latest"}, true},
		{"library prefix is ignored", "library/ngin?", []string{"nginx:latest"}, true},
		{"library prefix does not match another registry", "library/ngin?", []string{"harbor.example.com/library/nginx:latest"}, false},
		{"registry library path is kept", "harbor.example.com/library/*", []string{"harbor.example.com/library/nginx:latest"}, true},
		{"localhost registry glob", "localhost/*", []string{"localhost/app:latest"}, true},
		{"one matching tag is enough", "my-custom-*", []string{"other:v1", "my-custom-image:v1"}, true},
		{"dangling image is never matched", "*", nil, false},
		{"malformed glob never matches", "my-[app*", []string{"my-[app:latest"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := image.Summary{RepoTags: tt.tags}
			patterns := parseExclusionPatterns([]string{tt.pattern})
			assert.Equal(t, tt.excluded, isExcluded(img, patterns))
		})
	}
}

func TestOldImageCandidates_SkipsWildcardExcluded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	old := now.Add(-48 * time.Hour).Unix()
	imgs := []image.Summary{
		makeImg("protected", old, "my-custom-image:v1.2"),
		makeImg("removable", old, "other-image:v1.2"),
	}
	patterns := parseExclusionPatterns([]string{"my-custom-*"})

	got := oldImageCandidates(now, imgs, map[string]bool{}, patterns, 24*time.Hour)

	assert.Len(t, got, 1)
	assert.Equal(t, "removable", got[0].ID)
}

// ---- oldImageCandidates ----

func makeImg(id string, createdUnix int64, tags ...string) image.Summary {
	return image.Summary{
		ID:       id,
		Created:  createdUnix,
		RepoTags: tags,
	}
}

func TestOldImageCandidates_IncludesOldImages(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	maxAge := 30 * 24 * time.Hour

	old := makeImg("sha256:old", now.Add(-31*24*time.Hour).Unix())
	recent := makeImg("sha256:recent", now.Add(-1*time.Hour).Unix())

	candidates := oldImageCandidates(now, []image.Summary{old, recent}, nil, nil, maxAge)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "sha256:old", candidates[0].ID)
}

func TestOldImageCandidates_SkipsInUse(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	maxAge := 30 * 24 * time.Hour

	old := makeImg("sha256:old", now.Add(-31*24*time.Hour).Unix())
	inUse := map[string]bool{"sha256:old": true}

	candidates := oldImageCandidates(now, []image.Summary{old}, inUse, nil, maxAge)
	assert.Empty(t, candidates)
}

func TestOldImageCandidates_SkipsExcluded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	maxAge := 30 * 24 * time.Hour

	old := makeImg("sha256:old", now.Add(-31*24*time.Hour).Unix(), "nginx:latest")
	imagePatterns := parseExclusionPatterns([]string{"nginx:latest"})

	candidates := oldImageCandidates(now, []image.Summary{old}, nil, imagePatterns, maxAge)
	assert.Empty(t, candidates)
}

func TestOldImageCandidates_DoesNotAffectClearSpace(t *testing.T) {
	// oldImageCandidates should not know about clearSpaceCandidates; it simply applies age + exclusions
	now := time.Unix(1_000_000, 0)
	maxAge := 30 * 24 * time.Hour

	imgs := []image.Summary{
		makeImg("sha256:a", now.Add(-40*24*time.Hour).Unix()),
		makeImg("sha256:b", now.Add(-35*24*time.Hour).Unix()),
	}

	candidates := oldImageCandidates(now, imgs, nil, nil, maxAge)
	assert.Len(t, candidates, 2)
}

// ---- clearSpaceCandidates ----

func TestClearSpaceCandidates_ExcludesAlreadyRemoved(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	img := makeImg("sha256:old", now.Add(-48*time.Hour).Unix())
	alreadyRemoved := map[string]bool{"sha256:old": true}

	candidates := clearSpaceCandidates(now, []image.Summary{img}, nil, nil, minAge, alreadyRemoved)
	assert.Empty(t, candidates)
}

func TestClearSpaceCandidates_SkipsInUse(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	img := makeImg("sha256:old", now.Add(-48*time.Hour).Unix())
	inUse := map[string]bool{"sha256:old": true}

	candidates := clearSpaceCandidates(now, []image.Summary{img}, inUse, nil, minAge, nil)
	assert.Empty(t, candidates)
}

func TestClearSpaceCandidates_SkipsExcluded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	img := makeImg("sha256:old", now.Add(-48*time.Hour).Unix(), "redis:7")
	imagePatterns := parseExclusionPatterns([]string{"redis:7"})

	candidates := clearSpaceCandidates(now, []image.Summary{img}, nil, imagePatterns, minAge, nil)
	assert.Empty(t, candidates)
}

func TestClearSpaceCandidates_SkipsTooRecent(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	img := makeImg("sha256:new", now.Add(-1*time.Hour).Unix())
	candidates := clearSpaceCandidates(now, []image.Summary{img}, nil, nil, minAge, nil)
	assert.Empty(t, candidates)
}

func TestClearSpaceCandidates_SortedOldestFirst(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	a := makeImg("sha256:a", now.Add(-72*time.Hour).Unix()) // oldest
	b := makeImg("sha256:b", now.Add(-48*time.Hour).Unix()) // newer
	c := makeImg("sha256:c", now.Add(-96*time.Hour).Unix()) // oldest of all

	candidates := clearSpaceCandidates(now, []image.Summary{a, b, c}, nil, nil, minAge, nil)
	assert.Len(t, candidates, 3)
	assert.Equal(t, "sha256:c", candidates[0].ID) // most stale first
	assert.Equal(t, "sha256:a", candidates[1].ID)
	assert.Equal(t, "sha256:b", candidates[2].ID)
}

func TestClearSpaceCandidates_IncludesDangling(t *testing.T) {
	// Dangling images (no tags) with no exclusions should be candidates.
	now := time.Unix(1_000_000, 0)
	minAge := 24 * time.Hour

	dangling := makeImg("sha256:dangling", now.Add(-48*time.Hour).Unix()) // no tags
	candidates := clearSpaceCandidates(now, []image.Summary{dangling}, nil, nil, minAge, nil)
	assert.Len(t, candidates, 1)
}
