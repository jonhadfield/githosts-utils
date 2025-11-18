package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupInvalidBundles(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "cleanup-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create bundles with invalid timestamps like the real error case
	testCases := []struct {
		name           string
		shouldBeRenamed bool
	}{
		// Valid bundles
		{"valid-repo.20250128122009.bundle", false},
		{"valid-repo.20250128122009.bundle.age", false},
		{"valid-repo.20250128122009.manifest.age", false},

		// Invalid bundles like the actual error case
		{"goclone.202501028122009.bundle", true},         // 15 digits instead of 14
		{"other-repo.20249999999999.bundle", true},       // impossible date
		{"bad-repo.invaliddate.bundle", true},            // non-numeric
		{"encrypted-bad.202501028122009.bundle.age", true}, // encrypted with 15 digits
		{"encrypted-bad.202501028122009.manifest.age", true}, // manifest for encrypted

		// Already invalid (should be ignored)
		{"already-invalid.202501028122009.bundle.invalid", false},
	}

	// Create all test files
	for _, tc := range testCases {
		filePath := filepath.Join(tempDir, tc.name)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	// Run cleanup
	cleanupInvalidBundles(tempDir)

	// Verify results
	for _, tc := range testCases {
		originalPath := filepath.Join(tempDir, tc.name)
		invalidPath := originalPath + ".invalid"

		if tc.shouldBeRenamed {
			// Should be renamed
			_, err := os.Stat(originalPath)
			assert.True(t, os.IsNotExist(err), "File %s should not exist after cleanup", tc.name)

			_, err = os.Stat(invalidPath)
			assert.NoError(t, err, "File %s should be renamed to .invalid", tc.name)
		} else {
			// Should remain unchanged
			_, err := os.Stat(originalPath)
			assert.NoError(t, err, "Valid file %s should still exist", tc.name)

			_, err = os.Stat(invalidPath)
			assert.True(t, os.IsNotExist(err), "Valid file %s should not have .invalid version", tc.name)
		}
	}
}

func TestCleanupInvalidBundlesEmptyDir(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "cleanup-empty-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Run cleanup on empty directory - should not fail
	cleanupInvalidBundles(tempDir)

	// No files should exist
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestCleanupInvalidBundlesNonExistentDir(t *testing.T) {
	// Run cleanup on non-existent directory - should not fail
	cleanupInvalidBundles("/non/existent/path")
	// Test passes if no panic occurs
}