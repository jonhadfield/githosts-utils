package githosts

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// An organization's login is the immutable handle in its URL and the only value
// organization(login:) resolves. Its name is a mutable, human-readable display
// name that frequently differs. These fixtures keep the two clearly distinct.
const (
	testOrgLogin       = "acme-inc"
	testOrgDisplayName = "Acme Incorporated"
)

// orgLookupServer emulates the two GraphQL queries the wildcard org path
// issues, resolving them the way GitHub does rather than the way the client
// hopes: viewer.organizations answers with whichever of login/name the
// selection set actually asked for, and organization(login:) resolves only the
// login, returning NOT_FOUND for anything else - including the display name.
func orgLookupServer(t *testing.T, orgLookups *[]string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		switch {
		case strings.Contains(req.Query, "organizations(first"):
			// answer strictly the requested field, as the real API would
			if strings.Contains(req.Query, "node { login }") {
				writeRaw(w, `{"data":{"viewer":{"organizations":{"edges":[{"node":{"login":"`+testOrgLogin+`"}}]}}}}`)

				return
			}

			writeRaw(w, `{"data":{"viewer":{"organizations":{"edges":[{"node":{"name":"`+testOrgDisplayName+`"}}]}}}}`)

		case strings.Contains(req.Query, "organization(login:"):
			login := betweenQuotes(req.Query, `organization(login: "`)
			*orgLookups = append(*orgLookups, login)

			if login != testOrgLogin {
				writeRaw(w, `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to an Organization with the login of '`+login+`'."}]}`)

				return
			}

			writeRaw(w, `{"data":{"organization":{"repositories":{"edges":[{"node":{"name":"repo-one","nameWithOwner":"`+
				testOrgLogin+`/repo-one","url":"https://github.com/`+testOrgLogin+`/repo-one","sshUrl":"git@github.com:`+
				testOrgLogin+`/repo-one.git"},"cursor":"1"}],"pageInfo":{"endCursor":"1","hasNextPage":false}}}}}`)

		default:
			writeRaw(w, `{"data":{"viewer":{"repositories":{"edges":[],"pageInfo":{"endCursor":"","hasNextPage":false}}}}}`)
		}
	}
}

func writeRaw(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

// TestGitHubWildcardOrgsResolveByLogin is a regression test for looking
// organizations up by their display name. viewer.organizations was selecting
// name, so every organization whose display name differed from its login was
// then queried as organization(login: "<display name>"), which GitHub cannot
// resolve - failing the entire discovery run with NOT_FOUND and taking every
// repository in every organization down with it.
func TestGitHubWildcardOrgsResolveByLogin(t *testing.T) {
	var orgLookups []string

	srv := mockServer(t, orgLookupServer(t, &orgLookups))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient:    testHTTPClient(),
		APIURL:        srv.URL,
		BackupDir:     t.TempDir(),
		Token:         ghFixtureAuth,
		SkipUserRepos: true,
		Orgs:          []string{"*"},
	})
	require.NoError(t, err)

	result, err := host.describeRepos()
	require.NoError(t, err, "discovery must not fail when an org's display name differs from its login")

	require.Equal(t, []string{testOrgLogin}, orgLookups,
		"organizations must be looked up by login, never by display name")

	require.Len(t, result.Repos, 1)
	require.Equal(t, testOrgLogin+"/repo-one", result.Repos[0].PathWithNameSpace)
}

// TestGitHubUserOrganizationsQueriesLogin pins the selection set itself, so a
// future edit cannot quietly go back to requesting name.
func TestGitHubUserOrganizationsQueriesLogin(t *testing.T) {
	var queries []string

	srv := mockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req graphQLRequest
		require.NoError(t, json.Unmarshal(body, &req))

		queries = append(queries, req.Query)

		writeRaw(w, `{"data":{"viewer":{"organizations":{"edges":[{"node":{"login":"`+testOrgLogin+`"}}]}}}}`)
	}))

	host, err := NewGitHubHost(NewGitHubHostInput{
		HTTPClient: testHTTPClient(),
		APIURL:     srv.URL,
		BackupDir:  t.TempDir(),
		Token:      ghFixtureAuth,
	})
	require.NoError(t, err)

	orgs, err := host.describeGithubUserOrganizations()
	require.NoError(t, err)

	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "node { login }")
	require.NotContains(t, queries[0], "node { name }")

	require.Equal(t, []githubOrganization{{Login: testOrgLogin}}, orgs)
}
