package githosts

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// giteaListerRepoJSON is a repository as Gitea and Forgejo return it, with
// every field the listers read.
const giteaListerRepoJSON = `{"name":"%s","full_name":"%s/%s","owner":{"login":"%s"},
	"clone_url":"https://gitea.example.com/%s/%s.git","ssh_url":"git@gitea.example.com:%s/%s.git",
	"fork":%t,"archived":%t,"empty":%t,"private":%t,"internal":%t,"size":%d}`

// giteaRepoJSON renders giteaListerRepoJSON for owner/name with the given
// flags and size in KiB.
func giteaRepoJSON(owner, name string, fork, archived, empty, private, internal bool, size int) string {
	return fmt.Sprintf(giteaListerRepoJSON, name, owner, name, owner, owner, name, owner, name,
		fork, archived, empty, private, internal, size)
}

// requireGiteaToken fails the test unless r carries the token header.
func requireGiteaToken(t *testing.T, r *http.Request, token string) {
	t.Helper()

	require.Equal(t, AuthPrefixToken+token, r.Header.Get(HeaderAuthorization))
}

func TestGiteaListOwnReposListsEveryUsersRepos(t *testing.T) {
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-gitea-token")
		require.Equal(t, "50", r.URL.Query().Get("limit"))

		if r.URL.Query().Get("page") == "2" {
			writeRaw(w, `[{"login":"bob"}]`)

			return
		}

		w.Header().Set("Link", `<`+srvURL+`/api/v1/admin/users?limit=50&page=2>; rel="next"`)
		writeRaw(w, `[{"login":"alice"}]`)
	})
	mux.HandleFunc("/api/v1/users/alice/repos", func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-gitea-token")
		writeRaw(w, "["+giteaRepoJSON("alice", "plain", false, false, false, false, false, 0)+"]")
	})
	mux.HandleFunc("/api/v1/users/bob/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, "["+giteaRepoJSON("bob", "flagged", true, true, true, true, false, 1234)+"]")
	})

	srv := mockServer(t, mux)
	srvURL = srv.URL

	repos, err := newTestGiteaHost(t, srv.URL).ListOwnRepos(context.Background())
	require.NoError(t, err)

	auth := BasicAuth{User: giteaCloneUser, Password: "test-gitea-token"}

	require.Equal(t, []Repository{
		{
			Owner:             "alice",
			Name:              "plain",
			PathWithNamespace: "alice/plain",
			Domain:            "gitea.example.com",
			CloneURL:          "https://gitea.example.com/alice/plain.git",
			SSHURL:            "git@gitea.example.com:alice/plain.git",
			Auth:              auth,
		},
		{
			Owner:             "bob",
			Name:              "flagged",
			PathWithNamespace: "bob/flagged",
			Domain:            "gitea.example.com",
			CloneURL:          "https://gitea.example.com/bob/flagged.git",
			SSHURL:            "git@gitea.example.com:bob/flagged.git",
			Auth:              auth,
			IsFork:            true,
			IsArchived:        true,
			IsEmpty:           true,
			IsPrivate:         true,
			SizeKB:            1234,
		},
	}, repos)
}

func TestGiteaListOwnReposNeedsAdmin(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/users", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
	}))

	_, err := newTestGiteaHost(t, srv.URL).ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "HTTP 403")
}

func TestGiteaListOwnReposNeedsToken(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made without a token")
	}))

	host := newTestGiteaHost(t, srv.URL)
	host.Token = ""

	_, err := host.ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "token not provided")
}

func TestGiteaListUserReposEscapesAndReportsNotFound(t *testing.T) {
	var gotPath string

	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()

		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := newTestGiteaHost(t, srv.URL).ListUserRepos(context.Background(), "no/body")
	require.ErrorContains(t, err, "not found (HTTP 404)")
	require.Equal(t, "/api/v1/users/no%2Fbody/repos", gotPath)
}

func TestGiteaListOrgRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/acme/repos", func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-gitea-token")
		writeRaw(w, "["+giteaRepoJSON("acme", "pub", false, false, false, false, false, 10)+","+
			giteaRepoJSON("acme", "int", false, false, false, false, true, 20)+"]")
	})

	repos, err := newTestGiteaHost(t, mockServer(t, mux).URL).ListOrgRepos(context.Background(), "acme")
	require.NoError(t, err)
	require.Len(t, repos, 2)

	require.Equal(t, "acme/pub", repos[0].PathWithNamespace)
	require.Equal(t, "acme", repos[0].Owner)
	require.False(t, repos[0].IsPrivate)
	require.Equal(t, int64(10), repos[0].SizeKB)
	require.True(t, repos[1].IsPrivate, "internal repositories are not public")
	require.NotContains(t, repos[0].CloneURL, "@", "clone URL must not carry credentials")
}

func TestGiteaListOrgMembersFollowsLinks(t *testing.T) {
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/acme/members", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeRaw(w, `[{"login":"bob"}]`)

			return
		}

		w.Header().Set("Link", `<`+srvURL+`/api/v1/orgs/acme/members?limit=50&page=2>; rel="next"`)
		writeRaw(w, `[{"login":"alice"}]`)
	})

	srv := mockServer(t, mux)
	srvURL = srv.URL

	members, err := newTestGiteaHost(t, srv.URL).ListOrgMembers(context.Background(), "acme")
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, members)
}

func TestGiteaListerWithoutToken(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header[HeaderAuthorization]; ok {
			t.Errorf("%s was requested with an Authorization header", r.URL.Path)
		}

		writeRaw(w, "["+giteaRepoJSON("acme", "pub", false, false, false, false, false, 0)+"]")
	}))

	host := newTestGiteaHost(t, srv.URL)
	host.Token = ""

	repos, err := host.ListOrgRepos(context.Background(), "acme")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, BasicAuth{}, repos[0].Auth, "public repositories are cloned without credentials")
}

func TestGiteaListerStripsTokenNewline(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireGiteaToken(t, r, "test-gitea-token")
		writeRaw(w, "["+giteaRepoJSON("acme", "pub", false, false, false, false, false, 0)+"]")
	}))

	host := newTestGiteaHost(t, srv.URL)
	host.Token = "test-gitea-token\n"

	repos, err := host.ListOrgRepos(context.Background(), "acme")
	require.NoError(t, err)
	require.Equal(t, "test-gitea-token", repos[0].Auth.Password)
}

func TestGiteaListerDomainFallsBackToAPIHost(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[{"name":"x","full_name":"acme/x","owner":{"login":"acme"}}]`)
	}))

	repos, err := newTestGiteaHost(t, srv.URL).ListOrgRepos(context.Background(), "acme")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, strings.TrimPrefix(srv.URL, "http://"), repos[0].Domain)
}

func TestGiteaListerHonoursCancelledContext(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[]`)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestGiteaHost(t, srv.URL).ListOrgRepos(ctx, "acme")
	require.Error(t, err)
}
