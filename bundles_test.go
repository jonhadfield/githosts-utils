package githosts

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testBundleName1 = "repo0.20200401111111.bundle"
)

func TestRenameInvalidBundle(t *testing.T) {
	if getEnvOrFile(envGithubToken) == "" {
		t.Skip("Skipping GitHub test as GITHUB_TOKEN is missing")
	}

	resetBackups()

	backupDir := os.Getenv(envVarGitBackupDir)
	dfDir := path.Join(backupDir, gitHubDomain, "go-soba", "repo0")
	require.NoError(t, os.MkdirAll(dfDir, 0o755))

	dfName := testBundleName1
	dfPath := path.Join(dfDir, dfName)

	_, err := os.OpenFile(dfPath, os.O_RDONLY|os.O_CREATE, 0o666)
	require.NoError(t, err)
	// run
	gh, err := NewGitHubHost(NewGitHubHostInput{
		APIURL:           githubAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDir,
		Token:            getEnvOrFile(envGithubToken),
		BackupsToRetain:  1,
	})
	require.NoError(t, err)

	gh.Backup()
	// check only one bundle remains
	files, err := os.ReadDir(dfDir)
	require.NoError(t, err)

	dfRenamed := testBundleName1 + ".invalid"

	var originalFound int

	var renamedFound int

	for _, f := range files {
		require.NotEqual(t, f.Name(), dfName, fmt.Sprintf("unexpected bundle: %s", f.Name()))

		if dfName == f.Name() {
			originalFound++
		}

		if dfRenamed == f.Name() {
			renamedFound++
		}
	}

	require.Zero(t, originalFound)

	require.Equal(t, 1, renamedFound)
}
