package githosts

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

const azureDevOpsOriginalHostHeader = "X-Test-Original-Host"

// redirectTransport sends every request to target, recording the host the
// request was meant for, as the Azure DevOps hosts are fixed.
type redirectTransport struct {
	target *url.URL
}

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(azureDevOpsOriginalHostHeader, req.URL.Host)
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	req.Host = rt.target.Host

	return http.DefaultTransport.RoundTrip(req)
}

// newTestAzureDevOpsHost returns a host whose requests are all served by mux.
func newTestAzureDevOpsHost(t *testing.T, mux *http.ServeMux) *AzureDevOpsHost {
	t.Helper()

	srv := mockServer(t, mux)

	target, err := url.Parse(srv.URL)
	require.NoError(t, err)

	client := testHTTPClient()
	client.HTTPClient.Transport = redirectTransport{target: target}

	return &AzureDevOpsHost{
		HttpClient: client,
		UserName:   "test-user",
		PAT:        "test-pat",
		Orgs:       []string{"acme"},
	}
}

// requireAzureDevOpsRequest checks the request's host, PAT and API version.
func requireAzureDevOpsRequest(t *testing.T, r *http.Request, host string) {
	t.Helper()

	require.Equal(t, host, r.Header.Get(azureDevOpsOriginalHostHeader))
	require.Equal(t, AuthPrefixBasic+base64.StdEncoding.EncodeToString([]byte("test-user:test-pat")),
		r.Header.Get(HeaderAuthorization))
	require.Equal(t, azureDevOpsAPIVersion, r.URL.Query().Get("api-version"))
}

// azureDevOpsOrgMux serves two pages of projects for acme: a private
// project with two repositories, then a public one with one.
func azureDevOpsOrgMux(t *testing.T) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/acme/_apis/projects", func(w http.ResponseWriter, r *http.Request) {
		requireAzureDevOpsRequest(t, r, azureDevOpsDomain)

		if r.URL.Query().Get("continuationToken") == "1" {
			writeRaw(w, `{"count":1,"value":[{"id":"p2","name":"Open Source","visibility":"public"}]}`)

			return
		}

		w.Header().Set(azureDevOpsContinuationHeader, "1")
		writeRaw(w, `{"count":1,"value":[{"id":"p1","name":"Internal","visibility":"private"}]}`)
	})

	mux.HandleFunc("/acme/p1/_apis/git/repositories", func(w http.ResponseWriter, r *http.Request) {
		requireAzureDevOpsRequest(t, r, azureDevOpsDomain)

		writeRaw(w, `{"count":2,"value":[
			{"id":"r1","name":"service","project":{"id":"p1","name":"Internal"},"defaultBranch":"refs/heads/main",
			 "size":1500,"remoteUrl":"https://acme@dev.azure.com/acme/Internal/_git/service",
			 "sshUrl":"git@ssh.dev.azure.com:v3/acme/Internal/service","webUrl":"https://dev.azure.com/acme/Internal/_git/service"},
			{"id":"r2","name":"empty-fork","project":{"id":"p1","name":"Internal"},"isFork":true,"isDisabled":true,"size":0,
			 "remoteUrl":"https://acme@dev.azure.com/acme/Internal/_git/empty-fork",
			 "sshUrl":"git@ssh.dev.azure.com:v3/acme/Internal/empty-fork","webUrl":"https://dev.azure.com/acme/Internal/_git/empty-fork"}
		]}`)
	})

	mux.HandleFunc("/acme/p2/_apis/git/repositories", func(w http.ResponseWriter, r *http.Request) {
		requireAzureDevOpsRequest(t, r, azureDevOpsDomain)

		writeRaw(w, `{"count":1,"value":[
			{"id":"r3","name":"lib","project":{"id":"p2","name":"Open Source"},"defaultBranch":"refs/heads/main","size":2048,
			 "remoteUrl":"https://acme@dev.azure.com/acme/Open%20Source/_git/lib",
			 "sshUrl":"git@ssh.dev.azure.com:v3/acme/Open%20Source/lib","webUrl":"https://dev.azure.com/acme/Open%20Source/_git/lib"}
		]}`)
	})

	return mux
}

