package githosts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/peterhellberg/link"
	"gitlab.com/tozd/go/errors"
)

type gitLabMember struct {
	UserName string `json:"username"`
}

// cloneAuth returns the credentials Backup clones with: the token's username
// and the token itself.
func (gl *GitLabHost) cloneAuth(ctx context.Context) (BasicAuth, errors.E) {
	user := gl.User
	if user.ID == 0 {
		var err errors.E

		user, err = gl.authenticatedGitLabUser(ctx)
		if err != nil {
			return BasicAuth{}, err
		}

		if user.ID == 0 {
			return BasicAuth{}, errors.New("GitLab authentication failed")
		}
	}

	return BasicAuth{User: user.UserName, Password: stripTrailing(gl.Token, "\n")}, nil
}

// gitLabListURL returns the API URL for path, with paging applied and the
// given extra query parameters.
func (gl *GitLabHost) gitLabListURL(path string, params url.Values) (string, errors.E) {
	apiURL := gl.APIURL
	if apiURL == "" {
		apiURL = gitlabAPIURL
	}

	u, err := url.Parse(apiURL + path)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse url")
	}

	if params == nil {
		params = url.Values{}
	}

	params.Set("per_page", strconv.Itoa(gitlabProjectsPerPageDefault))
	u.RawQuery = params.Encode()

	return u.String(), nil
}

// listProjectsAt lists every project at the given API path and exports them
// with the token's clone credentials.
func (gl *GitLabHost) listProjectsAt(ctx context.Context, path string, params url.Values) ([]Repository, error) {
	auth, err := gl.cloneAuth(ctx)
	if err != nil {
		return nil, err
	}

	reqURL, err := gl.gitLabListURL(path, params)
	if err != nil {
		return nil, err
	}

	repos, err := gl.listGitLabProjects(ctx, newGitLabListingClient(), reqURL)
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, auth), nil
}

// ListOwnRepos lists the projects the authenticated user can access at
// ProjectMinAccessLevel or above.
func (gl *GitLabHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	auth, err := gl.cloneAuth(ctx)
	if err != nil {
		return nil, err
	}

	repos, err := gl.getAllProjectRepositories(ctx, *newGitLabListingClient())
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, auth), nil
}

// ListUserRepos lists the personal projects of the named user that the token
// can see.
func (gl *GitLabHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	return gl.listProjectsAt(ctx, "/users/"+url.PathEscape(user)+"/projects", nil)
}

// ListOrgRepos lists the projects of the group with the given full path,
// including those in its subgroups.
func (gl *GitLabHost) ListOrgRepos(ctx context.Context, group string) ([]Repository, error) {
	return gl.listProjectsAt(ctx, "/groups/"+url.PathEscape(group)+"/projects",
		url.Values{"include_subgroups": {"true"}})
}

// ListOrgMembers returns the usernames of the group's members, including
// those who inherit membership from a parent group.
func (gl *GitLabHost) ListOrgMembers(ctx context.Context, group string) ([]string, error) {
	reqURL, err := gl.gitLabListURL("/groups/"+url.PathEscape(group)+"/members/all", nil)
	if err != nil {
		return nil, err
	}

	client := newGitLabListingClient()

	var members []string

	for reqURL != "" {
		resp, body, rErr := makeGitLabRequest(ctx, client, reqURL, gl.Token) //nolint:bodyclose // response body is closed in makeGitLabRequest
		if rErr != nil {
			return nil, rErr
		}

		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			return nil, errors.Errorf("GitLab group %s not found (HTTP 404)", group)
		default:
			return nil, errors.Errorf("failed to get members of GitLab group %s: %d (%s)", group, resp.StatusCode, resp.Status)
		}

		var page []gitLabMember
		if uErr := json.Unmarshal(body, &page); uErr != nil {
			return nil, errors.Wrap(uErr, "failed to unmarshal GitLab members response")
		}

		for _, m := range page {
			members = append(members, m.UserName)
		}

		reqURL = ""

		for _, l := range link.ParseResponse(resp) {
			if l.Rel == txtNext {
				reqURL = l.URI
			}
		}
	}

	return members, nil
}
