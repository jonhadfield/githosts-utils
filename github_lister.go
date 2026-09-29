package githosts

import (
	"context"
	"encoding/json"
	"strconv"

	"gitlab.com/tozd/go/errors"
)

// githubCloneUser is the username sent with a token over HTTPS. GitHub
// authenticates on the token alone and accepts any username for a personal
// access token; x-access-token is the one it documents for app tokens.
const githubCloneUser = "x-access-token"

type githubQueryMembersResponse struct {
	Data struct {
		Organization struct {
			MembersWithRole struct {
				Nodes []struct {
					Login string `json:"login"`
				} `json:"nodes"`
				PageInfo struct {
					EndCursor   string
					HasNextPage bool
				}
			} `json:"membersWithRole"`
		}
	}
	Errors []struct {
		Type    string
		Path    []string
		Message string
	}
}

func (gh *GitHubHost) cloneAuth() BasicAuth {
	return BasicAuth{User: githubCloneUser, Password: stripTrailing(gh.Token, "\n")}
}

// ListOwnRepos lists the authenticated user's repositories, honouring
// LimitUserOwned.
func (gh *GitHubHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	repos, err := gh.describeGithubUserRepos(ctx)
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, gh.cloneAuth()), nil
}

// ListUserRepos lists the repositories the named user owns.
func (gh *GitHubHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	if err := validGitHubLogin(user); err != nil {
		return nil, err
	}

	repos, err := gh.describeGithubOwnerRepos(ctx, githubOwnerUser, user)
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, gh.cloneAuth()), nil
}

// ListOrgRepos lists the named organization's repositories.
func (gh *GitHubHost) ListOrgRepos(ctx context.Context, org string) ([]Repository, error) {
	if err := validGitHubLogin(org); err != nil {
		return nil, err
	}

	repos, err := gh.describeGithubOrgRepos(ctx, org)
	if err != nil {
		return nil, err
	}

	return exportRepositories(repos, gh.cloneAuth()), nil
}

// ListOrgMembers returns the logins of the named organization's members. The
// token needs the read:org scope to see members who keep their membership
// private.
func (gh *GitHubHost) ListOrgMembers(ctx context.Context, org string) ([]string, error) {
	if err := validGitHubLogin(org); err != nil {
		return nil, err
	}

	var members []string

	after := ""

	for {
		args := "first:" + strconv.Itoa(gitHubCallSize)
		if after != "" {
			args += " after: \"" + after + "\""
		}

		payload, err := createGithubRequestPayload("query { organization(login: \"" + org + "\") { membersWithRole(" + args +
			") { nodes { login } pageInfo { endCursor hasNextPage } } } }")
		if err != nil {
			return nil, err
		}

		bodyStr, err := gh.makeGithubRequest(ctx, payload)
		if err != nil {
			return nil, err
		}

		var respObj githubQueryMembersResponse
		if uErr := json.Unmarshal([]byte(bodyStr), &respObj); uErr != nil {
			return nil, errors.Wrap(uErr, "failed to unmarshal response")
		}

		for _, gqlErr := range respObj.Errors {
			if gqlErr.Type == "NOT_FOUND" {
				return nil, errors.Errorf("organization %s not found", org)
			}

			return nil, errors.Errorf("unexpected error: type: %s message: %s", gqlErr.Type, gqlErr.Message)
		}

		conn := respObj.Data.Organization.MembersWithRole
		for _, n := range conn.Nodes {
			members = append(members, n.Login)
		}

		if !conn.PageInfo.HasNextPage {
			break
		}

		after = conn.PageInfo.EndCursor
	}

	return members, nil
}
