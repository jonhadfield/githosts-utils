package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneBackupsWithInvalidBundles(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "prune-invalid-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test files with various timestamps
	testFiles := []struct {
		name         string
		isValid      bool
		expectRename bool
	}{
		// Valid unencrypted bundles
		{"test-repo.20240101120000.bundle", true, false},
		{"test-repo.20240201120000.bundle", true, false},
		{"test-repo.20240301120000.bundle", true, false},

		// Valid encrypted bundles
		{"test-repo.20240102120000.bundle.age", true, false},
		{"test-repo.20240102120000.manifest.age", true, false},

		// Invalid unencrypted bundles
		{"test-repo.99999999999999.bundle", false, true},
		{"test-repo.invaliddate.bundle", false, true},
		{"test-repo.202401.bundle", false, true},

		// Invalid encrypted bundles
		{"test-repo.20249999999999.bundle.age", false, true},
		{"test-repo.20249999999999.manifest.age", false, true},
		{"test-repo.badtimestamp.bundle.age", false, true},
		{"test-repo.badtimestamp.manifest.age", false, true},

		// Non-bundle files (should be ignored)
		{"test-repo.20240101120000.lfs.tar", true, false},
		{"README.md", true, false},
	}

	// Create all test files
	for _, tf := range testFiles {
		filePath := filepath.Join(tempDir, tf.name)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err, "Failed to create test file: %s", tf.name)
	}

	// Run pruneBackups with a high retention to avoid actual pruning
	err = pruneBackups(tempDir, 100)
	assert.NoError(t, err, "pruneBackups should not fail")

	// Verify files were renamed as expected
	for _, tf := range testFiles {
		originalPath := filepath.Join(tempDir, tf.name)
		invalidPath := originalPath + ".invalid"

		if tf.expectRename {
			// Should be renamed to .invalid
			_, err := os.Stat(originalPath)
			assert.True(t, os.IsNotExist(err), "Original file should not exist after rename: %s", tf.name)

			_, err = os.Stat(invalidPath)
			assert.NoError(t, err, "Invalid file should exist: %s.invalid", tf.name)
		} else {
			// Should remain unchanged
			_, err := os.Stat(originalPath)
			assert.NoError(t, err, "Valid file should still exist: %s", tf.name)

			_, err = os.Stat(invalidPath)
			assert.True(t, os.IsNotExist(err), "Invalid file should not exist for valid bundle: %s.invalid", tf.name)
		}
	}
}

func TestPruneBackupsSkipsAlreadyInvalid(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "prune-skip-invalid-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create files already marked as invalid
	invalidFiles := []string{
		"test-repo.99999999999999.bundle.invalid",
		"test-repo.99999999999999.manifest.age.invalid",
		"test-repo.baddate.bundle.age.invalid",
	}

	for _, name := range invalidFiles {
		filePath := filepath.Join(tempDir, name)
		err := os.WriteFile(filePath, []byte("already invalid"), 0644)
		require.NoError(t, err)
	}

	// Run pruneBackups
	err = pruneBackups(tempDir, 100)
	assert.NoError(t, err, "pruneBackups should not fail")

	// Verify invalid files were not touched
	for _, name := range invalidFiles {
		filePath := filepath.Join(tempDir, name)
		_, err := os.Stat(filePath)
		assert.NoError(t, err, "Invalid file should still exist: %s", name)

		// Make sure they weren't renamed again
		doubleInvalidPath := filePath + ".invalid"
		_, err = os.Stat(doubleInvalidPath)
		assert.True(t, os.IsNotExist(err), "Should not double-mark invalid files: %s", doubleInvalidPath)
	}
}