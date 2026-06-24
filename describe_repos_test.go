package githosts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
)

func testHTTPClient() *retryablehttp.Client {
	rc := retryablehttp.NewClient()
	rc.Logger = nil
	rc.RetryMax = 0

	return rc
}

// writeJSON writes v as a JSON response. Shared by the mock handlers below to
// avoid repeating the header/encode boilerplate.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// mockServer starts an httptest server that is closed when the test ends.
func mockServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return srv
}

// mockGiteaRepoOwner / mockGiteaRepo model the subset of Gitea's repo response
// the describeRepos tests assert on; shared across the Gitea test cases.
type mockGiteaRepoOwner struct {
	Login string `json:"login"`
}

type mockGiteaRepo struct {
	Name     string             `json:"name"`
	FullName string             `json:"full_name"`
	CloneUrl string             `json:"clone_url"`
	SshUrl   string             `json:"ssh_url"`
	Owner    mockGiteaRepoOwner `json:"owner"`
}

// giteaReposHandler answers a Gitea repos endpoint with the given repos.
func giteaReposHandler(repos ...mockGiteaRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, repos)
	}
}

// newTestGiteaHost builds a Gitea host pointing at apiBaseURL (the test
// server root; "/api/v1" is appended) with the shared test credentials.
func newTestGiteaHost(t *testing.T, apiBaseURL string, orgs ...string) *GiteaHost {
	t.Helper()

	host, err := NewGiteaHost(NewGiteaHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     apiBaseURL + "/api/v1",
		BackupDir:  t.TempDir(),
		Token:      "test-gitea-token",
		Orgs:       orgs,
	})
	require.NoError(t, err)

	return host
}

// ghEdge builds a GitHub GraphQL repository edge for owner/name.
func ghEdge(owner, name string) edge {
	var e edge

	e.Node.Name = name
	e.Node.NameWithOwner = owner + "/" + name
	e.Node.URL = "https://github.com/" + owner + "/" + name
	e.Node.SSHURL = "git@github.com:" + owner + "/" + name + ".git"

	return e
}

// --- Bitbucket ---

func TestBitbucketDescribeRepos_APIToken(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/user/workspaces", func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := r.BasicAuth()
		require.True(t, ok, "expected basic auth")
		require.Equal(t, "user@example.com", user)

		resp := bitbucketGetWorkspacesResponse{
			Pagelen: 10,
			Values: []bitbucketWorkspaceMembership{
				{Workspace: bitbucketWorkspace{Slug: "my-workspace", Name: "My Workspace", UUID: "{uuid-1}"}},
				{Workspace: bitbucketWorkspace{Slug: "other-ws", Name: "Other", UUID: "{uuid-2}"}},
			},
		}

		writeJSON(w, resp)
	})

	mux.HandleFunc("/repositories/my-workspace", func(w http.ResponseWriter, r *http.Request) {
		resp := bitbucketGetProjectsResponse{
			Pagelen: 10,
			Values: []bitbucketProject{
				{Scm: "git", Name: "repo-one", FullName: "my-workspace/repo-one"},
			},
		}

		writeJSON(w, resp)
	})

	mux.HandleFunc("/repositories/other-ws", func(w http.ResponseWriter, r *http.Request) {
		resp := bitbucketGetProjectsResponse{
			Pagelen: 10,
			Values: []bitbucketProject{
				{Scm: "git", Name: "repo-two", FullName: "other-ws/repo-two"},
				{Scm: "hg", Name: "hg-repo", FullName: "other-ws/hg-repo"},
			},
		}

		writeJSON(w, resp)
	})

	srv := mockServer(t, mux)

	host, err := NewBitBucketHost(NewBitBucketHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		AuthType:   AuthTypeBitbucketAPIToken,
		Email:      "user@example.com",
		APIToken:   "test-token",
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 2, "should have 2 git repos, hg repo filtered out")
	require.Equal(t, "repo-one", result.Repos[0].Name)
	require.Equal(t, "my-workspace/repo-one", result.Repos[0].PathWithNameSpace)
	require.Equal(t, "repo-two", result.Repos[1].Name)
}

