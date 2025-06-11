package githosts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var testLock sync.Mutex

func TestAzureDevOpsHostBackupWithEmptyBackupDir(t *testing.T) {
	t.Parallel()

	host, err := NewAzureDevOpsHost(NewAzureDevOpsHostInput{
		Caller:           "test",
		BackupDir:        "",
		DiffRemoteMethod: "refs",
		UserName:         "testuser",
		PAT:              "testpat",
		Orgs:             []string{"testorg"},
	})

	require.Error(t, err)

	require.Nil(t, host)
}

func TestNewAzureDevOpsHostWithEmptyUserName(t *testing.T) {
	t.Parallel()

	_, err := NewAzureDevOpsHost(NewAzureDevOpsHostInput{
		UserName: "",
	})

	require.Error(t, err)
}

func TestAddBasicAuthToURLWithInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := AddBasicAuthToURL("::", "username", "password")

	require.Error(t, err)
}

func TestAzureDevOpsBackupWithMissingOrg(t *testing.T) {
	t.Parallel()

	host := AzureDevOpsHost{}

	backup := host.Backup()

	require.Error(t, backup.Error)
}

func TestAddBasicAuthToURLWithValidURL(t *testing.T) {
	t.Parallel()

	g, err := AddBasicAuthToURL("https://example.com", "bob", "batteryhorsestaple")
	require.NoError(t, err)
	require.Equal(t, "https://bob:batteryhorsestaple@example.com", g)
}

func TestDescribeAzureDevOpsOrgsReposWithInvalidOrg(t *testing.T) {
	t.Parallel()

	azureDevOpsHost := AzureDevOpsHost{
		Caller:           "test",
		Provider:         "",
		PAT:              "testpat",
		Orgs:             nil,
		UserName:         "testuser",
		DiffRemoteMethod: refsMethod,
		BackupDir:        t.TempDir(),
	}

	_, err := azureDevOpsHost.describeAzureDevOpsOrgsRepos("")

	require.Error(t, err)
}

func TestAzureDevOpsOrgBackup(t *testing.T) {
	if os.Getenv(envAzureDevOpsUserName) == "" {
		t.Skip(msgSkipAzureDevOpsUserNameMissing)
	}

	testLock.Lock()
	defer testLock.Unlock()

	logger.SetOutput(&buf)
	defer logger.SetOutput(os.Stdout)
	resetBackups()

	resetGlobals()

	envBackup := backupEnvironmentVariables()

	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, envAzureDevOpsUserName})

	backupDIR := os.Getenv(envVarGitBackupDir)

	azureDevOpsHost, err := NewAzureDevOpsHost(NewAzureDevOpsHostInput{
		Caller:           "githosts-utils-test",
		BackupDir:        backupDIR,
		DiffRemoteMethod: refsMethod,
		UserName:         os.Getenv("AZURE_DEVOPS_USERNAME"),
		PAT:              os.Getenv("AZURE_DEVOPS_PAT"),
		Orgs:             []string{os.Getenv("AZURE_DEVOPS_ORGS")},
		BackupsToRetain:  2,
	})
	require.NoError(t, err)
	azureDevOpsHost.Backup()

	expectedPathOne := filepath.Join(backupDIR, azureDevOpsDomain, "jonhadfield", "soba", "soba-one")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^soba-one\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, azureDevOpsDomain, "jonhadfield", "soba-test-one", "soba-test-one")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^soba-test-one\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, azureDevOpsDomain, "jonhadfield", "soba-test-two", "soba-test-two")
	require.DirExists(t, expectedPathThree)
	dirThreeEntries, err := dirContents(expectedPathThree)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^soba-test-two\.\d{14}\.bundle$`), dirThreeEntries[0].Name())

	// backup once more so we have bundles to compare and skip
	azureDevOpsHost.Backup()

	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")

	reRepo0 := regexp.MustCompile(`skipping clone of dev\.azure\.com repo 'jonhadfield/soba/soba-one'`)
	reRepo1 := regexp.MustCompile(`skipping clone of dev\.azure\.com repo 'jonhadfield/soba-test-one/soba-test-one'`)
	reRepo2 := regexp.MustCompile(`skipping clone of dev\.azure\.com repo 'jonhadfield/soba-test-two/soba-test-two'`)

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
