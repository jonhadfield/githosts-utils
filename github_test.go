package githosts

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicGitHubRepositoryBackup(t *testing.T) {
	resetBackups()
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}
	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITHUB_TOKEN"})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	ghHost := githubHost{
		Provider: "GitHub",
		APIURL:   githubAPIURL,
	}

	ghHost.Backup(backupDIR)

	expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile("^repo0\\.\\d{14}\\.bundle"), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile("^repo1\\.\\d{14}\\.bundle"), dirTwoEntries[0].Name())

	restoreEnvironmentVariables(envBackup)
}
