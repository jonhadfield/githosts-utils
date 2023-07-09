package githosts

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var buf bytes.Buffer

const msgSkipGitHubTokenMissing = "Skipping GitHub test as GITHUB_TOKEN is missing"

func init() {
	if logger == nil {
		logger = log.New(os.Stdout, logEntryPrefix, log.Lshortfile|log.LstdFlags)
	}
	defer func() {
		log.SetOutput(os.Stderr)
	}()
}

func TestPublicGitHubRepositoryBackup(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv("GITHUB_TOKEN"),
		SkipUserRepos:    false,
	})
	require.NoError(t, err)

	ghHost.Backup()

	expectedPathOne := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo2")
	require.NoDirExists(t, expectedPathThree)
}

func TestDescribeGithubOrgRepos(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITHUB_TOKEN"),
	})
	require.NoError(t, err)

	repos := gh.describeGithubOrgRepos("Nudelmesse")
	require.Len(t, repos, 4)
}

func TestSinglePublicGitHubOrgRepoBackups(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv("GITHUB_TOKEN"),
		Orgs:             []string{"Nudelmesse"},
	})
	require.NoError(t, err)

	ghHost.Backup()

	expectedPathOne := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, gitHubDomain, "Nudelmesse", "public1")
	require.DirExists(t, expectedPathThree)
	dirThreeEntries, err := dirContents(expectedPathThree)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^public1\.\d{14}\.bundle$`), dirThreeEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	ghHost.Backup()
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo0'`)
	var reRepo1 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo1'`)
	var reRepo2 = regexp.MustCompile(`skipping clone of github\.com repo 'Nudelmesse/public1'`)
	var matches int

	logger.SetOutput(os.Stdout)

	for x := range logLines {
		if reRepo0.MatchString(logLines[x]) {
			matches++
		}
		if reRepo1.MatchString(logLines[x]) {
			matches++
		}
		if reRepo2.MatchString(logLines[x]) {
			matches++
		}
	}

	require.Equal(t, 3, matches)
}

func TestPublicGitHubOrgRepoBackups(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	buf = bytes.Buffer{}
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv("GITHUB_TOKEN"),
	})
	require.NoError(t, err)

	ghHost.Backup()

	expectedPathOne := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, gitHubDomain, "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	ghHost.Backup()
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo0'`)
	var reRepo1 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo1'`)
	var matches int

	logger.SetOutput(os.Stdout)

	for x := range logLines {
		if strings.TrimSpace(logLines[x]) == "" {
			continue
		}

		if reRepo0.MatchString(logLines[x]) {
			matches++
		}

		if reRepo1.MatchString(logLines[x]) {
			matches++
		}
	}

	require.Equal(t, 2, matches)
}

func TestDescribeGithubReposWithWildcard(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITHUB_TOKEN"),
		Orgs:             []string{"*"},
	})
	require.NoError(t, err)

	// repos := gh.describeRepos()
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "repo0",
		pathWithNamespace: "go-soba/repo0",
		httpsUrl:          "https://github.com/go-soba/repo0",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		pathWithNamespace: "go-soba/repo1",
		name:              "repo1",
		httpsUrl:          "https://github.com/go-soba/repo1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "repo2",
		pathWithNamespace: "go-soba/repo2",
		httpsUrl:          "https://github.com/go-soba/repo2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "private1",
		pathWithNamespace: "Nudelmesse/private1",
		httpsUrl:          "https://github.com/Nudelmesse/private1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "private2",
		pathWithNamespace: "Nudelmesse/private2",
		httpsUrl:          "https://github.com/Nudelmesse/private2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "public1",
		pathWithNamespace: "Nudelmesse/public1",
		httpsUrl:          "https://github.com/Nudelmesse/public1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             gh.describeRepos().Repos,
		name:              "public2",
		pathWithNamespace: "Nudelmesse/public2",
		httpsUrl:          "https://github.com/Nudelmesse/public2",
	}))
}

func TestRemove(t *testing.T) {
	s := []string{"a", "b", "c", "d", "e"}
	r := "c"
	expected := []string{"a", "b", "d", "e"}

	require.Equal(t, expected, remove(s, r))
}
