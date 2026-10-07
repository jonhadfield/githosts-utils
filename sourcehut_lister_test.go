package githosts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type sourcehutTestRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// sourcehutQueries records each GraphQL request received and answers with the
// raw JSON respond returns for it.
func sourcehutQueries(t *testing.T, reqs *[]sourcehutTestRequest, respond func(req sourcehutTestRequest) string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer "+sourcehutTestToken, r.Header.Get("Authorization"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req sourcehutTestRequest
		require.NoError(t, json.Unmarshal(body, &req))

		*reqs = append(*reqs, req)

		writeRaw(w, respond(req))
	}
}

func newTestSourcehutHost(t *testing.T, apiURL string) *SourcehutHost {
	t.Helper()

	host, err := NewSourcehutHost(NewSourcehutHostInput{
		HTTPClient:          testHTTPClient(),
		APIURL:              apiURL,
		PersonalAccessToken: sourcehutTestToken,
	})
	require.NoError(t, err)

	return host
}

const sourcehutTestToken = "test-srht-token" //nolint:gosec // not a real token

var sourcehutTestAuth = BasicAuth{User: sourcehutTestToken}

func TestSourcehutListOwnReposListsWhatBackupDoes(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(req sourcehutTestRequest) string {
		if req.Variables["cursor"] == "c1" {
			return `{"data":{"repositories":{"results":[
				{"id":3,"name":"empty","visibility":"PUBLIC","owner":{"username":"testuser"},"HEAD":null},
				{"id":4,"name":"secret","visibility":"PRIVATE","owner":{"username":"testuser"},"HEAD":{"name":"refs/heads/main"}}
			],"cursor":null}}}`
		}

		return `{"data":{"repositories":{"results":[
			{"id":1,"name":"plain","visibility":"PUBLIC","owner":{"username":"testuser"},"HEAD":{"name":"refs/heads/main"}},
			{"id":2,"name":"hidden","visibility":"UNLISTED","owner":{"username":"testuser"},"HEAD":{"name":"refs/heads/main"}}
		],"cursor":"c1"}}}`
	}))

	repos, err := newTestSourcehutHost(t, srv.URL).ListOwnRepos(context.Background())
	require.NoError(t, err)

	require.Len(t, reqs, 2)
	require.Nil(t, reqs[0].Variables["cursor"])
	require.Equal(t, "c1", reqs[1].Variables["cursor"])

	for _, req := range reqs {
		require.Contains(t, req.Query, "repositories(cursor: $cursor")
		require.Contains(t, req.Query, "HEAD { name }")
	}

	require.Equal(t, []Repository{
		{
			Owner:             "testuser",
			Name:              "plain",
			PathWithNamespace: "testuser/plain",
			Domain:            sourcehutDomain,
			CloneURL:          "https://git.sr.ht/~testuser/plain",
			SSHURL:            "git@git.sr.ht:~testuser/plain",
			Auth:              sourcehutTestAuth,
		},
		{
			Owner:             "testuser",
			Name:              "empty",
			PathWithNamespace: "testuser/empty",
			Domain:            sourcehutDomain,
			CloneURL:          "https://git.sr.ht/~testuser/empty",
			SSHURL:            "git@git.sr.ht:~testuser/empty",
			Auth:              sourcehutTestAuth,
			IsEmpty:           true,
		},
	}, repos, "only public repositories are listed, as Backup only processes those")
}

// TestSourcehutListOwnReposMatchesBackup guards the contract that
// ListOwnRepos returns the set describeRepos gives Backup.
func TestSourcehutListOwnReposMatchesBackup(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(sourcehutTestRequest) string {
		return `{"data":{"repositories":{"results":[
			{"id":1,"name":"a","visibility":"PUBLIC","owner":{"username":"u"}},
			{"id":2,"name":"b","visibility":"UNLISTED","owner":{"username":"u"}},
			{"id":3,"name":"c","visibility":"PRIVATE","owner":{"username":"u"}},
			{"id":4,"name":"d","visibility":"PUBLIC","owner":{"username":"u"}}
		],"cursor":null}}}`
	}))

	host := newTestSourcehutHost(t, srv.URL)

	listed, err := host.ListOwnRepos(context.Background())
	require.NoError(t, err)

	described, err := host.describeRepos()
	require.NoError(t, err)

	require.Len(t, listed, len(described.Repos))

	for i, r := range described.Repos {
		require.Equal(t, r.PathWithNameSpace, listed[i].PathWithNamespace)
		require.Equal(t, r.HTTPSUrl, listed[i].CloneURL)
		require.NotContains(t, listed[i].CloneURL, "@")
	}

	require.NotContains(t, reqs[len(reqs)-1].Query, "HEAD",
		"Backup's query must not need the OBJECTS scope")
}

func TestSourcehutListOwnReposNeedsToken(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made without a token")
	}))

	host := newTestSourcehutHost(t, srv.URL)
	host.PersonalAccessToken = ""

	_, err := host.ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "token not provided")
}

