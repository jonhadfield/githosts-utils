package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicGitLabRepositoryBackupCloneMethod(t *testing.T) {
	resetBackups()
	resetGlobals()
	envBackup := backupEnvironmentVariables()
	unsetEnvVars([]string{envVarGitBackupDir, gitlabEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewGitlabHost(NewGitlabHostInput{
		APIURL:           gitlabAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
	})
	require.NoError(t, err)

	gl.Backup()
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

func TestPublicGitLabRepositoryBackupRefsMethod(t *testing.T) {
	resetBackups()
	if os.Getenv(gitlabEnvVarToken) == "" {
		t.Skip("Skipping GitLab test as GITLAB_TOKEN is missing")
	}
	resetGlobals()
	envBackup := backupEnvironmentVariables()
	unsetEnvVars([]string{envVarGitBackupDir, gitlabEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewGitlabHost(NewGitlabHostInput{
		APIURL:           gitlabAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
	})
	require.NoError(t, err)

	gl.Backup()
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
