package githosts

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestCodebergListerHost points a Codeberg host at mux, served under
// /api/v1 as Forgejo serves it.
func newTestCodebergListerHost(t *testing.T, mux http.Handler, limitUserOwned bool) *CodebergHost {
	t.Helper()

	host, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient:     testHTTPClient(),
		APIURL:         mockServer(t, mux).URL + "/api/v1",
		Token:          "test-cb-token",
		LimitUserOwned: limitUserOwned,
	})
	require.NoError(t, err)

	return host
}

func TestCodebergListOwnReposUsesUserRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-cb-token")
		writeRaw(w, "["+giteaRepoJSON("soba", "sobaOne", false, false, false, false, false, 7)+","+
			giteaRepoJSON("other", "shared", true, false, false, true, false, 0)+"]")
	})
	mux.HandleFunc("/api/v1/user", func(http.ResponseWriter, *http.Request) {
		t.Error("the user must not be looked up unless LimitUserOwned is set")
	})

	repos, err := newTestCodebergListerHost(t, mux, false).ListOwnRepos(context.Background())
	require.NoError(t, err)

	require.Equal(t, []Repository{
		{
			Owner:             "soba",
			Name:              "sobaOne",
			PathWithNamespace: "soba/sobaOne",
			Domain:            "gitea.example.com",
			CloneURL:          "https://gitea.example.com/soba/sobaOne.git",
			SSHURL:            "git@gitea.example.com:soba/sobaOne.git",
			Auth:              BasicAuth{User: giteaCloneUser, Password: "test-cb-token"},
			SizeKB:            7,
		},
		{
			Owner:             "other",
			Name:              "shared",
			PathWithNamespace: "other/shared",
			Domain:            "gitea.example.com",
			CloneURL:          "https://gitea.example.com/other/shared.git",
			SSHURL:            "git@gitea.example.com:other/shared.git",
			Auth:              BasicAuth{User: giteaCloneUser, Password: "test-cb-token"},
			IsFork:            true,
			IsPrivate:         true,
		},
	}, repos)
}

func TestCodebergListOwnReposLimitUserOwned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"login":"soba"}`)
	})
	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, "["+giteaRepoJSON("Soba", "sobaOne", false, false, false, false, false, 0)+","+
			giteaRepoJSON("other", "shared", false, false, false, false, false, 0)+"]")
	})

	repos, err := newTestCodebergListerHost(t, mux, true).ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "Soba/sobaOne", repos[0].PathWithNamespace, "owners are compared case-insensitively")
}

func TestCodebergListOwnReposNeedsToken(t *testing.T) {
	host := newTestCodebergListerHost(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made without a token")
	}), false)
	host.Token = ""

	_, err := host.ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "Codeberg token not provided")
}

func TestCodebergListUserAndOrgRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/users/soba/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, "["+giteaRepoJSON("soba", "sobaOne", false, false, false, false, false, 0)+"]")
	})
	mux.HandleFunc("/api/v1/orgs/acme/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, "["+giteaRepoJSON("acme", "tool", false, true, false, false, false, 0)+"]")
	})

	host := newTestCodebergListerHost(t, mux, false)
	ctx := context.Background()

	repos, err := host.ListUserRepos(ctx, "soba")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "soba/sobaOne", repos[0].PathWithNamespace)

	repos, err = host.ListOrgRepos(ctx, "acme")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.True(t, repos[0].IsArchived)

	_, err = host.ListOrgRepos(ctx, "missing")
	require.ErrorContains(t, err, "Codeberg organization missing repositories not found (HTTP 404)")
}

func TestCodebergListOrgMembers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/acme/members", func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-cb-token")
		writeRaw(w, `[{"login":"alice"},{"login":"bob"}]`)
	})

	members, err := newTestCodebergListerHost(t, mux, false).ListOrgMembers(context.Background(), "acme")
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, members)
}