func TestSourcehutListUserRepos(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(req sourcehutTestRequest) string {
		if req.Variables["cursor"] == "c1" {
			return `{"data":{"user":{"repositories":{"results":[
				{"id":2,"name":"two","visibility":"PRIVATE","owner":{"username":"someone"},"HEAD":null}
			],"cursor":null}}}}`
		}

		return `{"data":{"user":{"repositories":{"results":[
			{"id":1,"name":"one","visibility":"PUBLIC","owner":{"username":"someone"},"HEAD":{"name":"refs/heads/master"}},
			{"id":3,"name":"three","visibility":"UNLISTED","owner":{"username":"someone"},"HEAD":{"name":"refs/heads/master"}}
		],"cursor":"c1"}}}}`
	}))

	repos, err := newTestSourcehutHost(t, srv.URL).ListUserRepos(context.Background(), "~someone")
	require.NoError(t, err)

	require.Len(t, reqs, 2)

	for _, req := range reqs {
		require.Contains(t, req.Query, "user(username: $username)")
		require.Equal(t, "someone", req.Variables["username"], "the ~ prefix is stripped")
	}

	require.Equal(t, []Repository{
		{
			Owner:             "someone",
			Name:              "one",
			PathWithNamespace: "someone/one",
			Domain:            sourcehutDomain,
			CloneURL:          "https://git.sr.ht/~someone/one",
			SSHURL:            "git@git.sr.ht:~someone/one",
			Auth:              sourcehutTestAuth,
		},
		{
			Owner:             "someone",
			Name:              "three",
			PathWithNamespace: "someone/three",
			Domain:            sourcehutDomain,
			CloneURL:          "https://git.sr.ht/~someone/three",
			SSHURL:            "git@git.sr.ht:~someone/three",
			Auth:              sourcehutTestAuth,
			IsPrivate:         true,
		},
		{
			Owner:             "someone",
			Name:              "two",
			PathWithNamespace: "someone/two",
			Domain:            sourcehutDomain,
			CloneURL:          "https://git.sr.ht/~someone/two",
			SSHURL:            "git@git.sr.ht:~someone/two",
			Auth:              sourcehutTestAuth,
			IsEmpty:           true,
			IsPrivate:         true,
		},
	}, repos)
}

func TestSourcehutListUserReposNotFound(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(sourcehutTestRequest) string {
		return `{"data":{"user":null}}`
	}))

	_, err := newTestSourcehutHost(t, srv.URL).ListUserRepos(context.Background(), "nobody")
	require.ErrorContains(t, err, "SourceHut user nobody not found")
}

func TestSourcehutListUserReposGraphQLError(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(sourcehutTestRequest) string {
		return `{"errors":[{"message":"Access denied"}],"data":null}`
	}))

	_, err := newTestSourcehutHost(t, srv.URL).ListUserRepos(context.Background(), "someone")
	require.ErrorContains(t, err, "SourceHut API returned errors")
}

// TestSourcehutListUserReposPassesUsernameAsVariable guards against query
// injection: the username is sent as a variable, never as query text.
func TestSourcehutListUserReposPassesUsernameAsVariable(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(sourcehutTestRequest) string {
		return `{"data":{"user":null}}`
	}))

	host := newTestSourcehutHost(t, srv.URL)
	evil := `x") { id } #`

	_, err := host.ListUserRepos(context.Background(), evil)
	require.Error(t, err)
	require.Len(t, reqs, 1)
	require.NotContains(t, reqs[0].Query, evil)
	require.Equal(t, evil, reqs[0].Variables["username"])
}

func TestSourcehutListUserReposRejectsEmptyUsername(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made for an empty username")
	}))

	host := newTestSourcehutHost(t, srv.URL)

	for _, user := range []string{"", " ", "~"} {
		_, err := host.ListUserRepos(context.Background(), user)
		require.Error(t, err, user)
	}
}

func TestSourcehutListerOrgsNotSupported(t *testing.T) {
	srv := mockServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be made for an unsupported operation")
	}))

	host := newTestSourcehutHost(t, srv.URL)

	_, err := host.ListOrgRepos(context.Background(), "acme")
	require.True(t, errors.Is(err, ErrNotSupported))

	_, err = host.ListOrgMembers(context.Background(), "acme")
	require.True(t, errors.Is(err, ErrNotSupported))
}

func TestSourcehutListerHonoursCancelledContext(t *testing.T) {
	var reqs []sourcehutTestRequest

	srv := mockServer(t, sourcehutQueries(t, &reqs, func(sourcehutTestRequest) string {
		return `{"data":{"repositories":{"results":[],"cursor":null}}}`
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestSourcehutHost(t, srv.URL).ListOwnRepos(ctx)
	require.Error(t, err)
	require.Empty(t, reqs)
}
