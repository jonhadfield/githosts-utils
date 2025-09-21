package githosts

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

var buf bytes.Buffer

const (
	envGithubToken            = "GITHUB_TOKEN" //nolint:gosec
	msgSkipGitHubTokenMissing = "Skipping GitHub test as " + envGithubToken + " is missing"
)

func init() {
	if logger == nil {
		logger = log.New(os.Stdout, logEntryPrefix, log.Lshortfile|log.LstdFlags)
	}

	defer func() {
		log.SetOutput(os.Stderr)
	}()
}

func TestPublicGitHubRepositoryBackup(t *testing.T) {
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv(envGithubToken),
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
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv(envGithubToken),
	})
	require.NoError(t, err)

	repos, err := gh.describeGithubOrgRepos("Nudelmesse")
	require.NoError(t, err)
	require.Len(t, repos, 4)
}

func TestSinglePublicGitHubOrgRepoBackups(t *testing.T) {
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv(envGithubToken),
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

	reRepo0 := regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo0'`)
	reRepo1 := regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo1'`)
	reRepo2 := regexp.MustCompile(`skipping clone of github\.com repo 'Nudelmesse/public1'`)

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
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	buf = bytes.Buffer{}
	logger.SetOutput(&buf)

	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            os.Getenv(envGithubToken),
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

	reRepo0 := regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo0'`)
	reRepo1 := regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo1'`)

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
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv(envGithubToken),
		Orgs:             []string{"*"},
	})
	require.NoError(t, err)

	descReposResp, err := gh.describeRepos()
	require.NoError(t, err)

	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo0",
		pathWithNamespace: "go-soba/repo0",
		httpsUrl:          "https://github.com/go-soba/repo0",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		pathWithNamespace: "go-soba/repo1",
		name:              "repo1",
		httpsUrl:          "https://github.com/go-soba/repo1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo2",
		pathWithNamespace: "go-soba/repo2",
		httpsUrl:          "https://github.com/go-soba/repo2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private1",
		pathWithNamespace: "Nudelmesse/private1",
		httpsUrl:          "https://github.com/Nudelmesse/private1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private2",
		pathWithNamespace: "Nudelmesse/private2",
		httpsUrl:          "https://github.com/Nudelmesse/private2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public1",
		pathWithNamespace: "Nudelmesse/public1",
		httpsUrl:          "https://github.com/Nudelmesse/public1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public2",
		pathWithNamespace: "Nudelmesse/public2",
		httpsUrl:          "https://github.com/Nudelmesse/public2",
	}))
}

func TestDescribeGithubReposWithWildcardAndLimitUserOwned(t *testing.T) { //nolint:dupl // test pattern similarity is acceptable
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		LimitUserOwned:   true,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv(envGithubToken),
		// Orgs:             []string{"*"},
	})
	require.NoError(t, err)

	descReposResp, err := gh.describeRepos()
	require.NoError(t, err)

	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo0",
		pathWithNamespace: "go-soba/repo0",
		httpsUrl:          "https://github.com/go-soba/repo0",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		pathWithNamespace: "go-soba/repo1",
		name:              "repo1",
		httpsUrl:          "https://github.com/go-soba/repo1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo2",
		pathWithNamespace: "go-soba/repo2",
		httpsUrl:          "https://github.com/go-soba/repo2",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private1",
		pathWithNamespace: "Nudelmesse/private1",
		httpsUrl:          "https://github.com/Nudelmesse/private1",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private2",
		pathWithNamespace: "Nudelmesse/private2",
		httpsUrl:          "https://github.com/Nudelmesse/private2",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public1",
		pathWithNamespace: "Nudelmesse/public1",
		httpsUrl:          "https://github.com/Nudelmesse/public1",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public2",
		pathWithNamespace: "Nudelmesse/public2",
		httpsUrl:          "https://github.com/Nudelmesse/public2",
	}))
}

