package githosts

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGitHubAPIUsageLoggingEnabled(t *testing.T) {
	tests := []struct {
		name        string
		logAPIUsage string
		gitHostsLog string
		want        bool
	}{
		{name: "unset everywhere", want: false},
		{name: "explicitly true", logAPIUsage: ghEnabledValue, want: true},
		{name: "explicitly 1", logAPIUsage: "1", want: true},
		{name: "explicitly false", logAPIUsage: "false", want: false},
		{name: "unparsable falls through to GITHOSTS_LOG", logAPIUsage: "yes-please", gitHostsLog: ghLogDebug, want: true},
		{name: "unparsable with no GITHOSTS_LOG", logAPIUsage: "yes-please", want: false},
		{name: "enabled via GITHOSTS_LOG=debug", gitHostsLog: ghLogDebug, want: true},
		{name: "GITHOSTS_LOG other value", gitHostsLog: "info", want: false},
		{name: "explicit false overrides GITHOSTS_LOG=debug", logAPIUsage: "false", gitHostsLog: ghLogDebug, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(githubEnvVarLogAPIUsage, tc.logAPIUsage)
			t.Setenv(envVarGitHostsLog, tc.gitHostsLog)

			require.Equal(t, tc.want, githubAPIUsageLoggingEnabled())
		})
	}
}

func TestFormatGitHubRateLimitReset(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, "-", formatGitHubRateLimitReset(""))
	})

	t.Run("unparsable passes through", func(t *testing.T) {
		require.Equal(t, "not-a-number", formatGitHubRateLimitReset("not-a-number"))
	})

	t.Run("renders absolute time and interval", func(t *testing.T) {
		reset := time.Now().Add(10 * time.Minute).Unix()

		got := formatGitHubRateLimitReset(strconv.FormatInt(reset, 10))

		require.Contains(t, got, time.Unix(reset, 0).UTC().Format(time.RFC3339))
		// the interval is computed at call time, so allow for sub-second drift
		require.Regexp(t, `\(in (9m5\ds|10m0s)\)$`, got)
	})
}

func TestGitHubAPIStatsNilReceiverIsSafe(t *testing.T) {
	var s *githubAPIStats

	require.NotPanics(t, func() {
		s.startPass("x", "repos")
		s.recordRequest()
		s.recordItems(3)
		s.logSummary(0, 0)
	})
	require.Equal(t, 0, s.totalRequests())
}

func TestHeaderOrDash(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(headerGitHubRateLimitLimit, "5000")

	require.Equal(t, "5000", headerOrDash(resp, headerGitHubRateLimitLimit))
	require.Equal(t, "-", headerOrDash(resp, headerGitHubRateLimitRemaining))
}

// --- discovery pass accounting across configuration combinations ---

// fixture values shared by the discovery tests below.
const (
	ghFixtureAuth    = "gh-test-fixture"
	ghTestViewer     = "viewer"
	ghTestOrg        = "my-org"
	ghTestOrgOne     = "org-one"
	ghTestOrgTwo     = "org-two"
	ghTestOrgOneRepo = "org-one/x"
	ghTestUserRepoA  = "user/a"
	ghLabelUserRepos = "user repos"
	ghLabelOrgOne    = "org org-one"
	ghLabelOrgsList  = "orgs list"
	ghLogDebug       = "debug"
	ghEnabledValue   = "true"
)

// ghMockRepos maps an "owner" to the repo names it should return. The key
// ghTestViewer holds the authenticated user's own repositories.
type ghMockRepos map[string][]string

// ghMockHandler answers the three GraphQL query shapes the GitHub discovery
// path issues, routing on the query text the way the real API would route on
// the selection set. Repos are served one per page so that pagination - and
// therefore the request counting under test - is exercised.
func ghMockHandler(t *testing.T, repos ghMockRepos, orgNames []string, seen *[]string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		*seen = append(*seen, req.Query)

		w.Header().Set(headerGitHubRateLimitLimit, "5000")
		w.Header().Set(headerGitHubRateLimitRemaining, "4999")
		w.Header().Set(headerGitHubRateLimitUsed, "1")
		w.Header().Set(headerGitHubRateLimitReset, strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

		switch {
		case strings.Contains(req.Query, "organizations(first"):
			resp := githubQueryOrgsResponse{}

			for _, name := range orgNames {
				var e orgsEdge

				e.Node.Login = name

				resp.Data.Viewer.Organizations.Edges = append(resp.Data.Viewer.Organizations.Edges, e)
			}

			writeJSON(w, resp)

		case strings.Contains(req.Query, "organization(login:"):
			org := betweenQuotes(req.Query, `organization(login: "`)
			names := repos[org]
			idx := pageIndex(req.Query)

			resp := githubQueryOrgResponse{}
			if idx < len(names) {
				resp.Data.Organization.Repositories.Edges = []edge{ghEdge(org, names[idx])}
				resp.Data.Organization.Repositories.PageInfo.EndCursor = strconv.Itoa(idx + 1)
				resp.Data.Organization.Repositories.PageInfo.HasNextPage = idx+1 < len(names)
			}

			writeJSON(w, resp)

		default:
			names := repos[ghTestViewer]
			idx := pageIndex(req.Query)

			resp := githubQueryNamesResponse{}
			if idx < len(names) {
				resp.Data.Viewer.Repositories.Edges = []edge{ghEdge("user", names[idx])}
				resp.Data.Viewer.Repositories.PageInfo.EndCursor = strconv.Itoa(idx + 1)
				resp.Data.Viewer.Repositories.PageInfo.HasNextPage = idx+1 < len(names)
			}

			writeJSON(w, resp)
		}
	}
}

