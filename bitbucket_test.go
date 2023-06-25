package githosts

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPublicBitbucketRepositoryRefsCompare(t *testing.T) {
	if os.Getenv(bitbucketEnvVarKey) == "" {
		t.Skip("Skipping Bitbucket test as BITBUCKET_KEY is missing")
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", bitbucketEnvVarKey, bitbucketEnvVarSecret, bitbucketEnvVarUser})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	bbHost := bitbucketHost{
		Provider:         "bitbucket",
		APIURL:           bitbucketAPIURL,
		DiffRemoteMethod: refsMethod,
	}

	bbHost.Backup(backupDIR)
	expectedPathOne := filepath.Join(backupDIR, "bitbucket.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "bitbucket.com", "teamsoba", "teamsobarepoone")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^teamSobaRepoOne\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	bbHost.Backup(backupDIR)
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping.*go-soba/repo0`)
	var reRepo1 = regexp.MustCompile(`skipping.*teamsoba/teamsobarepoone`)
	var matches int

	logger.SetOutput(os.Stdout)

	for x := range logLines {
		logger.Print(logLines[x])
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

func TestPublicBitbucketRepositoryCloneCompare(t *testing.T) {
	if os.Getenv(bitbucketEnvVarKey) == "" {
		t.Skip("Skipping Bitbucket test as BITBUCKET_KEY is missing")
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", bitbucketEnvVarKey, bitbucketEnvVarSecret, bitbucketEnvVarUser})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	bbHost := bitbucketHost{
		Provider:         "bitbucket",
		APIURL:           bitbucketAPIURL,
		DiffRemoteMethod: cloneMethod,
	}

	bbHost.Backup(backupDIR)
	expectedPathOne := filepath.Join(backupDIR, "bitbucket.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "bitbucket.com", "teamsoba", "teamsobarepoone")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^teamSobaRepoOne\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	bbHost.Backup(backupDIR)
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping.*go-soba/repo0`)
	var reRepo1 = regexp.MustCompile(`skipping.*teamsoba/teamsobarepoone`)
	var matches int

	logger.SetOutput(os.Stdout)

	for x := range logLines {
		logger.Print(logLines[x])
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
