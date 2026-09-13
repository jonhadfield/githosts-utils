package githosts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testCodebergToken    = "test-codeberg-token" //nolint:gosec // test credential
	testCodebergUser     = "soba-user"
	testCodebergOrgOne   = "soba-org-one"
	testCodebergOrgTwo   = "soba-org-two"
	testCodebergOrgThree = "soba-org-three"
	testCodebergOrgRepo  = "soba-org-one/org-one-repo"
	testDedupePath       = "a/one"
	testCodebergDomain   = "codeberg.org"
)

// cbRepo builds a Forgejo repository response for owner/name on the Codeberg
// domain, matching the subset of fields describeRepos reads.
func cbRepo(owner, name string) mockGiteaRepo {
	return mockGiteaRepo{
		Name:     name,
		FullName: owner + "/" + name,
		CloneUrl: "https://codeberg.org/" + owner + "/" + name + ".git",
		SshUrl:   "git@codeberg.org:" + owner + "/" + name + ".git",
		Owner:    mockGiteaRepoOwner{Login: owner},
	}
}

// newTestCodebergHost builds a Codeberg host pointing at apiBaseURL (the test
// server root; "/api/v1" is appended).
func newTestCodebergHost(t *testing.T, apiBaseURL string, orgs ...string) *CodebergHost {
	t.Helper()

	host, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     apiBaseURL + "/api/v1",
		BackupDir:  t.TempDir(),
		Token:      testCodebergToken,
		Orgs:       orgs,
	})
	require.NoError(t, err)

	return host
}

// repoPaths returns the PathWithNameSpace of each repo, for order-independent
// assertions.
func repoPaths(repos []repository) []string {
	paths := make([]string, 0, len(repos))
	for _, r := range repos {
		paths = append(paths, r.PathWithNameSpace)
	}

	return paths
}

func TestNewCodebergHostDefaults(t *testing.T) {
	host, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient: testHTTPClient(),
		Token:      testCodebergToken,
	})
	require.NoError(t, err)

	require.Equal(t, codebergAPIURL, host.getAPIURL())
	require.Equal(t, codebergProviderName, host.Provider)
	require.Equal(t, cloneMethod, host.diffRemoteMethod())
}

func TestNewCodebergHostInvalidDiffRemoteMethod(t *testing.T) {
	_, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient:       testHTTPClient(),
		Token:            testCodebergToken,
		DiffRemoteMethod: "invalid",
	})
	require.Error(t, err)
}

// The token-scoped /user/repos endpoint replaces Gitea's admin-only user
// enumeration, which Codeberg rejects.
func TestCodebergDescribeRepos_UserRepos(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "token "+testCodebergToken, r.Header.Get(HeaderAuthorization))

		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergUser, "repo-one")})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Len(t, result.Repos, 1)

	got := result.Repos[0]
	require.Equal(t, "repo-one", got.Name)
	require.Equal(t, testCodebergUser, got.Owner)
	require.Equal(t, "soba-user/repo-one", got.PathWithNameSpace)
	require.Equal(t, testCodebergDomain, got.Domain)
	require.Equal(t, "https://codeberg.org/soba-user/repo-one.git", got.HTTPSUrl)
}

