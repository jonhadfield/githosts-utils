package githosts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	sourcehutEnvVarToken = "SOURCEHUT_PAT" //nolint:gosec
)

func TestPublicsourcehutRepositoryBackupCloneMethod(t *testing.T) {
	resetBackups()
	if os.Getenv(sourcehutEnvVarToken) == "" {
		t.Skip("Skipping sourcehut test as SOURCEHUT_PAT is missing")
	}

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, sourcehutEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewSourcehutHost(NewSourcehutHostInput{
		APIURL:              sourcehutAPIURL,
		DiffRemoteMethod:    cloneMethod,
		BackupDir:           backupDIR,
		PersonalAccessToken: os.Getenv(sourcehutEnvVarToken),
	})
	require.NoError(t, err)

	gl.Backup()

	// Test that the public repository (sobaOne) was backed up successfully
	expectedSobaOnePath := filepath.Join(backupDIR, "sourcehut", "jonhadfield", "sobaOne")
	require.DirExists(t, expectedSobaOnePath)
	projectOneEntries, err := dirContents(expectedSobaOnePath)
	require.NoError(t, err)
	require.Len(t, projectOneEntries, 1)
	require.Contains(t, projectOneEntries[0].Name(), "sobaOne.")

	// Verify repository contents by extracting the bundle and checking for expected files
	bundlePath := projectOneEntries[0]
	tempExtractDir := filepath.Join(os.TempDir(), "sobaOne_extract_test")

	defer func() {
		if cleanupErr := os.RemoveAll(tempExtractDir); cleanupErr != nil {
			t.Logf("Warning: failed to cleanup temp directory: %v", cleanupErr)
		}
	}()

	// Extract the bundle to temporary directory
	err = extractBundleToTemp(bundlePath.Name(), expectedSobaOnePath, tempExtractDir)
	require.NoError(t, err)

	// Check that LICENSE and README.md exist in the repository root
	licensePath := filepath.Join(tempExtractDir, "LICENSE")
	readmePath := filepath.Join(tempExtractDir, "README.md")

	require.FileExists(t, licensePath, "LICENSE file should exist in sobaOne repository")
	require.FileExists(t, readmePath, "README.md file should exist in sobaOne repository")

	// Note: sobaTwo is private and will be automatically skipped
	// SourceHut private repositories cannot be cloned via HTTPS with personal access tokens
	// Only public repositories are backed up due to authentication limitations
	t.Logf("Public repository sobaOne backed up successfully and contains expected files")
}

func TestPublicsourcehutRepositoryBackupRefsMethod(t *testing.T) {
	resetBackups()
	if os.Getenv(sourcehutEnvVarToken) == "" {
		t.Skip("Skipping sourcehut test as SOURCEHUT_PAT is missing")
	}

	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, sourcehutEnvVarToken})
	backupDIR := os.Getenv(envVarGitBackupDir)

	gl, err := NewSourcehutHost(NewSourcehutHostInput{
		APIURL:              sourcehutAPIURL,
		DiffRemoteMethod:    refsMethod,
		BackupDir:           backupDIR,
		PersonalAccessToken: os.Getenv(sourcehutEnvVarToken),
		LogLevel:            1,
	})
	require.NoError(t, err)

	gl.Backup()

	// Test that the public repository (sobaOne) was backed up successfully
	expectedSobaOnePath := filepath.Join(backupDIR, "sourcehut", "jonhadfield", "sobaOne")
	require.DirExists(t, expectedSobaOnePath)
	projectOneEntries, err := dirContents(expectedSobaOnePath)
	require.NoError(t, err)
	require.Len(t, projectOneEntries, 1)
	require.Contains(t, projectOneEntries[0].Name(), "sobaOne.")

	// Verify repository contents by extracting the bundle and checking for expected files
	bundlePath := projectOneEntries[0]
	tempExtractDir := filepath.Join(os.TempDir(), "sobaOne_extract_test_refs")

	defer func() {
		if cleanupErr := os.RemoveAll(tempExtractDir); cleanupErr != nil {
			t.Logf("Warning: failed to cleanup temp directory: %v", cleanupErr)
		}
	}()

	// Extract the bundle to temporary directory
	err = extractBundleToTemp(bundlePath.Name(), expectedSobaOnePath, tempExtractDir)
	require.NoError(t, err)

	// Check that LICENSE and README.md exist in the repository root
	licensePath := filepath.Join(tempExtractDir, "LICENSE")
	readmePath := filepath.Join(tempExtractDir, "README.md")

	require.FileExists(t, licensePath, "LICENSE file should exist in sobaOne repository")
	require.FileExists(t, readmePath, "README.md file should exist in sobaOne repository")

	// Note: sobaTwo is private and will be automatically skipped
	// SourceHut private repositories cannot be cloned via HTTPS with personal access tokens
	t.Logf("Public repository sobaOne backed up successfully with refs method and contains expected files")
}

// extractBundleToTemp extracts a git bundle to a temporary directory for content verification
func extractBundleToTemp(bundleFileName, bundleDir, tempDir string) error {
	// Create temporary directory
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Full path to the bundle file
	bundlePath := filepath.Join(bundleDir, bundleFileName)

	// Use git clone to extract bundle contents
	cloneCmd := exec.Command("git", "clone", bundlePath, tempDir)

	output, err := cloneCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone bundle %s: %s - %w", bundlePath, string(output), err)
	}

	return nil
}
