package githosts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxConcurrentFromEnv(t *testing.T) {
	const envVar = "TEST_MAX_CONCURRENT"

	tests := []struct {
		name string
		env  string
		def  int
		want int
	}{
		{name: "unset falls back to the provider default", env: "", def: 5, want: 5},
		{name: "override lowers", env: "2", def: 10, want: 2},
		{name: "override raises", env: "20", def: 5, want: 20},
		{name: "one worker is valid", env: "1", def: 10, want: 1},
		{name: "zero ignored, would stall the backup", env: "0", def: 5, want: 5},
		{name: "negative ignored", env: "-3", def: 5, want: 5},
		{name: "unparsable ignored", env: "lots", def: 5, want: 5},
		{name: "whitespace is not a number", env: " 3 ", def: 5, want: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envVar, tc.env)

			require.Equal(t, tc.want, maxConcurrentFromEnv(envVar, tc.def))
		})
	}
}

// TestProviderMaxConcurrentEnvVars pins each provider to its own environment
// variable and default, so a copy-paste slip cannot leave two providers
// sharing one knob.
func TestProviderMaxConcurrentEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		envVar  string
		def     int
		resolve func() int
	}{
		{
			name:    "github",
			envVar:  githubEnvVarMaxConcurrent,
			def:     defaultMaxConcurrentGitHub,
			resolve: githubMaxConcurrent,
		},
		{
			name:   "gitea",
			envVar: giteaEnvVarMaxConcurrent,
			def:    defaultMaxConcurrentGitLab,
		},
		{
			name:   "gitlab",
			envVar: gitlabEnvVarMaxConcurrent,
			def:    defaultMaxConcurrentGitLab,
		},
		{
			name:   "bitbucket",
			envVar: bitbucketEnvVarMaxConcurrent,
			def:    defaultMaxConcurrentGitLab,
		},
		{
			name:   "azure devops",
			envVar: azureDevOpsEnvVarMaxConcurrent,
			def:    defaultMaxConcurrentOther,
		},
		{
			name:   "sourcehut",
			envVar: sourcehutEnvVarMaxConcurrent,
			def:    defaultMaxConcurrentSourcehut,
		},
		{
			name:   "codeberg",
			envVar: codebergEnvVarMaxConcurrent,
			def:    codebergMaxConcurrency,
		},
	}

	seen := make(map[string]string, len(tests))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEmpty(t, tc.envVar)

			if prior, dup := seen[tc.envVar]; dup {
				t.Fatalf("%s reuses the environment variable %s already used by %s", tc.name, tc.envVar, prior)
			}

			seen[tc.envVar] = tc.name

			resolve := tc.resolve
			if resolve == nil {
				resolve = func() int { return maxConcurrentFromEnv(tc.envVar, tc.def) }
			}

			t.Setenv(tc.envVar, "")
			require.Equal(t, tc.def, resolve(), "unset should fall back to the provider default")

			t.Setenv(tc.envVar, "2")
			require.Equal(t, 2, resolve(), "the provider should honour its own variable")
		})
	}

	require.Len(t, seen, len(tests), "every provider needs a distinct variable")
}