func TestBitbucketDescribeRepos_WorkspacePagination(t *testing.T) {
	mux := http.NewServeMux()

	callCount := 0
	mux.HandleFunc("/user/workspaces", func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var resp bitbucketGetWorkspacesResponse
		if callCount == 1 {
			resp = bitbucketGetWorkspacesResponse{
				Pagelen: 1,
				Values:  []bitbucketWorkspaceMembership{{Workspace: bitbucketWorkspace{Slug: "ws-1"}}},
				Next:    fmt.Sprintf("http://%s/user/workspaces?page=2", r.Host),
			}
		} else {
			resp = bitbucketGetWorkspacesResponse{
				Pagelen: 1,
				Values:  []bitbucketWorkspaceMembership{{Workspace: bitbucketWorkspace{Slug: "ws-2"}}},
			}
		}

		writeJSON(w, resp)
	})

	mux.HandleFunc("/repositories/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, bitbucketGetProjectsResponse{Pagelen: 10})
	})

	srv := mockServer(t, mux)

	host, err := NewBitBucketHost(NewBitBucketHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		AuthType:   AuthTypeBitbucketAPIToken,
		Email:      "user@example.com",
		APIToken:   "test-token",
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, 2, callCount, "should paginate workspaces")
	require.Empty(t, result.Repos)
}

func TestBitbucketDescribeRepos_ConfiguredWorkspaces(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/repositories/explicit-ws", func(w http.ResponseWriter, r *http.Request) {
		resp := bitbucketGetProjectsResponse{
			Pagelen: 10,
			Values: []bitbucketProject{
				{Scm: "git", Name: "my-repo", FullName: "explicit-ws/my-repo"},
			},
		}

		writeJSON(w, resp)
	})

	srv := mockServer(t, mux)

	host, err := NewBitBucketHost(NewBitBucketHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		AuthType:   AuthTypeBitbucketAPIToken,
		Email:      "user@example.com",
		APIToken:   "test-token",
		Workspaces: []string{"explicit-ws"},
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "my-repo", result.Repos[0].Name)
}

func TestBitbucketDescribeRepos_APIError(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))

	host, err := NewBitBucketHost(NewBitBucketHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		AuthType:   AuthTypeBitbucketAPIToken,
		Email:      "user@example.com",
		APIToken:   "test-token",
	})
	require.NoError(t, err)

	_, err = host.describeRepos()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get BitBucket workspaces")
}

func TestBitbucketDescribeRepos_BearerToken(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/user/workspaces", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		require.Equal(t, "Bearer test-oauth-token", auth)

		resp := bitbucketGetWorkspacesResponse{
			Pagelen: 10,
			Values:  []bitbucketWorkspaceMembership{{Workspace: bitbucketWorkspace{Slug: "oauth-ws"}}},
		}

		writeJSON(w, resp)
	})

	mux.HandleFunc("/repositories/oauth-ws", func(w http.ResponseWriter, r *http.Request) {
		resp := bitbucketGetProjectsResponse{
			Pagelen: 10,
			Values: []bitbucketProject{
				{Scm: "git", Name: "oauth-repo", FullName: "oauth-ws/oauth-repo"},
			},
		}

		writeJSON(w, resp)
	})

	srv := mockServer(t, mux)

	host := &BitbucketHost{
		HttpClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		AuthType:   AuthTypeBearerToken,
		OAuthToken: "test-oauth-token",
	}

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "oauth-repo", result.Repos[0].Name)
}

// --- GitHub ---

func TestGitHubDescribeRepos_UserRepos(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer test-gh-token", r.Header.Get("Authorization"))

		resp := githubQueryNamesResponse{}
		resp.Data.Viewer.Repositories.Edges = []edge{ghEdge("user", "my-repo")}
		resp.Data.Viewer.Repositories.PageInfo.HasNextPage = false

		writeJSON(w, resp)
	}))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		Token:      "test-gh-token",
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "my-repo", result.Repos[0].Name)
	require.Equal(t, "user/my-repo", result.Repos[0].PathWithNameSpace)
	require.Equal(t, gitHubDomain, result.Repos[0].Domain)
}

