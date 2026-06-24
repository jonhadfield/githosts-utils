package githosts

import (
	"testing"
	"time"
)

func TestGithubRequestTimeout(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset uses default", set: false, want: defaultGitHubRequestTimeout},
		{name: "valid override in seconds", set: true, val: "60", want: 60 * time.Second},
		{name: "empty falls back to default", set: true, val: "", want: defaultGitHubRequestTimeout},
		{name: "zero is ignored", set: true, val: "0", want: defaultGitHubRequestTimeout},
		{name: "negative is ignored", set: true, val: "-5", want: defaultGitHubRequestTimeout},
		{name: "non-numeric is ignored", set: true, val: "abc", want: defaultGitHubRequestTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(githubEnvVarRequestTimeout, tc.val)
			} else {
				// t.Setenv requires a value; ensure the var is cleared instead.
				t.Setenv(githubEnvVarRequestTimeout, "")
			}

			if got := githubRequestTimeout(); got != tc.want {
				t.Fatalf("githubRequestTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}
