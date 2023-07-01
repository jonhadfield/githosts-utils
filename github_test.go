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
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

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

	expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, "github.com", "go-soba", "repo2")
	require.NoDirExists(t, expectedPathThree)

	restoreEnvironmentVariables(envBackup)
}

func TestDescribeGithubOrgRepos(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, "GITHUB_TOKEN"})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITHUB_TOKEN"),
	})
	require.NoError(t, err)

	repos := gh.describeGithubOrgRepos("Nudelmesse")
	require.Len(t, repos, 4)

	restoreEnvironmentVariables(envBackup)
}

func TestSinglePublicGitHubOrgRepoBackups(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

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

	expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, "github.com", "Nudelmesse", "public1")
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

	restoreEnvironmentVariables(envBackup)
}

func TestPublicGitHubOrgRepoBackups(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	// need to set output to buffer in order to test output
	buf = bytes.Buffer{}
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

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

	expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
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

	restoreEnvironmentVariables(envBackup)
}