func TestGitHubDescribeRepos_OrgRepos(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := githubQueryOrgResponse{}
		resp.Data.Organization.Repositories.Edges = []edge{ghEdge("my-org", "org-repo")}
		resp.Data.Organization.Repositories.PageInfo.HasNextPage = false

		writeJSON(w, resp)
	}))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient:    testHTTPClient(),
		APIURL:        srv.URL,
		BackupDir:     t.TempDir(),
		Token:         "test-gh-token",
		SkipUserRepos: true,
		Orgs:          []string{"my-org"},
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "org-repo", result.Repos[0].Name)
	require.Equal(t, "my-org/org-repo", result.Repos[0].PathWithNameSpace)
}

func TestGitHubDescribeRepos_Unauthorized(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "Bad credentials"}`)
	}))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		Token:      "bad-token",
	})
	require.NoError(t, err)

	_, err = host.describeRepos()
	require.Error(t, err)
}

// --- GitLab ---

func TestGitLabDescribeRepos(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-gl-token", r.Header.Get("Private-Token"))

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": 42, "username": "testuser"}`)
	})

	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-gl-token", r.Header.Get("Private-Token"))

		projects := []gitLabProject{
			{
				Path:              "my-project",
				PathWithNameSpace: "testuser/my-project",
				HTTPSURL:          "https://gitlab.com/testuser/my-project.git",
				SSHURL:            "git@gitlab.com:testuser/my-project.git",
				Owner:             gitLabOwner{ID: 42, Name: "testuser"},
			},
		}

		writeJSON(w, projects)
	})

	srv := mockServer(t, mux)

	host, err := NewGitLabHost(NewGitLabHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL + "/api/v4",
		BackupDir:  t.TempDir(),
		Token:      "test-gl-token",
	})
	require.NoError(t, err)

	// Authenticate first (Backup() does this before describeRepos)
	user, err := host.getAuthenticatedGitLabUser()
	require.NoError(t, err)
	require.Equal(t, 42, user.ID)
	require.Equal(t, "testuser", user.UserName)

	host.User = user

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "my-project", result.Repos[0].Name)
	require.Equal(t, "testuser/my-project", result.Repos[0].PathWithNameSpace)
}

func TestGitLabDescribeRepos_Unauthorized(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message": "401 Unauthorized"}`)
	}))

	host, err := NewGitLabHost(NewGitLabHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL + "/api/v4",
		BackupDir:  t.TempDir(),
		Token:      "bad-token",
	})
	require.NoError(t, err)

	user, err := host.getAuthenticatedGitLabUser()
	require.NoError(t, err)
	require.Equal(t, 0, user.ID, "should return empty user on 401")
}

// --- Gitea ---

func TestGiteaDescribeRepos_UserRepos(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "token test-gitea-token", r.Header.Get("Authorization"))

		users := []giteaUser{
			{ID: 1, Login: "admin", Username: "admin"},
		}

		writeJSON(w, users)
	})

	mux.HandleFunc("/api/v1/users/admin/repos", giteaReposHandler(mockGiteaRepo{
		Name:     "test-repo",
		FullName: "admin/test-repo",
		CloneUrl: "https://gitea.example.com/admin/test-repo.git",
		SshUrl:   "git@gitea.example.com:admin/test-repo.git",
		Owner:    mockGiteaRepoOwner{Login: "admin"},
	}))

	host := newTestGiteaHost(t, mockServer(t, mux).URL)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "test-repo", result.Repos[0].Name)
	require.Equal(t, "admin/test-repo", result.Repos[0].PathWithNameSpace)
}

func TestGiteaDescribeRepos_WithOrgs(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	mux.HandleFunc("/api/v1/orgs/my-org", func(w http.ResponseWriter, r *http.Request) {
		org := giteaOrganization{ID: 1, Name: "my-org", Username: "my-org"}

		writeJSON(w, org)
	})

	mux.HandleFunc("/api/v1/orgs/my-org/repos", giteaReposHandler(mockGiteaRepo{
		Name:     "org-repo",
		FullName: "my-org/org-repo",
		CloneUrl: "https://gitea.example.com/my-org/org-repo.git",
		SshUrl:   "git@gitea.example.com:my-org/org-repo.git",
		Owner:    mockGiteaRepoOwner{Login: "my-org"},
	}))

	host := newTestGiteaHost(t, mockServer(t, mux).URL, "my-org")

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)
	require.Equal(t, "org-repo", result.Repos[0].Name)
}

