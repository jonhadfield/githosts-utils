package githosts

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestBitbucketHost returns an API token host pointed at mux, failing the
// test if a request arrives without the token's credentials.
func newTestBitbucketHost(t *testing.T, mux *http.ServeMux, workspaces ...string) *BitbucketHost {
	t.Helper()

	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user@example.com" || pass != "test-bb-token" {
			t.Errorf("%s was requested without the API token credentials", r.URL.Path)
		}

		mux.ServeHTTP(w, r)
	}))

	host, err := NewBitBucketHost(NewBitBucketHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		AuthType:   AuthTypeBitbucketAPIToken,
		Email:      "user@example.com",
		APIToken:   "test-bb-token",
		Workspaces: workspaces,
	})
	require.NoError(t, err)

	return host
}

const bitbucketListerReposJSON = `{"pagelen":10,"values":[
	{"scm":"git","name":"Plain Repo","full_name":"ws/plain-repo","is_private":false,"size":0,
	 "mainbranch":{"name":"main","type":"branch"},
	 "links":{"clone":[{"name":"https","href":"https://someone@bitbucket.org/ws/plain-repo.git"},
	                   {"name":"ssh","href":"git@bitbucket.org:ws/plain-repo.git"}]}},
	{"scm":"git","name":"flagged","full_name":"ws/flagged","is_private":true,"size":2049,
	 "parent":{"full_name":"other/flagged"},"mainbranch":null},
	{"scm":"hg","name":"legacy","full_name":"ws/legacy"}
]}`

func TestBitbucketListOrgRepos(t *testing.T) {
	var gotQuery string

	mux := http.NewServeMux()
	mux.HandleFunc("/repositories/ws", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery

		writeRaw(w, bitbucketListerReposJSON)
	})

	repos, err := newTestBitbucketHost(t, mux).ListOrgRepos(context.Background(), "ws")
	require.NoError(t, err)
	require.Empty(t, gotQuery, "a workspace listing is not narrowed to the caller's role")

	auth := BasicAuth{User: bitbucketStaticUserName, Password: "test-bb-token"}

	require.Equal(t, []Repository{
		{
			Owner:             "ws",
			Name:              "Plain Repo",
			PathWithNamespace: "ws/plain-repo",
			Domain:            bitbucketDomain,
			CloneURL:          "https://bitbucket.org/ws/plain-repo.git",
			SSHURL:            "git@bitbucket.org:ws/plain-repo.git",
			Auth:              auth,
		},
		{
			Owner:             "ws",
			Name:              "flagged",
			PathWithNamespace: "ws/flagged",
			Domain:            bitbucketDomain,
			CloneURL:          "https://bitbucket.org/ws/flagged.git",
			Auth:              auth,
			IsFork:            true,
			IsEmpty:           true,
			IsPrivate:         true,
			SizeKB:            3,
		},
	}, repos, "the hg repository is skipped and the API's user-bearing clone link is not used")
}

