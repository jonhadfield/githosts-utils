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
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, bitbucketEnvVarKey, bitbucketEnvVarSecret, bitbucketEnvVarUser})

	bbHost, err := NewBitBucketHost(NewBitBucketHostInput{
		APIURL:           bitbucketAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        os.Getenv(envVarGitBackupDir),
		User:             os.Getenv(bitbucketEnvVarUser),
		Key:              os.Getenv(bitbucketEnvVarKey),
		Secret:           os.Getenv(bitbucketEnvVarSecret),
		LogLevel:         1,
	})
	require.NoError(t, err)

	bbHost.Backup()
	expectedPathOne := filepath.Join(bbHost.BackupDir, "bitbucket.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(bbHost.BackupDir, "bitbucket.com", "teamsoba", "teamsobarepoone")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^teamSobaRepoOne\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	bbHost.Backup()
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping.*go-soba/repo0`)
	var reRepo1 = regexp.MustCompile(`skipping.*teamsoba/teamsobarepoone`)
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
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, bitbucketEnvVarKey, bitbucketEnvVarSecret, bitbucketEnvVarUser})

	bbHost, err := NewBitBucketHost(NewBitBucketHostInput{
		APIURL:           bitbucketAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        os.Getenv(envVarGitBackupDir),
		User:             os.Getenv(bitbucketEnvVarUser),
		Key:              os.Getenv(bitbucketEnvVarKey),
		Secret:           os.Getenv(bitbucketEnvVarSecret),
	})
	require.NoError(t, err)

	bbHost.Backup()

	expectedPathOne := filepath.Join(bbHost.BackupDir, "bitbucket.com", "go-soba", "repo0")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(bbHost.BackupDir, "bitbucket.com", "teamsoba", "teamsobarepoone")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^teamSobaRepoOne\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	bbHost.Backup()
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	var reRepo0 = regexp.MustCompile(`skipping.*go-soba/repo0`)
	var reRepo1 = regexp.MustCompile(`skipping.*teamsoba/teamsobarepoone`)
	var matches int

	logger.SetOutput(os.Stdout)

	for x := range logLines {
		if reRepo0.MatchString(logLines[x]) {
			matches++
		}
		if reRepo1.MatchString(logLines[x]) {
			matches++
		}
	}

	require.Equal(t, 2, matches)
}
