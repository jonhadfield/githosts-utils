package githosts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- GitHub ---

// ghFlaggedEdge builds an edge with the owner and flags the lister reports.
func ghFlaggedEdge(owner, name string, fork, archived, empty bool) edge {
	e := ghEdge(owner, name)
	e.Node.Owner.Login = owner
	e.Node.IsFork = fork
	e.Node.IsArchived = archived
	e.Node.IsEmpty = empty

	return e
}

// graphQLQueries records each GraphQL query received and answers with the
// response respond returns for it.
func graphQLQueries(t *testing.T, queries *[]string, respond func(query string) any) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		*queries = append(*queries, req.Query)

		writeJSON(w, respond(req.Query))
	}
}

func newTestGitHubHost(t *testing.T, apiURL string) *GitHubHost {
	t.Helper()

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     apiURL,
		Token:      "test-gh-token",
	})
	require.NoError(t, err)

	return host
}

func TestGitHubListOwnRepos(t *testing.T) {
	var queries []string

	srv := mockServer(t, graphQLQueries(t, &queries, func(string) any {
		resp := githubQueryNamesResponse{}
		resp.Data.Viewer.Repositories.Edges = []edge{
			ghFlaggedEdge("user", "plain", false, false, false),
			ghFlaggedEdge("user", "flagged", true, true, true),
		}

		return resp
	}))

	repos, err := newTestGitHubHost(t, srv.URL).ListOwnRepos(context.Background())
	require.NoError(t, err)

	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "viewer")
	require.Contains(t, queries[0], githubRepoNodeFields)

	require.Equal(t, []Repository{
		{
			Owner:             "user",
			Name:              "plain",
			PathWithNamespace: "user/plain",
			Domain:            gitHubDomain,
			CloneURL:          "https://github.com/user/plain",
			SSHURL:            "git@github.com:user/plain.git",
			Auth:              BasicAuth{User: githubCloneUser, Password: "test-gh-token"},
		},
		{
			Owner:             "user",
			Name:              "flagged",
			PathWithNamespace: "user/flagged",
			Domain:            gitHubDomain,
			CloneURL:          "https://github.com/user/flagged",
			SSHURL:            "git@github.com:user/flagged.git",
			Auth:              BasicAuth{User: githubCloneUser, Password: "test-gh-token"},
			IsFork:            true,
			IsArchived:        true,
			IsEmpty:           true,
		},
	}, repos)
}

func TestGitHubListUserReposPaginatesOwnedRepos(t *testing.T) {
	var queries []string

	srv := mockServer(t, graphQLQueries(t, &queries, func(query string) any {
		resp := githubQueryOrgResponse{}
		if strings.Contains(query, `after: "c1"`) {
			resp.Data.User.Repositories.Edges = []edge{ghFlaggedEdge("someone", "two", false, false, false)}

			return resp
		}

		resp.Data.User.Repositories.Edges = []edge{ghFlaggedEdge("someone", "one", false, false, false)}
		resp.Data.User.Repositories.PageInfo.HasNextPage = true
		resp.Data.User.Repositories.PageInfo.EndCursor = "c1"

		return resp
	}))

	repos, err := newTestGitHubHost(t, srv.URL).ListUserRepos(context.Background(), "someone")
	require.NoError(t, err)

	require.Len(t, queries, 2)

	for _, q := range queries {
		require.Contains(t, q, `user(login: "someone")`)
		require.Contains(t, q, "ownerAffiliations: OWNER",
			"repositories the user only collaborates on must not be attributed to it")
	}

	require.Len(t, repos, 2)
	require.Equal(t, "someone/one", repos[0].PathWithNamespace)
	require.Equal(t, "someone/two", repos[1].PathWithNamespace)
}

func TestGitHubListOrgReposNotFound(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to an Organization."}]}`)
	}))

	_, err := newTestGitHubHost(t, srv.URL).ListOrgRepos(context.Background(), "missing-org")
	require.ErrorContains(t, err, "organization missing-org not found")
}

// TestGitHubListerRejectsInvalidLogins guards the GraphQL queries, which
// interpolate the login: anything that is not a valid login is refused before
// a request is made.
func TestGitHubListerRejectsInvalidLogins(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made for an invalid login")
	}))

	host := newTestGitHubHost(t, srv.URL)
	ctx := context.Background()

	for _, login := range []string{"", `x") { id } #`, "-leading", "has space"} {
		_, err := host.ListUserRepos(ctx, login)
		require.Error(t, err, login)

		_, err = host.ListOrgRepos(ctx, login)
		require.Error(t, err, login)

		_, err = host.ListOrgMembers(ctx, login)
		require.Error(t, err, login)
	}
}

