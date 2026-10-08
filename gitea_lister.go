package githosts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterhellberg/link"
	"gitlab.com/tozd/go/errors"
)

const (
	// giteaListPageSize is the page size the listers request. Gitea and
	// Forgejo cap it at the instance's MAX_RESPONSE_ITEMS, 50 by default, and
	// the Link header is followed either way.
	giteaListPageSize = 50
	// giteaCloneUser is the username sent with a token over HTTPS. Backup
	// puts the token in the URL as the username; Gitea and Forgejo also take a
	// token as the password, in which case they ignore the username. Sending
	// the token as the password keeps it out of the field callers tend to log.
	giteaCloneUser = "token"
)

var _ Lister = (*GiteaHost)(nil)

// giteaAPILister lists repositories and members through the /api/v1 API that
// Gitea and Forgejo share. The Gitea and Codeberg hosts differ only in how
// they discover their own repositories, so everything else is implemented
// once, here.
type giteaAPILister struct {
	httpClient *retryablehttp.Client
	apiURL     string
	token      string
	provider   string
}

func (g *GiteaHost) apiLister() giteaAPILister {
	return giteaAPILister{httpClient: g.httpClient, apiURL: g.APIURL, token: g.Token, provider: giteaProviderName}
}

func (cb *CodebergHost) apiLister() giteaAPILister {
	return giteaAPILister{httpClient: cb.httpClient, apiURL: cb.APIURL, token: cb.Token, provider: codebergProviderName}
}

// cleanToken returns the configured token without the trailing newline a
// token read from a file often carries.
func (l giteaAPILister) cleanToken() string {
	return stripTrailing(l.token, "\n")
}

func (l giteaAPILister) hasToken() bool {
	return strings.TrimSpace(l.token) != ""
}

// cloneAuth returns the credentials Backup clones with. Without a token there
// are none, and only public repositories can be listed and cloned.
func (l giteaAPILister) cloneAuth() BasicAuth {
	if !l.hasToken() {
		return BasicAuth{}
	}

	return BasicAuth{User: giteaCloneUser, Password: l.cleanToken()}
}

// get requests reqURL, sending the token when there is one.
func (l giteaAPILister) get(parent context.Context, reqURL string) (*http.Response, []byte, errors.E) {
	ctx, cancel := context.WithTimeout(parent, defaultHttpRequestTimeout)
	defer cancel()

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create %s request", l.provider)
	}

	// without a token, only public data can be listed
	if l.hasToken() {
		req.Header.Set(HeaderAuthorization, AuthPrefixToken+l.cleanToken())
	}

	req.Header.Set(HeaderAccept, contentTypeApplicationJSON)

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "%s request failed", l.provider)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to read response body")
	}

	return resp, body, nil
}

// checkStatus turns an unsuccessful response for what into an error.
func (l giteaAPILister) checkStatus(resp *http.Response, what string) errors.E {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return errors.Errorf("%s %s not found (HTTP 404)", l.provider, what)
	case http.StatusUnauthorized:
		return errors.Errorf("failed to get %s %s: invalid or missing credentials (HTTP 401)", l.provider, what)
	case http.StatusForbidden:
		return errors.Errorf("failed to get %s %s: access denied (HTTP 403)", l.provider, what)
	default:
		return errors.Errorf("failed to get %s %s: %d (%s)", l.provider, what, resp.StatusCode, resp.Status)
	}
}

// paginate requests every page of the collection at path, following the
// Link header's rel="next", and hands each page's body to process.
func (l giteaAPILister) paginate(ctx context.Context, path, what string, process func([]byte) error) errors.E {
	if strings.TrimSpace(l.apiURL) == "" {
		return errors.Errorf("%s API URL missing", l.provider)
	}

	u, err := url.Parse(strings.TrimSuffix(l.apiURL, "/") + path)
	if err != nil {
		return errors.Wrapf(err, "failed to parse %s URL", what)
	}

	q := u.Query()
	q.Set("limit", strconv.Itoa(giteaListPageSize))
	u.RawQuery = q.Encode()

	for reqURL := u.String(); reqURL != ""; {
		resp, body, rErr := l.get(ctx, reqURL) //nolint:bodyclose // response body is closed in get
		if rErr != nil {
			return rErr
		}

		if sErr := l.checkStatus(resp, what); sErr != nil {
			return sErr
		}

		if pErr := process(body); pErr != nil {
			return errors.Wrapf(pErr, "failed to unmarshal %s response", what)
		}

		reqURL = ""

		for _, lnk := range link.ParseResponse(resp) {
			if lnk.Rel == txtNext {
				reqURL = lnk.URI
			}
		}
	}

	return nil
}

