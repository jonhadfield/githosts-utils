# githosts-utils

`githosts-utils` is a Go library for backing up repositories from major hosting providers. It powers [soba](https://github.com/jonhadfield/soba) and can be embedded in your own tools.

## Features

- Minimal dependencies and portable code
- Supports GitHub, GitLab, Bitbucket, Azure DevOps, and Gitea
- Clones repositories using `git --mirror` and stores timestamped bundle files
- Optional reference comparison to skip cloning when refs have not changed
- Ability to keep a configurable number of previous bundles
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
        Caller:    "example",
        BackupDir: backupDir,
        Token:     os.Getenv("GITHUB_TOKEN"),
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

## Environment Variables

The library reads the following variables where relevant:

- `GITHOSTS_LOG` – set to `debug` to emit verbose logs
- `GIT_BACKUP_DIR` – used by the tests to determine the backup location

Provider-specific tests require credentials through environment variables such as `GITHUB_TOKEN`, `GITLAB_TOKEN`, `BITBUCKET_KEY`, `BITBUCKET_SECRET`, `AZURE_DEVOPS_USERNAME`, `AZURE_DEVOPS_PAT`, and `GITEA_TOKEN`.

## Running Tests

```bash
export GIT_BACKUP_DIR=$(mktemp -d)
go test ./...
```

Integration tests are skipped unless the corresponding provider credentials are present.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
