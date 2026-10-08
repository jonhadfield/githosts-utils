package githosts

import (
	"context"
	"encoding/json"
	"strings"

	"gitlab.com/tozd/go/errors"
)

var _ Lister = (*SourcehutHost)(nil)

// sourcehutListerExtraFields adds what the lister reports beyond what Backup
// needs. git.sr.ht resolves Repository.HEAD to null when it does not resolve
// to a commit, which is how an empty repository is detected. HEAD requires
// the token to grant git.sr.ht's OBJECTS scope (read-only), as well as the
// PROFILE and REPOSITORIES scopes the listing itself needs.
const sourcehutListerExtraFields = ` HEAD { name }`

// sourcehutUserReposQuery lists the repositories owned by $username, a page at
// a time. The username is passed as a variable, never interpolated.
var sourcehutUserReposQuery = `query ($username: String!, $cursor: Cursor) { user(username: $username) { repositories(` +
	sourcehutRepoPageArgs + `) { results { ` + sourcehutRepoResultFields + sourcehutListerExtraFields + ` } cursor } } }`

type sourcehutUserRepositoriesResponse struct {
	Data struct {
		User *struct {
			Repositories sourcehutRepositoryCursor `json:"repositories"`
		} `json:"user"`
	} `json:"data"`
	Errors sourcehutGraphQLErrors `json:"errors"`
}

// cloneAuth returns the credentials Backup clones with: the token as the
// username and an empty password.
//
// SourceHut does not document HTTPS authentication for git, and Backup only
// processes public repositories because private ones could not be cloned over
// HTTPS with a personal access token. These credentials are therefore what
// Backup uses, not a guarantee that a non-public repository can be cloned
// with them; SSH (SSHURL) with a key registered on meta.sr.ht is the
// documented way to reach those.
func (sh *SourcehutHost) cloneAuth() BasicAuth {
	return BasicAuth{User: stripTrailing(sh.PersonalAccessToken, "\n")}
}

// exportSourcehutRepos converts repositories as returned by the lister's
// queries, which select HEAD, to the exported form.
func (sh *SourcehutHost) exportSourcehutRepos(in []sourcehutRepository) []Repository {
	repos := make([]repository, 0, len(in))

	for _, r := range in {
		repo := sourcehutRepoToRepository(r)
		repo.IsEmpty = r.HEAD == nil
		repos = append(repos, repo)
	}

	return exportRepositories(repos, sh.cloneAuth())
}

// ListOwnRepos lists the authenticated user's public repositories: the set
// Backup processes. git.sr.ht's repositories query returns only repositories
// the token's user owns. Unlisted and private repositories are left out, as
// Backup skips them; use ListUserRepos with the token's own username to see
// them.
//
// SourceHut has no forks, archiving or repository sizes in its API, so
// IsFork and IsArchived are always false and SizeKB is always 0. Domain is
// "sourcehut", the name Backup files the repositories under, as the API does
// not report a host.
func (sh *SourcehutHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	if strings.TrimSpace(sh.PersonalAccessToken) == "" {
		return nil, errors.New("SourceHut personal access token not provided")
	}

	results, err := sh.pageSourcehutRepos(ctx, sourcehutOwnReposQuery(sourcehutListerExtraFields), nil, decodeSourcehutOwnRepos)
	if err != nil {
		return nil, err
	}

	public := make([]sourcehutRepository, 0, len(results))

	for _, r := range results {
		if isSourcehutPublic(r) {
			public = append(public, r)
		}
	}

	return sh.exportSourcehutRepos(public), nil
}

// ListUserRepos lists the repositories owned by the named user that the token
// can see: their public repositories, plus any unlisted or private ones the
// token's user owns or has been granted access to. The user may be given with
// or without SourceHut's "~" prefix.
//
// IsPrivate is true for both UNLISTED and PRIVATE visibility. An unlisted
// repository can be read by anyone with its URL, but it is not public in the
// sense Backup uses, and Backup skips it.
func (sh *SourcehutHost) ListUserRepos(ctx context.Context, user string) ([]Repository, error) {
	username := strings.TrimPrefix(strings.TrimSpace(user), sourcehutTildePrefix)
	if username == "" {
		return nil, errors.Errorf("invalid SourceHut username %q", user)
	}

	results, err := sh.pageSourcehutRepos(ctx, sourcehutUserReposQuery, map[string]any{"username": username},
		func(body []byte) (sourcehutRepositoryCursor, errors.E) {
			var respObj sourcehutUserRepositoriesResponse
			if uErr := json.Unmarshal(body, &respObj); uErr != nil {
				return sourcehutRepositoryCursor{}, errors.Wrap(uErr, "failed to unmarshal response")
			}

			if err := respObj.Errors.asError(); err != nil {
				return sourcehutRepositoryCursor{}, err
			}

			if respObj.Data.User == nil {
				return sourcehutRepositoryCursor{}, errors.Errorf("SourceHut user %s not found", username)
			}

			return respObj.Data.User.Repositories, nil
		})
	if err != nil {
		return nil, err
	}

	return sh.exportSourcehutRepos(results), nil
}

// ListOrgRepos is not supported: SourceHut has users but no organisations or
// groups that own repositories.
func (sh *SourcehutHost) ListOrgRepos(context.Context, string) ([]Repository, error) {
	return nil, errors.WithMessage(ErrNotSupported, "SourceHut has no organisations")
}

// ListOrgMembers is not supported: SourceHut has no organisations.
func (sh *SourcehutHost) ListOrgMembers(context.Context, string) ([]string, error) {
	return nil, errors.WithMessage(ErrNotSupported, "SourceHut has no organisations")
}
