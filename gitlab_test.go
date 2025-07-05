package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	gitlabEnvVarToken                 = "GITLAB_TOKEN"
	gitlabEnvVarProjectMinAccessLevel = "GITLAB_PROJECT_MIN_ACCESS_LEVEL"
	gitlabEnvVarAPIUrl                = "GITLAB_APIURL"
)

func TestPublicGitLabRepositoryBackupCloneMethod(t *testing.T) {
	resetBackups()
	if os.Getenv(gitlabEnvVarToken) == "" {
		t.Skip(msgSkipGitLabTokenMissing)
	}

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, gitlabEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewGitLabHost(NewGitLabHostInput{
		APIURL:           gitlabAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv(gitlabEnvVarToken),
	})
	require.NoError(t, err)

	gl.Backup()
	expectedSubProjectOnePath := filepath.Join(backupDIR, gitLabDomain, "soba-test", "soba-sub", "soba-sub-project-one")
	expectedSubProjectTwoPath := filepath.Join(backupDIR, gitLabDomain, "soba-test", "soba-sub", "soba-sub-project-two")
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
}

func TestPublicGitLabRepositoryBackupRefsMethod(t *testing.T) {
	resetBackups()
	if os.Getenv(gitlabEnvVarToken) == "" {
		t.Skip(msgSkipGitLabTokenMissing)
	}

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, gitlabEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewGitLabHost(NewGitLabHostInput{
		APIURL:           gitlabAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv(gitlabEnvVarToken),
		LogLevel:         1,
	})
	require.NoError(t, err)

	gl.Backup()
	expectedSubProjectOnePath := filepath.Join(backupDIR, gitLabDomain, "soba-test", "soba-sub", "soba-sub-project-one")
	expectedSubProjectTwoPath := filepath.Join(backupDIR, gitLabDomain, "soba-test", "soba-sub", "soba-sub-project-two")
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
}
