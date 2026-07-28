package githosts

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestIsGitHubPrimaryRateLimited(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "RATE_LIMITED type", body: `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`, want: true},
		{name: "RATE_LIMIT type", body: `{"errors":[{"type":"RATE_LIMIT","message":"API rate limit already exceeded for user ID 1"}]}`, want: true},
		{name: "message only", body: `{"errors":[{"type":"SOMETHING","message":"You have exceeded a secondary rate limit"}]}`, want: true},
		{name: "not found is not rate limit", body: `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve"}]}`, want: false},
		{name: "clean response", body: `{"data":{"viewer":{"repositories":{"edges":[]}}}}`, want: false},
		{name: "invalid json", body: `not json`, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitHubPrimaryRateLimited(tc.body); got != tc.want {
				t.Fatalf("isGitHubPrimaryRateLimited(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestGitHubRateLimitWait(t *testing.T) {
	t.Run("uses X-RateLimit-Reset", func(t *testing.T) {
		reset := time.Now().Add(90 * time.Second).Unix()
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))

		wait, resetAt := gitHubRateLimitWait(resp)
		if resetAt.Unix() != reset {
			t.Fatalf("resetAt = %d, want %d", resetAt.Unix(), reset)
		}

		if wait < 80*time.Second || wait > 90*time.Second {
			t.Fatalf("wait = %s, want ~90s", wait)
		}
	})

	t.Run("falls back to Retry-After", func(t *testing.T) {
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("Retry-After", "45")

		wait, _ := gitHubRateLimitWait(resp)
		if wait != 45*time.Second {
			t.Fatalf("wait = %s, want 45s", wait)
		}
	})

	t.Run("no headers uses fallback", func(t *testing.T) {
		wait, _ := gitHubRateLimitWait(&http.Response{Header: http.Header{}})
		if wait != rateLimitFallbackWait {
			t.Fatalf("wait = %s, want %s", wait, rateLimitFallbackWait)
		}
	})
}

func TestGitHubRateLimitMaxWait(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		t.Setenv(githubEnvVarRateLimitMaxWait, "")

		if got := gitHubRateLimitMaxWait(); got != defaultGitHubRateLimitMaxWait {
			t.Fatalf("got %s, want default %s", got, defaultGitHubRateLimitMaxWait)
		}
	})

	t.Run("override in seconds", func(t *testing.T) {
		t.Setenv(githubEnvVarRateLimitMaxWait, "120")

		if got := gitHubRateLimitMaxWait(); got != 120*time.Second {
			t.Fatalf("got %s, want 120s", got)
		}
	})

	t.Run("zero disables waiting", func(t *testing.T) {
		t.Setenv(githubEnvVarRateLimitMaxWait, "0")

		if got := gitHubRateLimitMaxWait(); got != 0 {
			t.Fatalf("got %s, want 0", got)
		}
	})

	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv(githubEnvVarRateLimitMaxWait, "abc")

		if got := gitHubRateLimitMaxWait(); got != defaultGitHubRateLimitMaxWait {
			t.Fatalf("got %s, want default", got)
		}
	})
}