func TestAzureDevOpsListOrgRepos(t *testing.T) {
	repos, err := newTestAzureDevOpsHost(t, azureDevOpsOrgMux(t)).ListOrgRepos(context.Background(), "acme")
	require.NoError(t, err)

	auth := BasicAuth{User: "test-user", Password: "test-pat"}

	require.Equal(t, []Repository{
		{
			Owner:             "acme",
			Name:              "service",
			PathWithNamespace: "acme/Internal/service",
			Domain:            azureDevOpsDomain,
			CloneURL:          "https://dev.azure.com/acme/Internal/_git/service",
			SSHURL:            "git@ssh.dev.azure.com:v3/acme/Internal/service",
			Auth:              auth,
			IsPrivate:         true,
			SizeKB:            2,
		},
		{
			Owner:             "acme",
			Name:              "empty-fork",
			PathWithNamespace: "acme/Internal/empty-fork",
			Domain:            azureDevOpsDomain,
			CloneURL:          "https://dev.azure.com/acme/Internal/_git/empty-fork",
			SSHURL:            "git@ssh.dev.azure.com:v3/acme/Internal/empty-fork",
			Auth:              auth,
			IsFork:            true,
			IsEmpty:           true,
			IsPrivate:         true,
		},
		{
			Owner:             "acme",
			Name:              "lib",
			PathWithNamespace: "acme/Open Source/lib",
			Domain:            azureDevOpsDomain,
			CloneURL:          "https://dev.azure.com/acme/Open%20Source/_git/lib",
			SSHURL:            "git@ssh.dev.azure.com:v3/acme/Open%20Source/lib",
			Auth:              auth,
			SizeKB:            2,
		},
	}, repos)

	for _, r := range repos {
		require.NotContains(t, r.CloneURL, "@", "clone URL must not carry credentials")
		require.False(t, r.IsArchived, "a disabled repository is not reported as archived")
	}
}

func TestAzureDevOpsListOwnReposUsesFirstOrg(t *testing.T) {
	host := newTestAzureDevOpsHost(t, azureDevOpsOrgMux(t))
	host.Orgs = []string{"acme", "other"}

	repos, err := host.ListOwnRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 3)

	host.Orgs = nil

	_, err = host.ListOwnRepos(context.Background())
	require.ErrorContains(t, err, "no organizations specified")
}

func TestAzureDevOpsListOrgReposErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/missing/_apis/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/badpat/_apis/projects", func(w http.ResponseWriter, _ *http.Request) {
		// Azure DevOps answers an invalid PAT with a sign-in page
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNonAuthoritativeInfo)
		_, _ = w.Write([]byte("<html>sign in</html>"))
	})

	host := newTestAzureDevOpsHost(t, mux)
	ctx := context.Background()

	_, err := host.ListOrgRepos(ctx, "missing")
	require.ErrorContains(t, err, "HTTP 404")

	_, err = host.ListOrgRepos(ctx, "badpat")
	require.ErrorContains(t, err, "authentication failed")

	_, err = host.ListOrgRepos(ctx, "")
	require.ErrorContains(t, err, "organization not specified")
}

func TestAzureDevOpsListUserReposNotSupported(t *testing.T) {
	_, err := newTestAzureDevOpsHost(t, http.NewServeMux()).ListUserRepos(context.Background(), "someone")
	require.ErrorIs(t, err, ErrNotSupported)
	require.True(t, errors.Is(err, ErrNotSupported))
}

func TestAzureDevOpsListOrgMembersPaginates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/acme/_apis/userentitlements", func(w http.ResponseWriter, r *http.Request) {
		requireAzureDevOpsRequest(t, r, azureDevOpsEntitlementsDomain)

		if r.URL.Query().Get("continuationToken") == "next+page" {
			writeRaw(w, `{"items":[{"user":{"mailAddress":"bob@example.com"}}],"continuationToken":null}`)

			return
		}

		writeRaw(w, `{"items":[{"user":{"principalName":"alice@example.com","mailAddress":"alice.other@example.com"}}],
			"continuationToken":"next+page","totalCount":2}`)
	})

	members, err := newTestAzureDevOpsHost(t, mux).ListOrgMembers(context.Background(), "acme")
	require.NoError(t, err)
	require.Equal(t, []string{"alice@example.com", "bob@example.com"}, members)
}

func TestAzureDevOpsListOrgMembersForbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/acme/_apis/userentitlements", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := newTestAzureDevOpsHost(t, mux).ListOrgMembers(context.Background(), "acme")
	require.ErrorContains(t, err, "403")
}

func TestAzureDevOpsListerHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestAzureDevOpsHost(t, azureDevOpsOrgMux(t)).ListOrgRepos(ctx, "acme")
	require.Error(t, err)
}
