package githosts

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"testing"
)

// Common test constants
const (
	// Environment Variables for tests
	envGiteaToken        = "GITEA_TOKEN"
	envGitLabToken       = "GITLAB_TOKEN"
	envBitbucketAPIToken = "BITBUCKET_API_TOKEN"
	envBitbucketEmail    = "BITBUCKET_EMAIL"
	envBitbucketSecret   = "BITBUCKET_SECRET"
	envBitbucketKey      = "BITBUCKET_KEY"
	envBitbucketUser     = "BITBUCKET_USER"
	envSourcehutPAT      = "SOURCEHUT_PAT"

	// Worker delay environment variables
	envGitHubWorkerDelay      = "GITHUB_WORKER_DELAY"
	envGitLabWorkerDelay      = "GITLAB_WORKER_DELAY"
	envBitbucketWorkerDelay   = "BITBUCKET_WORKER_DELAY"
	envGiteaWorkerDelay       = "GITEA_WORKER_DELAY"
	envAzureDevOpsWorkerDelay = "AZURE_DEVOPS_WORKER_DELAY"
	envSourcehutWorkerDelay   = "SOURCEHUT_WORKER_DELAY"

	// Skip test messages
	msgSkipGiteaTokenMissing     = "Skipping Gitea test as GITEA_TOKEN is missing"
	msgSkipGitLabTokenMissing    = "Skipping GitLab test as GITLAB_TOKEN is missing"
	msgSkipBitbucketEmailMissing = "Skipping Bitbucket test as BITBUCKET_EMAIL is missing"
	msgSkipSourcehutTokenMissing = "Skipping sourcehut test as SOURCEHUT_PAT is missing"
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
	envGitHubWorkerDelay, envGitLabWorkerDelay, envBitbucketWorkerDelay,
	envGiteaWorkerDelay, envAzureDevOpsWorkerDelay, envSourcehutWorkerDelay, envGiteaToken,
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
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read dir %s: %w", path, err)
	}

	return entries, nil
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
