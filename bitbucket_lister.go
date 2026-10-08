package githosts

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"gitlab.com/tozd/go/errors"
)

// bitbucketOAuthCloneUser is the username Bitbucket expects alongside an
// OAuth access token over HTTPS.
const bitbucketOAuthCloneUser = "x-token-auth"

const bitbucketBytesPerKB = 1024

var _ Lister = (*BitbucketHost)(nil)

type bitbucketMemberUser struct {
	// UUID identifies the account, in braces, e.g. {...}. Unlike the nickname
	// Bitbucket offers in place of the retired username, it is unique.
	UUID string `json:"uuid"`
}

type bitbucketMembership struct {
	User bitbucketMemberUser `json:"user"`
}

type bitbucketGetMembersResponse struct {
	Values []bitbucketMembership `json:"values"`
	Next   string                `json:"next"`
}

// cloneAuth returns the credentials Backup clones with: an OAuth access token
// takes precedence over an API token. With neither there are none.
func (bb BitbucketHost) cloneAuth() BasicAuth {
	switch {
	case bb.OAuthToken != "":
		return BasicAuth{User: bitbucketOAuthCloneUser, Password: bb.OAuthToken}
	case bb.APIToken != "":
		return BasicAuth{User: bitbucketStaticUserName, Password: bb.APIToken}
	default:
		return BasicAuth{}
	}
}

// bitbucketHTTPSCloneURL returns the HTTPS clone URL for a repository's full
// name. It is built rather than taken from the API's clone links, as those
// embed the requesting user's name.
func bitbucketHTTPSCloneURL(fullName string) string {
	return "https://bitbucket.org/" + fullName + ".git"
}

// bitbucketSSHCloneURL returns the API's ssh clone link, if it gave one.
func bitbucketSSHCloneURL(p bitbucketProject) string {
	for _, c := range p.Links.Clone {
		if c.Name == "ssh" {
			return c.Href
		}
	}

	return ""
}

// exportBitbucketProjects converts git repositories from a workspace listing
// to the exported form; anything else is skipped, as Backup skips it.
//
// Owner is the workspace ID, the first segment of full_name, as the API no
// longer reports owners' usernames. Name is the repository's display name, as
// Backup uses it; full_name carries the slug. Bitbucket Cloud has no archived
// state, so IsArchived is always false. A repository is reported empty when
// it has no main branch, which Bitbucket only sets once there is a commit.
// Size is rounded up to whole kilobytes, so that only an unknown or zero size
// reports 0.
func exportBitbucketProjects(projects []bitbucketProject, auth BasicAuth) []Repository {
	out := make([]Repository, 0, len(projects))

	for _, p := range projects {
		if p.Scm != "git" {
			continue
		}

		owner, _, _ := strings.Cut(p.FullName, "/")

		out = append(out, Repository{
			Owner:             owner,
			Name:              p.Name,
			PathWithNamespace: p.FullName,
			Domain:            bitbucketDomain,
			CloneURL:          bitbucketHTTPSCloneURL(p.FullName),
			SSHURL:            bitbucketSSHCloneURL(p),
			Auth:              auth,
			IsFork:            p.Parent != nil,
			IsEmpty:           p.MainBranch == nil || p.MainBranch.Name == "",
			IsPrivate:         p.IsPrivate,
			SizeKB:            (p.Size + bitbucketBytesPerKB - 1) / bitbucketBytesPerKB,
		})
	}

	return out
}

// ListOwnRepos lists the repositories Backup would: those the authenticated
// user is a member of in each configured workspace, or in every workspace the
// user can access when none are configured.
func (bb *BitbucketHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	if bb.AuthType != AuthTypeBitbucketOAuth2 && bb.AuthType != AuthTypeBitbucketAPIToken {
		return nil, errors.New("no authentication method available - need either OAuth key/secret or API token/email")
	}

	workspaces, err := bb.getWorkspaces(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get BitBucket workspaces")
	}

	var projects []bitbucketProject

	for _, workspace := range workspaces {
		p, lErr := bb.listWorkspaceProjects(ctx, workspace, "member")
		if lErr != nil {
			return nil, lErr
		}

		projects = append(projects, p...)
	}

	return exportBitbucketProjects(projects, bb.cloneAuth()), nil
}

// ListUserRepos lists the repositories in the named user's personal
// workspace that the credentials can see. Bitbucket separates usernames from
// workspace IDs, so user is the personal workspace's ID, as shown in the
// user's repository URLs, or a UUID in braces as ListOrgMembers returns.
func (bb *BitbucketHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	return bb.listWorkspaceRepos(ctx, user)
}

// ListOrgRepos lists the repositories in the named workspace that the
// credentials can see.
func (bb *BitbucketHost) ListOrgRepos(ctx context.Context, workspace string) ([]Repository, error) {
	return bb.listWorkspaceRepos(ctx, workspace)
}

func (bb *BitbucketHost) listWorkspaceRepos(ctx context.Context, workspace string) ([]Repository, error) {
	if workspace == "" {
		return nil, errors.New("BitBucket workspace not provided")
	}

	projects, err := bb.listWorkspaceProjects(ctx, workspace, "")
	if err != nil {
		return nil, err
	}

	return exportBitbucketProjects(projects, bb.cloneAuth()), nil
}

// ListOrgMembers returns the UUIDs, in braces, of the named workspace's
// members. Bitbucket no longer reports usernames, and the nickname it
// documents in their place is not unique, so the UUID is returned instead. A
// UUID in braces is accepted wherever Bitbucket's API takes a workspace, so a
// member's UUID can be passed to ListUserRepos.
func (bb *BitbucketHost) ListOrgMembers(ctx context.Context, workspace string) ([]string, error) {
	if workspace == "" {
		return nil, errors.New("BitBucket workspace not provided")
	}

	rawRequestURL := bb.APIURL + "/workspaces/" + url.PathEscape(workspace) + "/members"

	var members []string

	for rawRequestURL != "" {
		body, err := bb.bitbucketAuthenticatedGet(ctx, rawRequestURL)
		if err != nil {
			return nil, errors.Errorf("failed to list members of BitBucket workspace %s: %s", workspace, err)
		}

		var respObj bitbucketGetMembersResponse
		if jErr := json.Unmarshal(body, &respObj); jErr != nil {
			return nil, errors.Wrap(jErr, "failed to unmarshal BitBucket workspace members response")
		}

		for _, m := range respObj.Values {
			members = append(members, m.User.UUID)
		}

		rawRequestURL = respObj.Next
	}

	return members, nil
}