// pageIndex reads the after: cursor out of a query, which the mock uses as a
// zero-based page number. Absent cursor means the first page.
func pageIndex(query string) int {
	cursor := betweenQuotes(query, `after: "`)
	if cursor == "" {
		cursor = betweenQuotes(query, `after:"`)
	}

	idx, err := strconv.Atoi(cursor)
	if err != nil {
		return 0
	}

	return idx
}

// betweenQuotes returns the text between start and the next double quote,
// which is how every value the mocks need is delimited in a GraphQL query.
func betweenQuotes(s, start string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}

	s = s[i+len(start):]

	j := strings.Index(s, `"`)
	if j < 0 {
		return ""
	}

	return s[:j]
}

type wantPass struct {
	Name     string
	Requests int
	Items    int
}

func TestGitHubDiscoveryPassAccounting(t *testing.T) {
	tests := []struct {
		name           string
		skipUserRepos  bool
		limitUserOwned bool
		orgs           []string
		repos          ghMockRepos
		orgNames       []string
		wantPasses     []wantPass
		wantRepos      []string
		wantDeduped    int
	}{
		{
			name:  "user repos only, no orgs",
			repos: ghMockRepos{ghTestViewer: {"a", "b"}},
			wantPasses: []wantPass{
				// two repos, one per page, plus the final empty page
				{Name: ghLabelUserRepos, Requests: 2, Items: 2},
			},
			wantRepos: []string{ghTestUserRepoA, "user/b"},
		},
		{
			name:           "LimitUserOwned still issues exactly one pass",
			limitUserOwned: true,
			repos:          ghMockRepos{ghTestViewer: {"a"}},
			wantPasses: []wantPass{
				{Name: ghLabelUserRepos, Requests: 1, Items: 1},
			},
			wantRepos: []string{ghTestUserRepoA},
		},
		{
			name:          "SkipUserRepos with one explicit org",
			skipUserRepos: true,
			orgs:          []string{ghTestOrg},
			repos:         ghMockRepos{ghTestViewer: {"a"}, ghTestOrg: {"x", "y"}},
			wantPasses: []wantPass{
				{Name: "org my-org", Requests: 2, Items: 2},
			},
			wantRepos: []string{"my-org/x", "my-org/y"},
		},
		{
			name:  "user repos plus explicit orgs, no wildcard means no orgs-list call",
			orgs:  []string{ghTestOrgOne, ghTestOrgTwo},
			repos: ghMockRepos{ghTestViewer: {"a"}, ghTestOrgOne: {"x"}, ghTestOrgTwo: {"y"}},
			wantPasses: []wantPass{
				{Name: ghLabelUserRepos, Requests: 1, Items: 1},
				{Name: ghLabelOrgOne, Requests: 1, Items: 1},
				{Name: "org org-two", Requests: 1, Items: 1},
			},
			wantRepos: []string{ghTestOrgOneRepo, "org-two/y", ghTestUserRepoA},
		},
		{
			name:     "wildcard adds an orgs-list pass then one pass per discovered org",
			orgs:     []string{"*"},
			orgNames: []string{ghTestOrgOne, ghTestOrgTwo},
			repos:    ghMockRepos{ghTestViewer: {"a"}, ghTestOrgOne: {"x"}, ghTestOrgTwo: {"y"}},
			wantPasses: []wantPass{
				{Name: ghLabelUserRepos, Requests: 1, Items: 1},
				{Name: ghLabelOrgsList, Requests: 1, Items: 2},
				{Name: ghLabelOrgOne, Requests: 1, Items: 1},
				{Name: "org org-two", Requests: 1, Items: 1},
			},
			wantRepos: []string{ghTestOrgOneRepo, "org-two/y", ghTestUserRepoA},
		},
		{
			name:     "wildcard combined with an explicitly listed org",
			orgs:     []string{"*", "extra-org"},
			orgNames: []string{ghTestOrgOne},
			repos:    ghMockRepos{ghTestViewer: {"a"}, ghTestOrgOne: {"x"}, "extra-org": {"z"}},
			wantPasses: []wantPass{
				{Name: ghLabelUserRepos, Requests: 1, Items: 1},
				{Name: ghLabelOrgsList, Requests: 1, Items: 1},
				{Name: "org extra-org", Requests: 1, Items: 1},
				{Name: ghLabelOrgOne, Requests: 1, Items: 1},
			},
			wantRepos: []string{"extra-org/z", ghTestOrgOneRepo, ghTestUserRepoA},
		},
		{
			name:          "SkipUserRepos with wildcard still lists orgs",
			skipUserRepos: true,
			orgs:          []string{"*"},
			orgNames:      []string{ghTestOrgOne},
			repos:         ghMockRepos{ghTestViewer: {"a"}, ghTestOrgOne: {"x"}},
			wantPasses: []wantPass{
				{Name: ghLabelOrgsList, Requests: 1, Items: 1},
				{Name: ghLabelOrgOne, Requests: 1, Items: 1},
			},
			wantRepos: []string{ghTestOrgOneRepo},
		},
		{
			name:          "no orgs and SkipUserRepos yields no passes at all",
			skipUserRepos: true,
			wantPasses:    nil,
			wantRepos:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(githubEnvVarLogAPIUsage, ghEnabledValue)

			var seen []string

			srv := mockServer(t, ghMockHandler(t, tc.repos, tc.orgNames, &seen))

			host, err := NewGitHubHost(NewGitHubHostInput{
				HTTPClient:     testHTTPClient(),
				APIURL:         srv.URL,
				BackupDir:      t.TempDir(),
				Token:          ghFixtureAuth,
				SkipUserRepos:  tc.skipUserRepos,
				LimitUserOwned: tc.limitUserOwned,
				Orgs:           tc.orgs,
			})
			require.NoError(t, err)

			result, err := host.describeRepos()
			require.NoError(t, err)

			var got []string
			for _, r := range result.Repos {
				got = append(got, r.PathWithNameSpace)
			}

			require.ElementsMatch(t, tc.wantRepos, got, "discovered repo set")

			gotPasses := make([]wantPass, 0, len(host.apiStats.passes))
			for _, p := range host.apiStats.passes {
				gotPasses = append(gotPasses, wantPass{Name: p.Name, Requests: p.Requests, Items: p.Items})
			}

			require.ElementsMatch(t, tc.wantPasses, gotPasses, "per-pass accounting")

			var wantRequests int
			for _, p := range tc.wantPasses {
				wantRequests += p.Requests
			}

			require.Equal(t, wantRequests, host.apiStats.totalRequests(), "total requests")
			require.Len(t, seen, wantRequests, "requests actually issued to the server")

			if tc.limitUserOwned {
				require.Contains(t, seen[0], "affiliations: OWNER ownerAffiliations: OWNER")
			}
		})
	}
}