// listRepos lists every repository at path, keeping those keep accepts, or
// all of them when keep is nil.
func (l giteaAPILister) listRepos(ctx context.Context, path, what string, keep func(giteaRepository) bool) ([]Repository, errors.E) {
	var repos []repository

	err := l.paginate(ctx, path, what, func(body []byte) error {
		var page []giteaRepository
		if uErr := json.Unmarshal(body, &page); uErr != nil {
			return uErr //nolint:wrapcheck // context is added by paginate
		}

		for _, r := range page {
			if keep == nil || keep(r) {
				repos = append(repos, l.toRepository(r))
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, l.cloneAuth()), nil
}

// toRepository converts a repository as Gitea and Forgejo report it. Both
// report size in KiB: they store it in bytes and divide by 1024 when
// converting it for the API. Internal repositories, which any signed-in user
// can see, count as private.
func (l giteaAPILister) toRepository(r giteaRepository) repository {
	return repository{
		Name:              r.Name,
		Owner:             r.Owner.Login,
		PathWithNameSpace: r.FullName,
		Domain:            l.domainOf(r.CloneUrl),
		HTTPSUrl:          r.CloneUrl,
		SSHUrl:            r.SshUrl,
		IsFork:            r.Fork,
		IsArchived:        r.Archived,
		IsEmpty:           r.Empty,
		IsPrivate:         r.Private || r.Internal,
		SizeKB:            int64(r.Size),
	}
}

// domainOf returns the host the repository is cloned from, as Backup names
// its directory, falling back to the API URL's host.
func (l giteaAPILister) domainOf(cloneURL string) string {
	if u, err := url.Parse(cloneURL); err == nil && u.Host != "" {
		return u.Host
	}

	if u, err := url.Parse(l.apiURL); err == nil {
		return u.Host
	}

	return ""
}

func (l giteaAPILister) listUserRepos(ctx context.Context, user string) ([]Repository, errors.E) {
	return l.listRepos(ctx, "/users/"+url.PathEscape(user)+"/repos", "user "+user+" repositories", nil)
}

func (l giteaAPILister) listOrgRepos(ctx context.Context, org string) ([]Repository, errors.E) {
	return l.listRepos(ctx, "/orgs/"+url.PathEscape(org)+"/repos", "organization "+org+" repositories", nil)
}

// listLogins returns the login of every user listed at path.
func (l giteaAPILister) listLogins(ctx context.Context, path, what string) ([]string, errors.E) {
	var logins []string

	err := l.paginate(ctx, path, what, func(body []byte) error {
		var page []giteaUser
		if uErr := json.Unmarshal(body, &page); uErr != nil {
			return uErr //nolint:wrapcheck // context is added by paginate
		}

		for _, u := range page {
			logins = append(logins, u.Login)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return logins, nil
}

func (l giteaAPILister) listOrgMembers(ctx context.Context, org string) ([]string, errors.E) {
	return l.listLogins(ctx, "/orgs/"+url.PathEscape(org)+"/members", "organization "+org+" members")
}

// authenticatedLogin returns the login of the token's owner.
func (l giteaAPILister) authenticatedLogin(ctx context.Context) (string, errors.E) {
	resp, body, err := l.get(ctx, strings.TrimSuffix(l.apiURL, "/")+"/user") //nolint:bodyclose // response body is closed in get
	if err != nil {
		return "", err
	}

	if sErr := l.checkStatus(resp, "authenticated user"); sErr != nil {
		return "", sErr
	}

	var user giteaUser
	if uErr := json.Unmarshal(body, &user); uErr != nil {
		return "", errors.Wrap(uErr, "failed to unmarshal authenticated user response")
	}

	if user.Login == "" {
		return "", errors.Errorf("%s returned an empty login for the authenticated user", l.provider)
	}

	return user.Login, nil
}

func (l giteaAPILister) requireToken() errors.E {
	if !l.hasToken() {
		return errors.Errorf("%s token not provided", l.provider)
	}

	return nil
}

// ListOwnRepos lists the repositories of every user on the instance, as
// Backup does before adding the configured organisations. Users are
// enumerated through /admin/users, so the token must belong to a site
// administrator; any other token is refused with HTTP 403. Each user's
// repositories come from /users/{username}/repos.
func (g *GiteaHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	l := g.apiLister()

	if err := l.requireToken(); err != nil {
		return nil, err
	}

	users, err := l.listLogins(ctx, "/admin/users", "users")
	if err != nil {
		return nil, err
	}

	var repos []Repository

	for _, user := range users {
		userRepos, uErr := l.listUserRepos(ctx, user)
		if uErr != nil {
			return nil, uErr
		}

		repos = append(repos, userRepos...)
	}

	return repos, nil
}

// ListUserRepos lists the repositories the named user owns that the token can
// see, from /users/{username}/repos. Without a token, or with one that
// belongs to neither the user nor an administrator, private repositories are
// left out.
func (g *GiteaHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	return g.apiLister().listUserRepos(ctx, user)
}

// ListOrgRepos lists the named organisation's repositories that the token can
// see, from /orgs/{org}/repos.
func (g *GiteaHost) ListOrgRepos(ctx context.Context, org string) ([]Repository, error) {
	return g.apiLister().listOrgRepos(ctx, org)
}

// ListOrgMembers returns the logins of the named organisation's members, from
// /orgs/{org}/members. A caller that is neither a member of the organisation
// nor a site administrator, or has no token, sees only its public members.
func (g *GiteaHost) ListOrgMembers(ctx context.Context, org string) ([]string, error) {
	return g.apiLister().listOrgMembers(ctx, org)
}
