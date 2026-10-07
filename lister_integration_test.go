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
	repos, err := gl.ListOrgRepos(ctx, "go-soba-test")
	require.NoError(t, err)

	for _, name := range []string{"soba-sub-project-one", "soba-sub-project-two"} {
		repo, ok := findRepository(repos, "go-soba-test/soba-sub/"+name)
		require.True(t, ok, "%s should be listed", name)
		require.Equal(t, gitLabDomain, repo.Domain)
		requireListedCloneable(t, repo)
	}

	members, err := gl.ListOrgMembers(ctx, "go-soba-test")
	require.NoError(t, err)
	require.NotEmpty(t, members)

	own, err := gl.ListOwnRepos(ctx)
	require.NoError(t, err)

	_, ok := findRepository(own, "go-soba-test/soba-sub/soba-sub-project-one")
	require.True(t, ok, "the token's own listing should include the group's projects")
}

// TestSourcehutListerIntegration uses the fixtures of sourcehut_test.go:
// jonhadfield's public sobaOne and private sobaTwo. SourceHut has no
// organisations, and Backup clones only public repositories, with the token
// as the username and no password, so requireListedCloneable does not apply.
func TestSourcehutListerIntegration(t *testing.T) {
	token := os.Getenv(sourcehutEnvVarToken)
	if token == "" {
		t.Skip(msgSkipSourcehutTokenMissing)
	}

	ctx := context.Background()

	sh, err := NewSourcehutHost(NewSourcehutHostInput{APIURL: sourcehutAPIURL, PersonalAccessToken: token})
	require.NoError(t, err)

	own, err := sh.ListOwnRepos(ctx)
	require.NoError(t, err)

	public, ok := findRepository(own, "jonhadfield/sobaOne")
	require.True(t, ok, "the own listing should include the public sobaOne")
	require.False(t, public.IsPrivate)
	require.False(t, public.IsEmpty)
	require.Equal(t, "jonhadfield", public.Owner)
	require.Equal(t, sourcehutDomain, public.Domain)
	require.NotContains(t, public.CloneURL, "@", "clone URL must not carry credentials")

	refs, err := getRemoteRefs(ctx, urlWithBasicAuthURL(public.CloneURL, public.Auth.User, public.Auth.Password))
	require.NoError(t, err, "listed credentials should read %s", public.PathWithNamespace)
	require.NotEmpty(t, refs)

	_, ok = findRepository(own, "jonhadfield/sobaTwo")
	require.False(t, ok, "the own listing should leave out the private sobaTwo, as Backup does")

	user, err := sh.ListUserRepos(ctx, "~jonhadfield")
	require.NoError(t, err)

	_, ok = findRepository(user, "jonhadfield/sobaOne")
	require.True(t, ok, "the user listing should include sobaOne")

	private, ok := findRepository(user, "jonhadfield/sobaTwo")
	require.True(t, ok, "the token's own user listing should include the private sobaTwo")
	require.True(t, private.IsPrivate)

	_, err = sh.ListUserRepos(ctx, "this-user-does-not-exist-githosts-utils")
	require.Error(t, err)

	_, err = sh.ListOrgRepos(ctx, "jonhadfield")
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = sh.ListOrgMembers(ctx, "jonhadfield")
	require.ErrorIs(t, err, ErrNotSupported)
}

func TestBitbucketListerIntegration(t *testing.T) {
	email := os.Getenv(bitbucketEnvVarEmail)
	token := os.Getenv(bitbucketEnvVarAPIToken)

	if email == "" || token == "" {
		t.Skip("Skipping Bitbucket lister test as BITBUCKET_EMAIL or BITBUCKET_API_TOKEN is missing")
	}

	ctx := context.Background()

	bb, err := NewBitBucketHost(NewBitBucketHostInput{
		APIURL:   bitbucketAPIURL,
		AuthType: AuthTypeBitbucketAPIToken,
		Email:    email,
		APIToken: token,
	})
	require.NoError(t, err)

	repos, err := bb.ListOrgRepos(ctx, "teamsoba")
	require.NoError(t, err)

	repo, ok := findRepository(repos, "teamsoba/teamsobarepoone")
	require.True(t, ok, "teamsoba/teamsobarepoone should be listed")
	require.Equal(t, "teamsoba", repo.Owner)
	require.Equal(t, bitbucketDomain, repo.Domain)
	require.NotEmpty(t, repo.SSHURL)
	requireListedCloneable(t, repo)

	userRepos, err := bb.ListUserRepos(ctx, "go-soba")
	require.NoError(t, err)

	_, ok = findRepository(userRepos, "go-soba/repo0")
	require.True(t, ok, "go-soba/repo0 should be listed")

	members, err := bb.ListOrgMembers(ctx, "teamsoba")
	require.NoError(t, err)
	require.NotEmpty(t, members)

	// members are UUIDs, which ListUserRepos must accept as a workspace
	require.True(t, strings.HasPrefix(members[0], "{"), "members should be UUIDs in braces")

	_, err = bb.ListUserRepos(ctx, members[0])
	require.NoError(t, err, "a member's UUID should list their personal workspace")

	own, err := bb.ListOwnRepos(ctx)
	require.NoError(t, err)

	for _, path := range []string{"go-soba/repo0", "teamsoba/teamsobarepoone"} {
		_, ok = findRepository(own, path)
		require.True(t, ok, "the token's own listing should include %s, as Backup does", path)
	}

	_, err = bb.ListOrgRepos(ctx, "this-workspace-does-not-exist-githosts-utils")
	require.Error(t, err)
}