// --- Sourcehut ---

func TestSourcehutDescribeRepos(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer test-srht-token", r.Header.Get("Authorization"))

		resp := sourcehutRepositoriesResponse{}
		resp.Data.Repositories.Results = []sourcehutRepository{
			{
				ID:         1,
				Name:       "my-repo",
				Visibility: "public",
				Owner: struct {
					Username string `json:"username"`
				}{Username: "~testuser"},
			},
			{
				ID:         2,
				Name:       "private-repo",
				Visibility: "private",
				Owner: struct {
					Username string `json:"username"`
				}{Username: "~testuser"},
			},
		}
		resp.Data.Repositories.Cursor = nil

		writeJSON(w, resp)
	}))

	host, err := NewSourcehutHost(NewSourcehutHostInput{
		HTTPClient:          testHTTPClient(),
		APIURL:              srv.URL,
		BackupDir:           t.TempDir(),
		PersonalAccessToken: "test-srht-token",
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1, "should only include public repos")
	require.Equal(t, "my-repo", result.Repos[0].Name)
	require.Equal(t, "testuser/my-repo", result.Repos[0].PathWithNameSpace)
	require.Equal(t, "https://git.sr.ht/~testuser/my-repo", result.Repos[0].HTTPSUrl)
}

func TestSourcehutDescribeRepos_Pagination(t *testing.T) {
	callCount := 0
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		resp := sourcehutRepositoriesResponse{}
		if callCount == 1 {
			cursor := "cursor-page-2"
			resp.Data.Repositories.Results = []sourcehutRepository{
				{ID: 1, Name: "repo-1", Visibility: "public",
					Owner: struct {
						Username string `json:"username"`
					}{Username: "~user"}},
			}
			resp.Data.Repositories.Cursor = &cursor
		} else {
			resp.Data.Repositories.Results = []sourcehutRepository{
				{ID: 2, Name: "repo-2", Visibility: "public",
					Owner: struct {
						Username string `json:"username"`
					}{Username: "~user"}},
			}
			resp.Data.Repositories.Cursor = nil
		}

		writeJSON(w, resp)
	}))

	host, err := NewSourcehutHost(NewSourcehutHostInput{
		HTTPClient:          testHTTPClient(),
		APIURL:              srv.URL,
		BackupDir:           t.TempDir(),
		PersonalAccessToken: "test-token",
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, 2, callCount)
	require.Len(t, result.Repos, 2)
}

func TestSourcehutDescribeRepos_Unauthorized(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errors": [{"message": "unauthorized"}]}`)
	}))

	host, err := NewSourcehutHost(NewSourcehutHostInput{
		HTTPClient:          testHTTPClient(),
		APIURL:              srv.URL,
		BackupDir:           t.TempDir(),
		PersonalAccessToken: "bad-token",
	})
	require.NoError(t, err)

	_, err = host.describeRepos()
	require.Error(t, err)
}

// --- Azure DevOps ---

func TestAzureDevOpsListAllRepositories(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/my-org/my-project/_apis/git/repositories", func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.Header.Get("Authorization"), "Basic ")

		repos := repoListBody{
			Value: []AzureDevOpsRepo{
				{
					Id:        "repo-id-1",
					Name:      "test-repo",
					RemoteUrl: "https://dev.azure.com/my-org/my-project/_git/test-repo",
					WebUrl:    "https://dev.azure.com/my-org/my-project/_git/test-repo",
					SshUrl:    "git@ssh.dev.azure.com:v3/my-org/my-project/test-repo",
					Project:   Project{Name: "my-project"},
				},
			},
		}

		writeJSON(w, repos)
	})

	srv := mockServer(t, mux)

	client := testHTTPClient()
	basicAuth := generateBasicAuth("testuser", "test-pat")

	// Override the URL to point at our test server
	repos, err := listAllRepositoriesWithURL(client, basicAuth, "my-project", "my-org", srv.URL)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "test-repo", repos[0].Name)
}
