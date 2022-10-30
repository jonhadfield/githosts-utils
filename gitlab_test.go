package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicGitLabRepositoryBackup(t *testing.T) {
	resetBackups()
	if os.Getenv("GITLAB_TOKEN") == "" {
		t.Skip("Skipping GitLab test as GITLAB_TOKEN is missing")
	}
	resetGlobals()
	envBackup := backupEnvironmentVariables()
	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITLAB_TOKEN"})
	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	require.NoError(t, Backup("gitlab", backupDIR, os.Getenv("GITLAB_APIURL")))
	expectedSubProjectOnePath := filepath.Join(backupDIR, "gitlab.com", "soba-test", "soba-sub", "soba-sub-project-one")
	expectedSubProjectTwoPath := filepath.Join(backupDIR, "gitlab.com", "soba-test", "soba-sub", "soba-sub-project-two")
	require.DirExists(t, expectedSubProjectOnePath)
	require.DirExists(t, expectedSubProjectTwoPath)
	projectOneEntries, err := dirContents(expectedSubProjectOnePath)
	require.NoError(t, err)
	require.Len(t, projectOneEntries, 1)
	require.Contains(t, projectOneEntries[0].Name(), "soba-sub-project-one.")
	projectTwoEntries, err := dirContents(expectedSubProjectTwoPath)
	require.NoError(t, err)
	require.Len(t, projectTwoEntries, 1)
	require.Contains(t, projectTwoEntries[0].Name(), "soba-sub-project-two.")

	restoreEnvironmentVariables(envBackup)
}
