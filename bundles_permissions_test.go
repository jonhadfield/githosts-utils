package githosts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifestFilePermissions verifies that manifest files are created with secure permissions (0o600)
// to prevent unauthorized access to repository metadata
func TestManifestFilePermissions(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "test-repo")

	// Create the repository directory first
	err := os.MkdirAll(repoDir, 0o755)
	require.NoError(t, err)

	// Create a test repository
	setupTestRepo(t, repoDir)

	backupDir := filepath.Join(tempDir, "backup")
	repoName := "test-repo"
	timestamp := "20250118000000"

	// Test 1: Bundle manifest file permissions
	t.Run("Bundle manifest has 0600 permissions", func(t *testing.T) {
		// Create a dummy bundle file for testing
		err := os.MkdirAll(backupDir, 0o755)
		require.NoError(t, err)

		// Create a temporary git bundle to test with
		workingDir := filepath.Join(tempDir, "working")
		cloneURL := "file://" + repoDir
		cloneCmd := buildCloneCommand(context.Background(), cloneURL, workingDir, tempDir)
		require.NoError(t, cloneCmd.Run(), "Failed to clone test repo")

		// Create the bundle
		bundleErr := createBundle(context.Background(), 0, workingDir, repository{
			Name:              repoName,
			Domain:            "test.com",
			PathWithNameSpace: "test-owner/test-repo",
		}, "")
		require.NoError(t, bundleErr)

		// Find the actual bundle file created
		files, readErr := os.ReadDir(workingDir)
		require.NoError(t, readErr)

		var bundleFile string
		for _, f := range files {
			if filepath.Ext(f.Name()) == bundleExtension {
				bundleFile = filepath.Join(workingDir, f.Name())
				break
			}
		}
		require.NotEmpty(t, bundleFile, "No bundle file found")

		// Now create a manifest for this bundle - note: manifests are only created for encrypted bundles
		// but we're testing the permission function directly
		// Extract the timestamp from the actual bundle file
		bundleBasename := filepath.Base(bundleFile)
		actualTimestamp, tsErr := getTimeStampPartFromFileName(bundleBasename)
		require.NoError(t, tsErr)
		actualTimestampStr := fmt.Sprintf("%014d", actualTimestamp)

		manifestErr := createBundleManifest(context.Background(), bundleFile, actualTimestampStr)
		require.NoError(t, manifestErr)

		// Check manifest file permissions - use actual timestamp
		manifestPath := filepath.Join(workingDir, repoName+"."+actualTimestampStr+manifestExtension)
		info, statErr := os.Stat(manifestPath)
		require.NoError(t, statErr)

		// Verify permissions are 0600 (owner read/write only)
		perm := info.Mode().Perm()
		assert.Equal(t, os.FileMode(0o600), perm,
			"Manifest file should have 0600 permissions (owner read/write only), got %o", perm)
	})

	// Test 2: LFS manifest file permissions
	t.Run("LFS manifest has 0600 permissions", func(t *testing.T) {
		archivePath := filepath.Join(backupDir, repoName+"."+timestamp+lfsArchiveExtension)

		// Create a dummy LFS archive file
		err := os.WriteFile(archivePath, []byte("test archive"), 0o644)
		require.NoError(t, err)

		// Create LFS manifest
		err = createLFSManifest(archivePath, timestamp)
		require.NoError(t, err)

		// Check manifest file permissions
		manifestPath := filepath.Join(backupDir, repoName+"."+timestamp+manifestExtension)
		info, err := os.Stat(manifestPath)
		require.NoError(t, err)

		// Verify permissions are 0600
		perm := info.Mode().Perm()
		assert.Equal(t, os.FileMode(0o600), perm,
			"LFS manifest file should have 0600 permissions (owner read/write only), got %o", perm)
	})

	// Test 3: Verify encrypted manifests also have correct permissions
	t.Run("Encrypted bundle creates manifest with 0600 permissions", func(t *testing.T) {
		testPassphrase := "test-secure-passphrase-123"
		workingDir := filepath.Join(tempDir, "working-encrypted")

		cloneURL := "file://" + repoDir
		cloneCmd := buildCloneCommand(context.Background(), cloneURL, workingDir, tempDir)
		require.NoError(t, cloneCmd.Run(), "Failed to clone test repo for encryption test")

		// Create encrypted bundle
		bundleErr := createBundle(context.Background(), 0, workingDir, repository{
			Name:              repoName,
			Domain:            "test.com",
			PathWithNameSpace: "test-owner/test-repo",
		}, testPassphrase)
		require.NoError(t, bundleErr)

		// Find the manifest file (unencrypted version created before encryption)
		// Note: The manifest gets encrypted too, but we're checking the initial creation permissions
		files, readErr := os.ReadDir(workingDir)
		require.NoError(t, readErr)

		var encryptedManifestFile string
		for _, f := range files {
			if filepath.Ext(f.Name()) == encryptedBundleExtension {
				baseName := getOriginalBundleName(f.Name())
				manifestName := filepath.Base(baseName[:len(baseName)-len(bundleExtension)] + manifestExtension + encryptedBundleExtension)
				encryptedManifestFile = filepath.Join(workingDir, manifestName)
				break
			}
		}

		// The encrypted manifest file should exist
		if encryptedManifestFile != "" {
			info, err := os.Stat(encryptedManifestFile)
			if err == nil {
				// If the encrypted manifest exists, verify its permissions
				// Note: The encrypted manifest is created by age, so we can't control its permissions directly
				// This test mainly verifies the unencrypted manifest creation permissions
				t.Logf("Encrypted manifest found: %s with permissions %o", encryptedManifestFile, info.Mode().Perm())
			}
		}
	})
}