func TestGitHubListOrgMembersPaginates(t *testing.T) {
	var queries []string

	srv := mockServer(t, graphQLQueries(t, &queries, func(query string) any {
		if strings.Contains(query, `after: "m1"`) {
			return json.RawMessage(`{"data":{"organization":{"membersWithRole":{"nodes":[{"login":"bob"}],"pageInfo":{"endCursor":"m2","hasNextPage":false}}}}}`)
		}

		return json.RawMessage(`{"data":{"organization":{"membersWithRole":{"nodes":[{"login":"alice"}],"pageInfo":{"endCursor":"m1","hasNextPage":true}}}}}`)
	}))

	members, err := newTestGitHubHost(t, srv.URL).ListOrgMembers(context.Background(), "acme")
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, members)
	require.Len(t, queries, 2)
	require.Contains(t, queries[0], `organization(login: "acme")`)
}

func TestGitHubListerHonoursCancelledContext(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, githubQueryNamesResponse{})
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestGitHubHost(t, srv.URL).ListOwnRepos(ctx)
	require.Error(t, err)
}

// --- GitLab ---

// gitLabListerServer serves /user plus whatever handlers the test adds.
func gitLabListerServer(t *testing.T, mux *http.ServeMux) *GitLabHost {
	t.Helper()

	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"id": 42, "username": "testuser"}`)
	})

	srv := mockServer(t, mux)

	host, err := NewGitLabHost(NewGitLabHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL + "/api/v4",
		Token:      "test-gl-token",
	})
	require.NoError(t, err)

	return host
}

func TestGitLabListOwnRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-gl-token", r.Header.Get("Private-Token"))

		writeRaw(w, `[
			{"path":"plain","path_with_namespace":"testuser/plain","http_url_to_repo":"https://gitlab.com/testuser/plain.git",
			 "ssh_url_to_repo":"git@gitlab.com:testuser/plain.git","owner":{"name":"testuser"},"visibility":"public"},
			{"path":"flagged","path_with_namespace":"grp/sub/flagged","http_url_to_repo":"https://gitlab.com/grp/sub/flagged.git",
			 "namespace":{"full_path":"grp/sub"},"archived":true,"empty_repo":true,"forked_from_project":{"id":7},
			 "visibility":"private"}
		]`)
	})

	repos, err := gitLabListerServer(t, mux).ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 2)

	auth := BasicAuth{User: "testuser", Password: "test-gl-token"}

	require.Equal(t, Repository{
		Owner:             "testuser",
		Name:              "plain",
		PathWithNamespace: "testuser/plain",
		Domain:            gitLabDomain,
		CloneURL:          "https://gitlab.com/testuser/plain.git",
		SSHURL:            "git@gitlab.com:testuser/plain.git",
		Auth:              auth,
	}, repos[0])

	require.Equal(t, Repository{
		Owner:             "grp/sub",
		Name:              "flagged",
		PathWithNamespace: "grp/sub/flagged",
		Domain:            gitLabDomain,
		CloneURL:          "https://gitlab.com/grp/sub/flagged.git",
		Auth:              auth,
		IsFork:            true,
		IsArchived:        true,
		IsEmpty:           true,
		IsPrivate:         true,
	}, repos[1])
}

func TestGitLabListOrgReposEscapesGroupPath(t *testing.T) {
	var gotPath, gotSubgroups string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotSubgroups = r.URL.Query().Get("include_subgroups")

		writeRaw(w, `[{"path":"p","path_with_namespace":"grp/sub/p","namespace":{"full_path":"grp/sub"}}]`)
	})

	repos, err := gitLabListerServer(t, mux).ListOrgRepos(context.Background(), "grp/sub")
	require.NoError(t, err)
	require.Equal(t, "/api/v4/groups/grp%2Fsub/projects", gotPath)
	require.Equal(t, "true", gotSubgroups)
	require.Len(t, repos, 1)
	require.Equal(t, "grp/sub", repos[0].Owner)
}

func TestGitLabListUserReposNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/nobody/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := gitLabListerServer(t, mux).ListUserRepos(context.Background(), "nobody")
	require.ErrorContains(t, err, "HTTP 404")
}

func TestGitLabListOrgMembersFollowsLinks(t *testing.T) {
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups/grp/members/all", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeRaw(w, `[{"username":"bob"}]`)

			return
		}

		w.Header().Set("Link", `<`+srvURL+`/api/v4/groups/grp/members/all?page=2>; rel="next"`)
		writeRaw(w, `[{"username":"alice"}]`)
	})

	host := gitLabListerServer(t, mux)
	srvURL = strings.TrimSuffix(host.APIURL, "/api/v4")

	members, err := host.ListOrgMembers(context.Background(), "grp")
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, members)
}

func TestGitLabListerFailsWhenTokenRejected(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeRaw(w, `{"message": "401 Unauthorized"}`)
	}))

	host, err := NewGitLabHost(NewGitLabHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL + "/api/v4",
		Token:      "bad-token",
	})
	require.NoError(t, err)

	_, err = host.ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "authentication failed")
}

func TestErrNotSupportedIsMatchable(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), ErrNotSupported)
	require.ErrorIs(t, wrapped, ErrNotSupported)
}

func TestGitHubListerReportsPrivacyAndSize(t *testing.T) {
	var queries []string

	srv := mockServer(t, graphQLQueries(t, &queries, func(string) any {
		private := ghFlaggedEdge("user", "private", false, false, false)
		private.Node.IsPrivate = true
		private.Node.DiskUsage = 2048

		resp := githubQueryNamesResponse{}
		resp.Data.Viewer.Repositories.Edges = []edge{private, ghFlaggedEdge("user", "public", false, false, false)}

		return resp
	}))

	repos, err := newTestGitHubHost(t, srv.URL).ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Contains(t, queries[0], "isPrivate diskUsage")

	require.Len(t, repos, 2)
	require.True(t, repos[0].IsPrivate)
	require.Equal(t, int64(2048), repos[0].SizeKB)
	require.False(t, repos[1].IsPrivate)
	require.Zero(t, repos[1].SizeKB, "an unknown size is reported as 0")
}

func TestGitLabListerReportsVisibility(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/users/someone/projects", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[{"path":"pub","path_with_namespace":"someone/pub","visibility":"public"},
			{"path":"int","path_with_namespace":"someone/int","visibility":"internal"},
			{"path":"priv","path_with_namespace":"someone/priv","visibility":"private"}]`)
	})

	repos, err := gitLabListerServer(t, mux).ListUserRepos(context.Background(), "someone")
	require.NoError(t, err)
	require.Len(t, repos, 3)

	require.False(t, repos[0].IsPrivate)
	require.True(t, repos[1].IsPrivate, "internal projects are not public")
	require.True(t, repos[2].IsPrivate)
}