func TestCodebergDescribeRepos_Pagination(t *testing.T) {
	mux := http.NewServeMux()

	var srvURL string

	var calls int

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, r *http.Request) {
		calls++

		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergUser, "repo-two")})

			return
		}

		w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/user/repos?page=2>; rel="next"`, srvURL))
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergUser, "repo-one")})
	})

	srv := mockServer(t, mux)
	srvURL = srv.URL

	host := newTestCodebergHost(t, srv.URL)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.ElementsMatch(t, []string{"soba-user/repo-one", "soba-user/repo-two"}, repoPaths(result.Repos))
}

// LimitUserOwned drops repositories the token can reach but does not own, which
// on a public host would otherwise pull in every repo the user collaborates on.
func TestCodebergDescribeRepos_LimitUserOwned(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, giteaUser{ID: 1, Login: testCodebergUser, Username: testCodebergUser})
	})

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{
			cbRepo(testCodebergUser, "mine"),
			cbRepo("someone-else", "theirs"),
		})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL)
	host.LimitUserOwned = true

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, []string{"soba-user/mine"}, repoPaths(result.Repos))
}

// A wildcard org expands via /user/orgs rather than Gitea's /orgs, which on
// Codeberg would list every organisation on the instance.
func TestCodebergDescribeRepos_OrgWildcard(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/orgs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []giteaOrganization{
			{ID: 1, Name: testCodebergOrgOne, Username: testCodebergOrgOne},
			{ID: 2, Name: testCodebergOrgTwo, Username: testCodebergOrgTwo},
		})
	})

	mux.HandleFunc("/api/v1/orgs/soba-org-one/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergOrgOne, "org-one-repo")})
	})

	mux.HandleFunc("/api/v1/orgs/soba-org-two/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergOrgTwo, "org-two-repo")})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL, "*")
	host.SkipUserRepos = true

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{testCodebergOrgRepo, "soba-org-two/org-two-repo"},
		repoPaths(result.Repos))
}

// The wildcard tops up the named organisations rather than replacing them, so
// an organisation the token does not belong to can still be named alongside it.
func TestCodebergDescribeRepos_OrgWildcardKeepsNamedOrgs(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/orgs", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []giteaOrganization{
			{ID: 1, Name: testCodebergOrgOne, Username: testCodebergOrgOne},
			{ID: 2, Name: testCodebergOrgTwo, Username: testCodebergOrgTwo},
		})
	})

	orgCalls := make(map[string]int)

	for _, org := range []string{testCodebergOrgOne, testCodebergOrgTwo, testCodebergOrgThree} {
		mux.HandleFunc("/api/v1/orgs/"+org+"/repos", func(w http.ResponseWriter, _ *http.Request) {
			orgCalls[org]++

			writeJSON(w, []mockGiteaRepo{cbRepo(org, org+"-repo")})
		})
	}

	// soba-org-three is not one of the token's orgs, and soba-org-two is both
	// named and returned by the expansion.
	host := newTestCodebergHost(t, mockServer(t, mux).URL, "*", testCodebergOrgTwo, testCodebergOrgThree)
	host.SkipUserRepos = true

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"soba-org-one/soba-org-one-repo", "soba-org-two/soba-org-two-repo", "soba-org-three/soba-org-three-repo"},
		repoPaths(result.Repos))
	require.Equal(t, map[string]int{testCodebergOrgOne: 1, testCodebergOrgTwo: 1, testCodebergOrgThree: 1}, orgCalls)
}

func TestCodebergDescribeRepos_NamedOrgSkipsUserOrgsLookup(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/orgs", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("named orgs should not trigger a /user/orgs lookup")
		w.WriteHeader(http.StatusInternalServerError)
	})

	mux.HandleFunc("/api/v1/orgs/soba-org-one/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergOrgOne, "org-one-repo")})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL, testCodebergOrgOne)
	host.SkipUserRepos = true

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, []string{testCodebergOrgRepo}, repoPaths(result.Repos))
}

// /user/repos already returns org repos, so naming an org must not queue the
// same repository twice: two workers would clone into one working directory.
func TestCodebergDescribeRepos_DedupesOrgAndUserRepos(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{
			cbRepo(testCodebergUser, "personal"),
			cbRepo(testCodebergOrgOne, "shared"),
		})
	})

	mux.HandleFunc("/api/v1/orgs/soba-org-one/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergOrgOne, "shared")})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL, testCodebergOrgOne)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"soba-user/personal", "soba-org-one/shared"}, repoPaths(result.Repos))
}

func TestCodebergDescribeRepos_SkipUserRepos(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("user repos should not be requested when SkipUserRepos is set")
		w.WriteHeader(http.StatusInternalServerError)
	})

	mux.HandleFunc("/api/v1/orgs/soba-org-one/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{cbRepo(testCodebergOrgOne, "org-one-repo")})
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL, testCodebergOrgOne)
	host.SkipUserRepos = true

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.Equal(t, []string{testCodebergOrgRepo}, repoPaths(result.Repos))
}

func TestCodebergDescribeRepos_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	host := newTestCodebergHost(t, mockServer(t, mux).URL)

	_, err := host.describeRepos()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestCodebergBackupNoBackupDir(t *testing.T) {
	host, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient: testHTTPClient(),
		Token:      testCodebergToken,
	})
	require.NoError(t, err)

	result := host.Backup()
	require.Error(t, result.Error)
	require.Empty(t, result.BackupResults)
}

func TestDedupeRepos(t *testing.T) {
	in := []repository{
		{Domain: testCodebergDomain, PathWithNameSpace: testDedupePath},
		{Domain: testCodebergDomain, PathWithNameSpace: testDedupePath},
		{Domain: testCodebergDomain, PathWithNameSpace: "b/one"},
		// same path on a different host must be kept
		{Domain: "example.org", PathWithNameSpace: testDedupePath},
	}

	out := dedupeRepos(in)
	require.Len(t, out, 3)
	require.ElementsMatch(t, []string{testDedupePath, "b/one", testDedupePath}, repoPaths(out))
}

// serveGitRepo publishes a bare repository containing a single commit over
// plain HTTP (git's "dumb" protocol), returning the clone URL. It lets the
// backup run end to end without reaching Codeberg.
func serveGitRepo(t *testing.T, name string) string {
	t.Helper()

	root := t.TempDir()
	repoDir := filepath.Join(root, name+".git")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))

	setupTestRepo(t, repoDir)

	// dumb HTTP clients read info/refs and objects/info/packs rather than
	// asking git for them, so they have to be written out first.
	cmd := exec.CommandContext(context.Background(), "git", "update-server-info")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	return mockServer(t, http.FileServer(http.Dir(root))).URL + "/" + name + ".git"
}

// The Backup path beyond describeRepos - worker wiring, token injection, and
// bundle creation - is otherwise only covered by the no-backup-dir case.
func TestCodebergBackupCreatesBundle(t *testing.T) {
	t.Setenv(codebergEnvVarWorkerDelay, "0")

	cloneURL := serveGitRepo(t, "repo-one")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/user/repos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []mockGiteaRepo{{
			Name:     "repo-one",
			FullName: "soba-user/repo-one",
			CloneUrl: cloneURL,
			Owner:    mockGiteaRepoOwner{Login: testCodebergUser},
		}})
	})

	backupDir := t.TempDir()

	host, err := NewCodebergHost(NewCodebergHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     mockServer(t, mux).URL + "/api/v1",
		BackupDir:  backupDir,
		Token:      testCodebergToken,
	})
	require.NoError(t, err)

	result := host.Backup()
	require.NoError(t, result.Error)
	require.Len(t, result.BackupResults, 1)
	require.Equal(t, "soba-user/repo-one", result.BackupResults[0].Repo)
	require.NoError(t, result.BackupResults[0].Error)
	require.Equal(t, statusOk, result.BackupResults[0].Status)

	// bundles are written beneath the domain the clone URL resolved to
	cloneHost, err := url.Parse(cloneURL)
	require.NoError(t, err)

	bundles, err := filepath.Glob(filepath.Join(backupDir, cloneHost.Host, testCodebergUser, "repo-one", "*.bundle"))
	require.NoError(t, err)
	require.Len(t, bundles, 1)
	require.Contains(t, filepath.Base(bundles[0]), "repo-one.")
}
