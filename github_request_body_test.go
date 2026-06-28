package githosts

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGitHubRequestBodiesAreValidJSON is a regression test for a long-standing
// bug where the GitHub GraphQL request bodies were hand-built by string
// concatenation and omitted the closing brace, e.g. `{"query": "..."`. GitHub
// accepted the malformed body most of the time but intermittently returned
// HTTP 502, which retryablehttp treated as retryable and backed off on for
// RetryWaitMin (60s) before retrying — so listing repositories would stall for
// 60-240s instead of completing in seconds.
//
// This drives a describeRepos run that exercises every previously hand-built
// query (user repos first page + pagination, the `*` organizations lookup, and
// org repos) and asserts that every body actually sent to GitHub is valid JSON
// carrying a non-empty "query".
func TestGitHubRequestBodiesAreValidJSON(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies [][]byte
	)

	viewerRepoRequests := func() int {
		n := 0
		for _, b := range bodies {
			if strings.Contains(string(b), "viewer { repositories") {
				n++
			}
		}

		return n
	}

	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		mu.Lock()
		bodies = append(bodies, body)
		seen := viewerRepoRequests()
		mu.Unlock()

		switch q := string(body); {
		case strings.Contains(q, "viewer { organizations"):
			resp := githubQueryOrgsResponse{}
			resp.Data.Viewer.Organizations.Edges = []orgsEdge{{Node: struct{ Name string }{Name: "my-org"}}}
			writeJSON(w, resp)
		case strings.Contains(q, "organization(login:"):
			resp := githubQueryOrgResponse{}
			resp.Data.Organization.Repositories.Edges = []edge{ghEdge("my-org", "org-repo")}
			resp.Data.Organization.Repositories.PageInfo.HasNextPage = false
			writeJSON(w, resp)
		case strings.Contains(q, "viewer { repositories"):
			resp := githubQueryNamesResponse{}
			if seen == 1 {
				// first page: signal another page to exercise the cursor query
				resp.Data.Viewer.Repositories.Edges = []edge{ghEdge("user", "repo-1")}
				resp.Data.Viewer.Repositories.PageInfo.HasNextPage = true
				resp.Data.Viewer.Repositories.PageInfo.EndCursor = "cursor-1"
			} else {
				resp.Data.Viewer.Repositories.Edges = []edge{ghEdge("user", "repo-2")}
				resp.Data.Viewer.Repositories.PageInfo.HasNextPage = false
			}
			writeJSON(w, resp)
		default:
			t.Errorf("unexpected GraphQL query: %s", q)
		}
	}))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		Token:      "test-gh-token",
		Orgs:       []string{"*"}, // wildcard triggers the organizations lookup
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)
	require.NotEmpty(t, result.Repos)

	mu.Lock()
	defer mu.Unlock()

	// user repos (2 pages) + organizations lookup + org repos = at least 4 bodies
	require.GreaterOrEqual(t, len(bodies), 4,
		"expected user pagination, organizations lookup, and org repos to all be requested")

	for i, b := range bodies {
		var payload map[string]json.RawMessage
		require.NoErrorf(t, json.Unmarshal(b, &payload),
			"request body %d is not valid JSON: %s", i, b)
		require.Containsf(t, payload, "query",
			"request body %d missing \"query\" field: %s", i, b)

		var query string
		require.NoErrorf(t, json.Unmarshal(payload["query"], &query),
			"request body %d has a non-string \"query\": %s", i, b)
		require.NotEmptyf(t, query, "request body %d has an empty query: %s", i, b)
	}
}