func TestAzureDevOpsListerIntegration(t *testing.T) {
	userName := getEnvOrFile(envAzureDevOpsUserName)
	pat := getEnvOrFile("AZURE_DEVOPS_PAT")
	org := getEnvOrFile("AZURE_DEVOPS_ORGS")

	if userName == "" || pat == "" || org == "" {
		t.Skip("Skipping Azure DevOps lister test as AZURE_DEVOPS_USERNAME, AZURE_DEVOPS_PAT or AZURE_DEVOPS_ORGS is missing")
	}

	ctx := context.Background()

	ad := &AzureDevOpsHost{UserName: userName, PAT: pat, Orgs: []string{org}}

	repos, err := ad.ListOrgRepos(ctx, org)
	require.NoError(t, err)

	for _, name := range []string{"soba-test-one", "soba-test-two"} {
		repo, ok := findRepository(repos, org+"/"+name+"/"+name)
		require.True(t, ok, "%s should be listed", name)
		require.True(t, strings.EqualFold(org, repo.Owner))
		require.Equal(t, azureDevOpsDomain, repo.Domain)
		require.NotEmpty(t, repo.SSHURL)
		requireListedCloneable(t, repo)
	}

	own, err := ad.ListOwnRepos(ctx)
	require.NoError(t, err)
	require.Len(t, own, len(repos), "the configured organization's listing is what Backup processes")

	_, err = ad.ListUserRepos(ctx, userName)
	require.ErrorIs(t, err, ErrNotSupported)

	// listing members needs the Member Entitlement Management (Read) scope,
	// which a PAT used only for backups may lack
	members, err := ad.ListOrgMembers(ctx, org)
	if err != nil {
		t.Logf("listing members failed, the PAT may lack the vso.memberentitlementmanagement scope: %v", err)

		return
	}

	require.NotEmpty(t, members)
}

func TestGiteaListerIntegration(t *testing.T) {
	token := os.Getenv(envGiteaToken)
	if token == "" {
		t.Skip(msgSkipGiteaTokenMissing)
	}

	apiURL := os.Getenv(giteaEnvVarAPIUrl)
	if apiURL == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarAPIUrl)
	}

	ctx := context.Background()

	g, err := NewGiteaHost(NewGiteaHostInput{APIURL: apiURL, Token: token})
	require.NoError(t, err)

	userRepos, err := g.ListUserRepos(ctx, "soba-test-rod")
	require.NoError(t, err)

	repo, ok := findRepository(userRepos, "soba-test-rod/soba-test-rod-repo-one")
	require.True(t, ok, "soba-test-rod-repo-one should be listed")
	require.Equal(t, "soba-test-rod", repo.Owner)
	requireListedCloneable(t, repo)

	orgRepos, err := g.ListOrgRepos(ctx, "soba-org-one")
	require.NoError(t, err)

	repo, ok = findRepository(orgRepos, "soba-org-one/soba-org-one-repo-one")
	require.True(t, ok, "soba-org-one-repo-one should be listed")
	requireListedCloneable(t, repo)

	_, err = g.ListOrgMembers(ctx, "soba-org-one")
	require.NoError(t, err)

	// the token must belong to a site administrator, as Backup's does
	own, err := g.ListOwnRepos(ctx)
	require.NoError(t, err)

	_, ok = findRepository(own, "soba-test-rod/soba-test-rod-repo-one")
	require.True(t, ok, "the instance-wide listing should include every user's repositories")

	_, err = g.ListOrgRepos(ctx, "this-org-does-not-exist-githosts-utils")
	require.ErrorContains(t, err, "not found")
}

func TestCodebergListerIntegration(t *testing.T) {
	token := skipWithoutCodebergToken(t)
	ctx := context.Background()

	cb, err := NewCodebergHost(NewCodebergHostInput{Token: token})
	require.NoError(t, err)

	own, err := cb.ListOwnRepos(ctx)
	require.NoError(t, err)

	for _, name := range []string{codebergTestPublicRepo, codebergTestPrivRepo} {
		repo, ok := findRepository(own, codebergTestUser+"/"+name)
		require.True(t, ok, "%s should be listed", name)
		require.Equal(t, codebergTestUser, repo.Owner)
		require.Equal(t, "codeberg.org", repo.Domain)
	}

	// sobaTwo is private, so reading it proves the listed Auth works
	private, _ := findRepository(own, codebergTestUser+"/"+codebergTestPrivRepo)
	require.True(t, private.IsPrivate)
	requireListedCloneable(t, private)

	userRepos, err := cb.ListUserRepos(ctx, codebergTestUser)
	require.NoError(t, err)

	_, ok := findRepository(userRepos, codebergTestUser+"/"+codebergTestPublicRepo)
	require.True(t, ok, "%s should be listed for its owner", codebergTestPublicRepo)

	_, err = cb.ListOrgRepos(ctx, "this-org-does-not-exist-githosts-utils")
	require.ErrorContains(t, err, "not found")
}
