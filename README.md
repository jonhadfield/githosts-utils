# githosts-utils

`githosts-utils` is a Go library for backing up repositories from major hosting providers. It powers [soba](https://github.com/jonhadfield/soba) and can be embedded in your own tools.

## Features

- Minimal dependencies and portable code
- Supports GitHub, GitLab, Bitbucket, Azure DevOps, Gitea, Codeberg, and Sourcehut
- Clones repositories using `git --mirror` and stores timestamped bundle files
- **Encryption support**: Optional age-based encryption for bundles and manifests
- Optional reference comparison to skip cloning when refs have not changed
- Ability to keep a configurable number of previous bundles
- Optional Git LFS archival alongside each bundle
- Pluggable HTTP client and simple logging via the `GITHOSTS_LOG` environment variable

## Installation

```bash
go get github.com/jonhadfield/githosts-utils/v2
```

Requires Go 1.26 or later.

## Quick Start

Create a host for the provider you want to back up and call `Backup()` on it. Each provider has its own input struct with the required options. The example below backs up a set of GitHub repositories:

```go
package main

import (
    "log"
    "os"

    "github.com/jonhadfield/githosts-utils/v2"
)

func main() {
    backupDir := "/path/to/backups"

    host, err := githosts.NewGitHubHost(githosts.NewGitHubHostInput{
        Caller:               "example",
        BackupDir:            backupDir,
        Token:                os.Getenv("GITHUB_TOKEN"),
        BackupLFS:            true,
        EncryptionPassphrase: os.Getenv("BUNDLE_PASSPHRASE"), // Optional encryption
    })
    if err != nil {
        log.Fatal(err)
    }

    results := host.Backup()
    for _, r := range results.BackupResults {
        log.Printf("%s: %s", r.Repo, r.Status)
    }
}
```

`Backup()` returns a `ProviderBackupResult` containing the status of each repository. Bundles are written beneath `<backupDir>/<provider>/<owner>/<repo>/`.

### Diff Remote Method

Each host accepts a `DiffRemoteMethod` value of either `"clone"` or `"refs"`:

- `clone` (default) – always clone and create a new bundle
- `refs` – fetch remote references first and skip cloning when the refs match the latest bundle

### Codeberg

Codeberg runs Forgejo, which serves the same `/api/v1` surface as Gitea, so it has its own host rather than reusing the Gitea one. The difference is ownership: the Gitea host discovers repositories through the instance-admin endpoints (`/admin/users` and `/orgs`), which a Codeberg personal access token cannot reach, and whose `/orgs` listing would return every organisation on the instance rather than yours. The Codeberg host uses the token-scoped endpoints (`/user/repos` and `/user/orgs`) instead.

```go
host, err := githosts.NewCodebergHost(githosts.NewCodebergHostInput{
    BackupDir: backupDir,
    Token:     os.Getenv("CODEBERG_TOKEN"),
    Orgs:      []string{"*"}, // "*" expands to every org the token belongs to
})
```

`APIURL` defaults to `https://codeberg.org/api/v1` and can be pointed at any other Forgejo instance. The `"*"` entry expands to every organisation the token belongs to and is combined with any organisations named alongside it, so organisations the token is not a member of can still be backed up. Two further options control the breadth of the backup:

- `SkipUserRepos` – omit `/user/repos`, backing up only the organisations named in `Orgs`
- `LimitUserOwned` – keep only repositories owned by the authenticated user, excluding those they are merely a collaborator on

`/user/repos` already includes repositories owned by your organisations, so repositories reachable both ways are backed up once.

Codeberg is a donation-funded shared host, so this provider defaults to lower concurrency and a longer delay between repositories than the Gitea one. Override the delay with `CODEBERG_WORKER_DELAY` (milliseconds) and the number of repositories backed up at once with `CODEBERG_MAX_CONCURRENT`.

### Listing Repositories

Every host also implements `Lister`, for tools that need a provider's repositories without backing them up:

```go
host, err := githosts.NewGitHubHost(githosts.NewGitHubHostInput{Token: os.Getenv("GITHUB_TOKEN")})
if err != nil {
    log.Fatal(err)
}

repos, err := host.ListOrgRepos(ctx, "my-org")
```

| Host | `ListOwnRepos(ctx)` | `ListUserRepos(ctx, user)` | `ListOrgRepos(ctx, org)` | `ListOrgMembers(ctx, org)` |
|---|---|---|---|---|
| GitHub | the token owner's repositories, honouring `LimitUserOwned` | repositories the user owns | the organisation's repositories | member logins (`read:org` shows private members) |
| GitLab | projects at `ProjectMinAccessLevel` or above | the user's personal projects | the group's projects, including subgroups (`org` is the full path) | usernames, including inherited members |
| Bitbucket | repositories in the configured or discovered workspaces | the user's personal workspace (`user` is its workspace ID or UUID) | the workspace's repositories | member UUIDs in braces, which `ListUserRepos` accepts |
| Azure DevOps | every repository in the first configured organisation, as `Backup()` processes | not supported | every repository in every project of the organisation | users' principal names (needs the Member Entitlement Management read scope) |
| Gitea | every user's repositories, through the admin endpoints `Backup()` uses (needs a site-admin token) | the user's repositories | the organisation's repositories | member logins (non-members see public members only) |
| Codeberg | the token's repositories (`/user/repos`), honouring `LimitUserOwned` | the user's repositories | the organisation's repositories | member logins (non-members see public members only) |
| Sourcehut | the token owner's public repositories, as `Backup()` processes | the user's repositories the token can see (`~` optional) | not supported | not supported |

Each `Repository` reports `IsFork`, `IsArchived`, `IsEmpty`, `IsPrivate` and `SizeKB`, and carries a `CloneURL` without credentials plus the `Auth` it needs, so a caller can clone with its own git implementation without credentials appearing in URLs. A method a provider cannot support returns an error wrapping `ErrNotSupported`. Where a provider has no such field, the value is false or 0:

- `SizeKB` is 0 for GitLab and Sourcehut, which do not report a size.
- `IsArchived` is always false for Bitbucket, Azure DevOps and Sourcehut, which have no archived state. A disabled Azure DevOps repository cannot be cloned at all, so it is not reported as archived.
- `IsFork` is always false for Sourcehut.
- GitLab internal projects, Gitea and Codeberg internal repositories, and Sourcehut unlisted repositories count as private.

The GitLab, Gitea and Codeberg listers also work without a token, listing only public repositories and returning no `Auth`; their `ListOwnRepos` still needs one. Sourcehut's lister needs a token with git.sr.ht's read-only objects scope, to tell whether a repository is empty.

### Retaining Bundles

Set `BackupsToRetain` to keep only the most recent _n_ bundle files per repository. Older bundles are automatically deleted after a successful backup.

## Encryption

The library supports optional age-based encryption for all backup bundles and their associated manifests. When encryption is enabled:

- Bundle files are encrypted and saved with a `.age` extension (e.g., `repo.20240101000000.bundle.age`)
- Manifest files containing bundle metadata and git refs are also encrypted
- The system can seamlessly work with both encrypted and unencrypted bundles in the same repository

### Enabling Encryption

You can enable encryption in two ways:

1. **Via environment variable**: Set `BUNDLE_PASSPHRASE` to your encryption passphrase
2. **Via host configuration**: Pass the `EncryptionPassphrase` parameter when creating a host

```go
host, err := githosts.NewGitHubHost(githosts.NewGitHubHostInput{
    BackupDir:            backupDir,
    Token:                token,
    EncryptionPassphrase: "your-secure-passphrase",
})
```

### Encryption Behavior

- When using the `refs` diff method, the system can compare encrypted bundles without decrypting them by using manifest files
- If you switch from encrypted to unencrypted backups (or vice versa), the system handles this gracefully
- Wrong passphrases are detected and reported with appropriate error messages
- Corrupted encrypted files are handled safely with fallback mechanisms

## Environment Variables

The library reads the following variables where relevant:

- `GITHOSTS_LOG` – set to `debug` to emit verbose logs
- `GIT_BACKUP_DIR` – used by the tests to determine the backup location
- `BUNDLE_PASSPHRASE` – optional passphrase for encrypting backup bundles

Provider-specific tests require credentials through environment variables such as `GITHUB_TOKEN`, `GITLAB_TOKEN`, `BITBUCKET_EMAIL` and `BITBUCKET_API_TOKEN` (or `BITBUCKET_KEY` and `BITBUCKET_SECRET` for OAuth2), `AZURE_DEVOPS_USERNAME`, `AZURE_DEVOPS_PAT`, and `AZURE_DEVOPS_ORGS`, `GITEA_TOKEN` and `GITEA_APIURL`, `CODEBERG_TOKEN`, and `SOURCEHUT_PAT`.

## Running Tests

```bash
export GIT_BACKUP_DIR=$(mktemp -d)
go test ./...
```

Integration tests are skipped unless the corresponding provider credentials are present.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