func TestDescribeGithubReposWithWildcardAndNoLimitUserOwned(t *testing.T) { //nolint:dupl // test pattern similarity is acceptable
	// will return all user's repos and those they're affiliated with (collaborators on)
	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		LimitUserOwned:   false,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv(envGithubToken),
		// Orgs:             []string{"*"},
	})
	require.NoError(t, err)

	descReposResp, err := gh.describeRepos()
	require.NoError(t, err)

	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo0",
		pathWithNamespace: "go-soba/repo0",
		httpsUrl:          "https://github.com/go-soba/repo0",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		pathWithNamespace: "go-soba/repo1",
		name:              "repo1",
		httpsUrl:          "https://github.com/go-soba/repo1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "repo2",
		pathWithNamespace: "go-soba/repo2",
		httpsUrl:          "https://github.com/go-soba/repo2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private1",
		pathWithNamespace: "Nudelmesse/private1",
		httpsUrl:          "https://github.com/Nudelmesse/private1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private2",
		pathWithNamespace: "Nudelmesse/private2",
		httpsUrl:          "https://github.com/Nudelmesse/private2",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public1",
		pathWithNamespace: "Nudelmesse/public1",
		httpsUrl:          "https://github.com/Nudelmesse/public1",
	}))
	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public2",
		pathWithNamespace: "Nudelmesse/public2",
		httpsUrl:          "https://github.com/Nudelmesse/public2",
	}))
}

func TestDescribeGithubReposWithSkipUserRepos(t *testing.T) {
	t.Parallel()

	if os.Getenv(envGithubToken) == "" {
		t.Skip(msgSkipGitHubTokenMissing)
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envGithubToken})

	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		SkipUserRepos:    true,
		Token:            os.Getenv(envGithubToken),
		Orgs:             []string{"*"},
	})
	require.NoError(t, err)

	descReposResp, err := gh.describeRepos()
	require.NoError(t, err)

	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private1",
		pathWithNamespace: "Nudelmesse/private1",
		httpsUrl:          "https://github.com/Nudelmesse/private1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "private2",
		pathWithNamespace: "Nudelmesse/private2",
		httpsUrl:          "https://github.com/Nudelmesse/private2",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public1",
		pathWithNamespace: "Nudelmesse/public1",
		httpsUrl:          "https://github.com/Nudelmesse/public1",
	}))
	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             descReposResp.Repos,
		name:              "public2",
		pathWithNamespace: "Nudelmesse/public2",
		httpsUrl:          "https://github.com/Nudelmesse/public2",
	}))
}

func TestRemove(t *testing.T) {
	t.Parallel()

	s := []string{"a", "b", "c", "d", "e"}
	r := "c"
	expected := []string{"a", "b", "d", "e"}

	require.Equal(t, expected, remove(s, r))
}

func TestDiffRemoteMethodReturnsRefsMethodWhenInputIsRefs(t *testing.T) {
	t.Parallel()

	gh := GitHubHost{DiffRemoteMethod: "refs"}

	result := gh.diffRemoteMethod()

	assert.Equal(t, "refs", result)
}

func TestDiffRemoteMethodReturnsCloneMethodWhenInputIsClone(t *testing.T) {
	t.Parallel()

	gh := GitHubHost{DiffRemoteMethod: "clone"}

	result := gh.diffRemoteMethod()

	assert.Equal(t, "clone", result)
}

func TestDiffRemoteMethodReturnsCloneMethodWhenInputIsUnexpected(t *testing.T) {
	t.Parallel()

	gh := GitHubHost{DiffRemoteMethod: "unexpected"}

	result := gh.diffRemoteMethod()

	assert.Equal(t, "clone", result)
}

func TestDiffRemoteMethodReturnsCloneMethodWhenInputIsEmpty(t *testing.T) {
	t.Parallel()

	gh := GitHubHost{DiffRemoteMethod: ""}

	result := gh.diffRemoteMethod()

	assert.Equal(t, "clone", result)
}