// TestGitHubDiscoveryDedupeIsCounted proves the dedupe step still collapses a
// repository returned by more than one pass, and that the pass counters report
// the pre-dedupe totals.
func TestGitHubDiscoveryDedupeIsCounted(t *testing.T) {
	t.Setenv(githubEnvVarLogAPIUsage, ghEnabledValue)

	var seen []string

	// the viewer pass and the org pass both return my-org/shared
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		seen = append(seen, req.Query)

		if strings.Contains(req.Query, "organization(login:") {
			resp := githubQueryOrgResponse{}
			resp.Data.Organization.Repositories.Edges = []edge{ghEdge(ghTestOrg, "shared")}

			writeJSON(w, resp)

			return
		}

		resp := githubQueryNamesResponse{}
		resp.Data.Viewer.Repositories.Edges = []edge{ghEdge(ghTestOrg, "shared")}

		writeJSON(w, resp)
	}

	srv := mockServer(t, http.HandlerFunc(handler))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		Token:      ghFixtureAuth,
		Orgs:       []string{ghTestOrg},
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err)

	require.Len(t, result.Repos, 1, "duplicate should collapse to one repo")
	require.Equal(t, "my-org/shared", result.Repos[0].PathWithNameSpace)

	// both passes counted their own copy before dedupe
	require.Equal(t, 2, host.apiStats.totalRequests())

	var items int
	for _, p := range host.apiStats.passes {
		items += p.Items
	}

	require.Equal(t, 2, items, "pass counters report pre-dedupe totals")
}

func TestGitHubMaxConcurrent(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "unset uses default", env: "", want: defaultMaxConcurrentGitHub},
		{name: "override lowers concurrency", env: "3", want: 3},
		{name: "override raises concurrency", env: "20", want: 20},
		{name: "one worker is valid", env: "1", want: 1},
		{name: "zero ignored, would stall the backup", env: "0", want: defaultMaxConcurrentGitHub},
		{name: "negative ignored", env: "-4", want: defaultMaxConcurrentGitHub},
		{name: "unparsable ignored", env: "many", want: defaultMaxConcurrentGitHub},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(githubEnvVarMaxConcurrent, tc.env)

			require.Equal(t, tc.want, githubMaxConcurrent())
		})
	}
}
