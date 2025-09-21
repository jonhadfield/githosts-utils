# githosts-utils

`githosts-utils` is a Go library for backing up repositories from major hosting providers. It powers [soba](https://github.com/jonhadfield/soba) and can be embedded in your own tools.

## Features

- Minimal dependencies and portable code
- Supports GitHub, GitLab, Bitbucket, Azure DevOps, Gitea, and Sourcehut
- Clones repositories using `git --mirror` and stores timestamped bundle files
- **Encryption support**: Optional age-based encryption for bundles and manifests
- Optional reference comparison to skip cloning when refs have not changed
- Ability to keep a configurable number of previous bundles
- Optional Git LFS archival alongside each bundle
- Pluggable HTTP client and simple logging via the `GITHOSTS_LOG` environment variable

## Installation

```bash
go get github.com/jonhadfield/githosts-utils
```

Requires Go 1.22 or later.

## Quick Start

Create a host for the provider you want to back up and call `Backup()` on it. Each provider has its own input struct with the required options. The example below backs up a set of GitHub repositories:

```go
package main

import (
    "log"
    "os"

    "github.com/jonhadfield/githosts-utils"
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

Provider-specific tests require credentials through environment variables such as `GITHUB_TOKEN`, `GITLAB_TOKEN`, `BITBUCKET_KEY`, `BITBUCKET_SECRET`, `AZURE_DEVOPS_USERNAME`, `AZURE_DEVOPS_PAT`, `GITEA_TOKEN`, and `SOURCEHUT_TOKEN`.

## Running Tests

```bash
export GIT_BACKUP_DIR=$(mktemp -d)
go test ./...
```

Integration tests are skipped unless the corresponding provider credentials are present.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
