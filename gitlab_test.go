package githosts

import (
	"github.com/stretchr/testify/require"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	preflight()
	code := m.Run()
	os.Exit(code)
}

func stringInStrings(single string, group []string) bool {
	for _, item := range group {
		if single == item {
			return true
		}
	}

	return false
}

func stripTrailingLineBreak(input string) string {
	if strings.HasSuffix(input, "\n") {
		return input[:len(input)-2]
	}

	return input
}

var sobaEnvVarKeys = []string{
	"GIT_BACKUP_DIR", "GITHUB_TOKEN", "GITHUB_BACKUPS", "GITLAB_TOKEN", "GITLAB_BACKUPS", "GITLAB_APIURL",
	"BITBUCKET_USER", "BITBUCKET_KEY", "BITBUCKET_SECRET", "BITBUCKET_BACKUPS",
}

var numUserDefinedProviders int64

func preflight() {
	// create backup dir if defined but missing
	bud := os.Getenv("GIT_BACKUP_DIR")
	if bud == "" {
		bud = os.TempDir()
	}

	_, err := os.Stat(bud)

	if os.IsNotExist(err) {
		errDir := os.MkdirAll(bud, 0o755)
		if errDir != nil {
			log.Fatal(err)
		}
	}
}

func resetGlobals() {
	// reset global var
	numUserDefinedProviders = 0
}

func backupEnvironmentVariables() map[string]string {
	m := make(map[string]string)
	for _, e := range os.Environ() {
		if i := strings.Index(e, "="); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}

	return m
}

func restoreEnvironmentVariables(input map[string]string) {
	for key, val := range input {
		_ = os.Setenv(key, val)
	}
}

func unsetEnvVars(exceptionList []string) {
	for _, sobaVar := range sobaEnvVarKeys {
		if !stringInStrings(sobaVar, exceptionList) {
			_ = os.Unsetenv(sobaVar)
		}
	}
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func dirContents(path string) (contents []os.DirEntry, err error) {
	return os.ReadDir(path)
}

func resetBackups() {
	_ = os.RemoveAll(os.Getenv("GIT_BACKUP_DIR"))
	if err := os.MkdirAll(os.Getenv("GIT_BACKUP_DIR"), 0o755); err != nil {
		log.Fatal(err)
	}
}

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