// newAnonymousGitLabHost points a host with no token at mux, failing the test
// if anything is requested with a token or the user is looked up.
func newAnonymousGitLabHost(t *testing.T, mux *http.ServeMux) *GitLabHost {
	t.Helper()

	mux.HandleFunc("/api/v4/user", func(http.ResponseWriter, *http.Request) {
		t.Error("the user must not be looked up without a token")
	})

	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Private-Token"]; ok {
			t.Errorf("%s was requested with a token header", r.URL.Path)
		}

		mux.ServeHTTP(w, r)
	}))

	host, err := NewGitLabHost(NewGitLabHostInput{HTTPClient: testHTTPClient(), APIURL: srv.URL + "/api/v4"})
	require.NoError(t, err)

	return host
}

func TestGitLabListerWithoutToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups/grp/projects", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[{"path":"p","path_with_namespace":"grp/p","http_url_to_repo":"https://gitlab.com/grp/p.git","visibility":"public"}]`)
	})
	mux.HandleFunc("/api/v4/groups/grp/members/all", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `[{"username":"alice"}]`)
	})

	host := newAnonymousGitLabHost(t, mux)
	ctx := context.Background()

	repos, err := host.ListOrgRepos(ctx, "grp")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, BasicAuth{}, repos[0].Auth, "public projects are cloned without credentials")

	members, err := host.ListOrgMembers(ctx, "grp")
	require.NoError(t, err)
	require.Equal(t, []string{"alice"}, members)

	_, err = host.ListOwnRepos(ctx)
	require.ErrorContains(t, err, "token not provided")
}
