//go:build integration

package githosts

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireListedCloneable checks that a listed repository carries no
// credentials in its URL and that its Auth is enough to read it, which a
// private repository only allows with valid credentials.
func requireListedCloneable(t *testing.T, repo Repository) {
	t.Helper()

	require.NotContains(t, repo.CloneURL, "@", "clone URL must not carry credentials")
	require.NotEmpty(t, repo.Auth.User)
	require.NotEmpty(t, repo.Auth.Password)

	refs, err := getRemoteRefs(context.Background(), urlWithBasicAuthURL(repo.CloneURL, repo.Auth.User, repo.Auth.Password))
	require.NoError(t, err, "listed credentials should read %s", repo.PathWithNamespace)
	require.NotEmpty(t, refs)
}

func findRepository(repos []Repository, path string) (Repository, bool) {
	for _, r := range repos {
		if strings.EqualFold(r.PathWithNamespace, path) {
			return r, true
		}
	}

	return Repository{}, false
}

func TestGitHubListerIntegration(t *testing.T) {
	token := os.Getenv(envGithubToken)
	if token == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	org := githubTestOrg(t)
	ctx := context.Background()

	gh, err := NewGitHubHost(NewGitHubHostInput{Token: token})
	require.NoError(t, err)

	repos, err := gh.ListOrgRepos(ctx, org)
	require.NoError(t, err)

	for _, name := range []string{"public1", "public2", "private1", "private2"} {
		repo, ok := findRepository(repos, org+"/"+name)
		require.True(t, ok, "%s/%s should be listed", org, name)
		require.True(t, strings.EqualFold(org, repo.Owner))
		require.Equal(t, gitHubDomain, repo.Domain)
	}

	private, _ := findRepository(repos, org+"/private1")
	requireListedCloneable(t, private)

	members, err := gh.ListOrgMembers(ctx, org)
	require.NoError(t, err)
	require.NotEmpty(t, members)

	_, err = gh.ListOwnRepos(ctx)
	require.NoError(t, err)

	_, err = gh.ListOrgRepos(ctx, "this-org-does-not-exist-githosts-utils")
	require.ErrorContains(t, err, "not found")
}

func TestGitLabListerIntegration(t *testing.T) {
	token := os.Getenv(gitlabEnvVarToken)
	if token == "" {
		t.Skip(msgSkipGitLabTokenMissing)
	}

	ctx := context.Background()

	gl, err := NewGitLabHost(NewGitLabHostInput{APIURL: gitlabAPIURL, Token: token})
	require.NoError(t, err)

	// the projects live in a subgroup, so finding them proves subgroups are
	// included in a group's listing
	repos, err := gl.ListOrgRepos(ctx, "soba-test")
	require.NoError(t, err)

	for _, name := range []string{"soba-sub-project-one", "soba-sub-project-two"} {
		repo, ok := findRepository(repos, "soba-test/soba-sub/"+name)
		require.True(t, ok, "%s should be listed", name)
		require.Equal(t, gitLabDomain, repo.Domain)
		requireListedCloneable(t, repo)
	}

	members, err := gl.ListOrgMembers(ctx, "soba-test")
	require.NoError(t, err)
	require.NotEmpty(t, members)

	own, err := gl.ListOwnRepos(ctx)
	require.NoError(t, err)

	_, ok := findRepository(own, "soba-test/soba-sub/soba-sub-project-one")
	require.True(t, ok, "the token's own listing should include the group's projects")
}