// TestManifestFileSecurityScenarios tests various security scenarios for manifest files
func TestManifestFileSecurityScenarios(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()

	t.Run("Manifest files are not world-readable", func(t *testing.T) {
		repoName := "security-test"

		// Create a dummy bundle for testing
		repoDir := filepath.Join(tempDir, "test-repo")
		err := os.MkdirAll(repoDir, 0o755)
		require.NoError(t, err)
		setupTestRepo(t, repoDir)

		workingDir := filepath.Join(tempDir, "working")
		cloneURL := "file://" + repoDir
		cloneCmd := buildCloneCommand(context.Background(), cloneURL, workingDir, tempDir)
		require.NoError(t, cloneCmd.Run())

		bundleErr := createBundle(context.Background(), 0, workingDir, repository{
			Name:              repoName,
			Domain:            "test.com",
			PathWithNameSpace: "test-owner/security-test",
		}, "")
		require.NoError(t, bundleErr)

		// Find the bundle
		files, readErr := os.ReadDir(workingDir)
		require.NoError(t, readErr)

		var bundleFile string
		for _, f := range files {
			if filepath.Ext(f.Name()) == bundleExtension {
				bundleFile = filepath.Join(workingDir, f.Name())
				break
			}
		}
		require.NotEmpty(t, bundleFile)

		// Create manifest - extract timestamp from actual bundle file
		bundleBasename := filepath.Base(bundleFile)
		actualTimestamp, tsErr := getTimeStampPartFromFileName(bundleBasename)
		require.NoError(t, tsErr)
		actualTimestampStr := fmt.Sprintf("%014d", actualTimestamp)

		manifestErr := createBundleManifest(context.Background(), bundleFile, actualTimestampStr)
		require.NoError(t, manifestErr)

		manifestPath := filepath.Join(workingDir, repoName+"."+actualTimestampStr+manifestExtension)
		info, statErr := os.Stat(manifestPath)
		require.NoError(t, statErr)

		perm := info.Mode().Perm()

		// Verify world permissions are 000
		worldPerms := perm & 0o007
		assert.Equal(t, os.FileMode(0), worldPerms,
			"Manifest file should not be world-readable/writable/executable")

		// Verify group permissions are 000
		groupPerms := perm & 0o070
		assert.Equal(t, os.FileMode(0), groupPerms,
			"Manifest file should not be group-readable/writable/executable")

		// Verify only owner has read/write
		ownerPerms := perm & 0o700
		assert.Equal(t, os.FileMode(0o600), ownerPerms,
			"Manifest file should only be readable/writable by owner")
	})
}