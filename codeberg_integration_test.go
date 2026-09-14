//go:build integration

package githosts

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	codebergEnvVarToken = "CODEBERG_TOKEN" //nolint:gosec // the name of the variable, not a credential
	// codebergTestUser owns the fixture repositories. sobaOne is public and
	// sobaTwo is private, so a run that backs up both proves the token is being
	// used for the clone and not merely for discovery.
	codebergTestUser       = "soba"
	codebergTestPublicRepo = "sobaOne"
	codebergTestPrivRepo   = "sobaTwo"
)

// skipWithoutCodebergToken keeps these tests opt-in, matching how the other
// providers' integration tests behave without credentials.
func skipWithoutCodebergToken(t *testing.T) string {
	t.Helper()

	token := os.Getenv(codebergEnvVarToken)
	if token == "" {
		t.Skipf("Skipping Codeberg test as %s is missing", codebergEnvVarToken)
	}

	return token
}

// backupOnePerRepo asserts a single bundle was written for repo and returns its
// directory entry, so a caller can go on to extract it.
func backupOnePerRepo(t *testing.T, backupDIR, owner, repo string) (string, os.DirEntry) {
	t.Helper()

	// The host derives the on-disk domain from each repository's clone URL
	// host rather than a constant, so derive it the same way here instead of
	// hardcoding a path the implementation does not actually promise.
	api, err := url.Parse(codebergAPIURL)
	require.NoError(t, err)

	repoPath := filepath.Join(backupDIR, api.Host, owner, repo)
	require.DirExists(t, repoPath, "%s should have been backed up", repo)

	entries, err := dirContents(repoPath)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly one bundle for %s", repo)
	require.Contains(t, entries[0].Name(), repo+".")

	return repoPath, entries[0]
}

// TestCodebergRepositoryBackupCloneMethod backs up the authenticated user's
// repositories and checks both arrive. sobaTwo is private, so it can only be
// cloned with the token - which is the part the SourceHut tests cannot cover,
// as SourceHut private repositories are not clonable over HTTPS with a personal
// access token.
func TestCodebergRepositoryBackupCloneMethod(t *testing.T) {
	resetBackups(t)

	token := skipWithoutCodebergToken(t)

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, codebergEnvVarToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	cb, err := NewCodebergHost(NewCodebergHostInput{
		APIURL:           codebergAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
		Token:            token,
	})
	require.NoError(t, err)

	cb.Backup()

	publicPath, publicBundle := backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPublicRepo)
	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPrivRepo)

	// Extract the public repository's bundle and confirm it carries real
	// content rather than an empty history.
	tempExtractDir := filepath.Join(t.TempDir(), "codeberg_sobaone_extract")

	require.NoError(t, extractBundleToTemp(publicBundle.Name(), publicPath, tempExtractDir))

	entries, err := os.ReadDir(tempExtractDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "extracted bundle should not be empty")
	require.DirExists(t, filepath.Join(tempExtractDir, ".git"), "extracted bundle should be a git repository")
}

// TestCodebergRepositoryBackupRefsMethod repeats the backup with the refs diff
// method, which compares remote refs against the previous bundle rather than
// cloning to decide whether anything changed.
func TestCodebergRepositoryBackupRefsMethod(t *testing.T) {
	resetBackups(t)

	token := skipWithoutCodebergToken(t)

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, codebergEnvVarToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	cb, err := NewCodebergHost(NewCodebergHostInput{
		APIURL:           codebergAPIURL,
		DiffRemoteMethod: refsMethod,
		BackupDir:        backupDIR,
		Token:            token,
	})
	require.NoError(t, err)

	cb.Backup()

	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPublicRepo)
	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPrivRepo)
}

// TestCodebergBackupIsIdempotent runs the backup twice. The second run must not
// add a second bundle: the refs are unchanged, so the new bundle is recognised
// as a duplicate and discarded rather than accumulating a copy per run.
func TestCodebergBackupIsIdempotent(t *testing.T) {
	resetBackups(t)

	token := skipWithoutCodebergToken(t)

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, codebergEnvVarToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	newHost := func() *CodebergHost {
		cb, err := NewCodebergHost(NewCodebergHostInput{
			APIURL:           codebergAPIURL,
			DiffRemoteMethod: refsMethod,
			BackupDir:        backupDIR,
			Token:            token,
		})
		require.NoError(t, err)

		return cb
	}

	newHost().Backup()
	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPublicRepo)

	newHost().Backup()

	// Still exactly one bundle per repository after the second run.
	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPublicRepo)
	backupOnePerRepo(t, backupDIR, codebergTestUser, codebergTestPrivRepo)
}
