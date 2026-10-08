package githosts

import (
	"context"
	"strings"
)

// Codeberg runs Forgejo, which serves the same /api/v1 API as Gitea, so the
// listing itself is shared with the Gitea host through giteaAPILister. Only
// the discovery of the token's own repositories differs: it uses the
// token-scoped /user/repos rather than the Gitea host's admin endpoints.

var _ Lister = (*CodebergHost)(nil)

// ListOwnRepos lists the repositories the token can access through
// /user/repos, as Backup does before adding the configured organisations:
// those the user owns, those of organisations they belong to and those they
// collaborate on. LimitUserOwned keeps only those the token's user owns.
// SkipUserRepos is ignored, as it only stops Backup from using this listing.
func (cb *CodebergHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	l := cb.apiLister()

	if err := l.requireToken(); err != nil {
		return nil, err
	}

	var keep func(giteaRepository) bool

	if cb.LimitUserOwned {
		owner, err := l.authenticatedLogin(ctx)
		if err != nil {
			return nil, err
		}

		keep = func(r giteaRepository) bool {
			return strings.EqualFold(r.Owner.Login, owner)
		}
	}

	return l.listRepos(ctx, "/user/repos", "user repositories", keep)
}

// ListUserRepos lists the repositories the named user owns that the token can
// see, from /users/{username}/repos. Without a token, or with another user's,
// private repositories are left out.
func (cb *CodebergHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	return cb.apiLister().listUserRepos(ctx, user)
}

// ListOrgRepos lists the named organisation's repositories that the token can
// see, from /orgs/{org}/repos.
func (cb *CodebergHost) ListOrgRepos(ctx context.Context, org string) ([]Repository, error) {
	return cb.apiLister().listOrgRepos(ctx, org)
}

// ListOrgMembers returns the logins of the named organisation's members, from
// /orgs/{org}/members. A caller that is not a member of the organisation, or
// has no token, sees only its public members.
func (cb *CodebergHost) ListOrgMembers(ctx context.Context, org string) ([]string, error) {
	return cb.apiLister().listOrgMembers(ctx, org)
}
