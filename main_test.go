package githosts

import (
	"log"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	preflight()

	code := m.Run()
	os.Exit(code)
}

var sobaEnvVarKeys = []string{
	envVarGitBackupDir, gitlabEnvVarToken, gitlabEnvVarAPIUrl,
	bitbucketEnvVarEmail, bitbucketEnvVarAPIToken, bitbucketEnvVarSecret, bitbucketEnvVarKey,
	envSourcehutToken, envSourcehutAPIURL,
}

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
		if !slices.Contains(exceptionList, sobaVar) {
			_ = os.Unsetenv(sobaVar)
		}
	}
}

func dirContents(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func resetBackups() {
	backupDir := os.Getenv(envVarGitBackupDir)
	if backupDir == "" {
		log.Fatalf("backup dir not set with env var %s", envVarGitBackupDir)
	}

	_ = os.RemoveAll(backupDir)

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		log.Fatal(err)
	}
}
