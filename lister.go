package githosts

import (
	"context"
	"regexp"

	"gitlab.com/tozd/go/errors"
)

// ErrNotSupported is returned by a Lister method the provider has no
// equivalent for, such as listing the members of an organisation on a host
// without organisations. Test for it with errors.Is.
var ErrNotSupported = errors.Base("operation not supported by provider")

// Repository describes a repository as reported by a provider's API, with
// what a caller needs to clone it using its own git implementation.
type Repository struct {
	// Owner is the user, organisation or group namespace the repository
	// belongs to.
	Owner string
	Name  string
	// PathWithNamespace is the provider's full path, e.g. "owner/name", or
	// "group/subgroup/name" on GitLab.
	PathWithNamespace string
	// Domain is the host the repository is served from, e.g. "github.com".
	Domain string
	// CloneURL is the HTTPS clone URL. It never carries credentials: apply
	// Auth separately so that credentials cannot leak through the URL into
	// logs or error messages.
	CloneURL string
	SSHURL   string
	// Auth holds the HTTP basic auth credentials CloneURL requires, in the
	// form this provider expects.
	Auth       BasicAuth
	IsFork     bool
	IsArchived bool
	// IsEmpty reports that the repository has no commits. It is only as good
	// as the provider's API: a provider that cannot report it leaves it
	// false.
	IsEmpty bool
}

// Lister lists repositories and organisation members on a provider. A method
// the provider cannot support returns an error wrapping ErrNotSupported.
type Lister interface {
	// ListOwnRepos lists the repositories visible to the authenticated user,
	// as selected by the host's configuration; it is the same set Backup
	// would process before any organisations are added.
	ListOwnRepos(ctx context.Context) ([]Repository, error)
	// ListUserRepos lists the repositories owned by the named user.
	ListUserRepos(ctx context.Context, user string) ([]Repository, error)
	// ListOrgRepos lists the repositories belonging to the named
	// organisation, or group on GitLab.
	ListOrgRepos(ctx context.Context, org string) ([]Repository, error)
	// ListOrgMembers returns the usernames of the named organisation's
	// members.
	ListOrgMembers(ctx context.Context, org string) ([]string, error)
}

var (
	_ Lister = (*GitHubHost)(nil)
	_ Lister = (*GitLabHost)(nil)
)

// exportRepositories converts internal repositories to the exported form,
// applying the same credentials to each.
func exportRepositories(in []repository, auth BasicAuth) []Repository {
	out := make([]Repository, 0, len(in))

	for _, r := range in {
		out = append(out, Repository{
			Owner:             r.Owner,
			Name:              r.Name,
			PathWithNamespace: r.PathWithNameSpace,
			Domain:            r.Domain,
			CloneURL:          r.HTTPSUrl,
			SSHURL:            r.SSHUrl,
			Auth:              auth,
			IsFork:            r.IsFork,
			IsArchived:        r.IsArchived,
			IsEmpty:           r.IsEmpty,
		})
	}

	return out
}

// githubLoginPattern matches a GitHub user or organisation login. Logins are
// interpolated into GraphQL queries, so anything else is rejected rather than
// escaped.
var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

func validGitHubLogin(login string) errors.E {
	if !githubLoginPattern.MatchString(login) {
		return errors.Errorf("invalid GitHub login %q", login)
	}

	return nil
}
