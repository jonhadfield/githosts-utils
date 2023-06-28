package githosts

import (
	"log"
	"os"
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

var sobaEnvVarKeys = []string{
	envVarGitBackupDir, githubEnvVarBackups, gitlabEnvVarToken, gitlabEnvVarBackups, gitlabEnvVarAPIUrl,
	bitbucketEnvVarUser, bitbucketEnvVarKey, bitbucketEnvVarSecret, bitbucketEnvVarBackups,
}

var numUserDefinedProviders int64

func preflight() {
	// create backup dir if defined but missing
	bud := os.Getenv(envVarGitBackupDir)
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

func dirContents(path string) (contents []os.DirEntry, err error) {
	return os.ReadDir(path)
}

func resetBackups() {
	_ = os.RemoveAll(os.Getenv(envVarGitBackupDir))
	if err := os.MkdirAll(os.Getenv(envVarGitBackupDir), 0o755); err != nil {
		log.Fatal(err)
	}
}
