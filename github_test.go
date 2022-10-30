package githosts

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var buf bytes.Buffer

func init() {
	logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
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

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITHUB_TOKEN"})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	ghHost := githubHost{
		Provider: "GitHub",
		APIURL:   githubAPIURL,
	}

	ghHost.Backup(backupDIR)

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

	restoreEnvironmentVariables(envBackup)
}

func TestPublicGitHubRepositoryQuickCompare(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	// need to set output to buffer in order to test output
	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)

	resetBackups()
	require.NoError(t, os.Setenv("SOBA_DEV", "true"))
	defer os.Unsetenv("SOBA_DEV")

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITHUB_TOKEN"})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	ghHost := githubHost{
		Provider: "GitHub",
		APIURL:   githubAPIURL,
	}

	ghHost.Backup(backupDIR)

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
	ghHost.Backup(backupDIR)
	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")
	var reRepo0 = regexp.MustCompile(`skipping.*go-soba/repo0`)
	var reRepo1 = regexp.MustCompile(`skipping.*go-soba/repo1`)
	var matches int
	for x := range logLines {
		fmt.Println(logLines[x])
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