func TestBitbucketListOwnReposUsesWorkspacesAndMemberRole(t *testing.T) {
	var srvURL string

	var roles []string

	mux := http.NewServeMux()
	mux.HandleFunc("/user/workspaces", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"values":[{"workspace":{"slug":"ws"}},{"workspace":{"slug":"team"}}]}`)
	})
	mux.HandleFunc("/repositories/ws", func(w http.ResponseWriter, r *http.Request) {
		roles = append(roles, r.URL.Query().Get("role"))

		if r.URL.Query().Get("page") == "2" {
			writeRaw(w, `{"values":[{"scm":"git","name":"two","full_name":"ws/two"}]}`)

			return
		}

		writeRaw(w, `{"values":[{"scm":"git","name":"one","full_name":"ws/one"}],"next":"`+srvURL+`/repositories/ws?role=member&page=2"}`)
	})
	mux.HandleFunc("/repositories/team", func(w http.ResponseWriter, r *http.Request) {
		roles = append(roles, r.URL.Query().Get("role"))

		writeRaw(w, `{"values":[{"scm":"git","name":"three","full_name":"team/three"}]}`)
	})

	host := newTestBitbucketHost(t, mux)
	srvURL = host.APIURL

	repos, err := host.ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"member", "member", "member"}, roles)

	paths := make([]string, 0, len(repos))
	for _, r := range repos {
		paths = append(paths, r.PathWithNamespace)
	}

	require.Equal(t, []string{"ws/one", "ws/two", "team/three"}, paths)
}

func TestBitbucketListOwnReposConfiguredWorkspaces(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/workspaces", func(http.ResponseWriter, *http.Request) {
		t.Error("configured workspaces must not be discovered")
	})
	mux.HandleFunc("/repositories/explicit", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, `{"values":[{"scm":"git","name":"r","full_name":"explicit/r"}]}`)
	})

	repos, err := newTestBitbucketHost(t, mux, "explicit").ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "explicit", repos[0].Owner)
}

// TestBitbucketListerMatchesBackupCredentials checks that the listed
// credentials are the ones Backup clones with for each auth mode.
func TestBitbucketListerMatchesBackupCredentials(t *testing.T) {
	require.Equal(t, BasicAuth{User: "x-token-auth", Password: "oauth"},
		BitbucketHost{OAuthToken: "oauth", APIToken: "api"}.cloneAuth(), "OAuth takes precedence")
	require.Equal(t, BasicAuth{User: bitbucketStaticUserName, Password: "api"},
		BitbucketHost{APIToken: "api"}.cloneAuth())
	require.Equal(t, BasicAuth{}, BitbucketHost{}.cloneAuth())
}

func TestBitbucketListUserReposUsesPersonalWorkspace(t *testing.T) {
	var gotPath string

	mux := http.NewServeMux()
	mux.HandleFunc("/repositories/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()

		writeRaw(w, `{"values":[{"scm":"git","name":"mine","full_name":"someone/mine"}]}`)
	})

	repos, err := newTestBitbucketHost(t, mux).ListUserRepos(context.Background(), "someone")
	require.NoError(t, err)
	require.Equal(t, "/repositories/someone", gotPath)
	require.Len(t, repos, 1)
	require.Equal(t, "someone", repos[0].Owner)
}

func TestBitbucketListOrgReposNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repositories/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := newTestBitbucketHost(t, mux).ListOrgRepos(context.Background(), "missing")
	require.ErrorContains(t, err, "workspace missing")
	require.ErrorContains(t, err, "404")
}

func TestBitbucketListOrgMembersPaginates(t *testing.T) {
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/members", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			writeRaw(w, `{"values":[{"user":{"nickname":"bob","uuid":"{2}"}}]}`)

			return
		}

		writeRaw(w, `{"values":[{"user":{"nickname":"alice","uuid":"{1}"}}],"next":"`+srvURL+`/workspaces/ws/members?page=2"}`)
	})

	host := newTestBitbucketHost(t, mux)
	srvURL = host.APIURL

	members, err := host.ListOrgMembers(context.Background(), "ws")
	require.NoError(t, err)
	require.Equal(t, []string{"{1}", "{2}"}, members, "members are identified by UUID, not the non-unique nickname")
}

func TestBitbucketListUserReposAcceptsUUID(t *testing.T) {
	var gotPath string

	mux := http.NewServeMux()
	// a catch-all, as a pattern containing braces would be read as a wildcard
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()

		writeRaw(w, `{"values":[]}`)
	})

	host := newTestBitbucketHost(t, mux)

	_, err := host.ListUserRepos(context.Background(), "{abc-123}")
	require.NoError(t, err)
	require.Equal(t, "/repositories/%7Babc-123%7D", gotPath)
}

func TestBitbucketListerRejectsEmptyWorkspace(t *testing.T) {
	host := newTestBitbucketHost(t, http.NewServeMux())
	ctx := context.Background()

	_, err := host.ListOrgRepos(ctx, "")
	require.Error(t, err)

	_, err = host.ListUserRepos(ctx, "")
	require.Error(t, err)

	_, err = host.ListOrgMembers(ctx, "")
	require.Error(t, err)
}

func TestBitbucketListerHonoursCancelledContext(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repositories/ws", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, bitbucketListerReposJSON)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestBitbucketHost(t, mux).ListOrgRepos(ctx, "ws")
	require.ErrorContains(t, err, "failed to make request")
}
