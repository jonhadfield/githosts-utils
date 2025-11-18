package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBundleWithExtraDigitInTimestamp tests the specific case where a bundle has
// an extra digit in the timestamp (15 digits instead of 14)
func TestBundleWithExtraDigitInTimestamp(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "extra-digit-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a bundle with 15-digit timestamp (extra digit) like the error case
	invalidBundleName := "goclone.202501028122009.bundle"
	invalidBundlePath := filepath.Join(tempDir, invalidBundleName)
	err = os.WriteFile(invalidBundlePath, []byte("test bundle content"), 0644)
	require.NoError(t, err)

	// Try to get bundle files - should handle the invalid timestamp
	bundles, err := getBundleFiles(tempDir)
	assert.NoError(t, err, "getBundleFiles should not fail on invalid timestamps")
	assert.Empty(t, bundles, "No valid bundles should be returned")

	// Check that the invalid bundle was renamed
	_, err = os.Stat(invalidBundlePath)
	assert.True(t, os.IsNotExist(err), "Original bundle should not exist after rename")

	invalidPath := invalidBundlePath + ".invalid"
	_, err = os.Stat(invalidPath)
	assert.NoError(t, err, "Bundle should be renamed with .invalid extension")
}

// TestGetLatestBundlePathWithAllInvalid tests that getLatestBundlePath handles
// the case where all bundles have invalid timestamps
func TestGetLatestBundlePathWithAllInvalid(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "all-invalid-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create multiple bundles with invalid timestamps
	invalidBundles := []string{
		"repo.202501028122009.bundle",     // 15 digits
		"repo.20249999999999.bundle",      // impossible date
		"repo.invalidtimestamp.bundle",    // non-numeric
		"repo.123.bundle",                 // too short
	}

	for _, name := range invalidBundles {
		bundlePath := filepath.Join(tempDir, name)
		err := os.WriteFile(bundlePath, []byte("invalid bundle"), 0644)
		require.NoError(t, err)
	}

	// Try to get latest bundle path
	latestPath, err := getLatestBundlePath(tempDir)
	assert.Error(t, err, "Should return error when no valid bundles exist")
	assert.Contains(t, err.Error(), "no valid bundle files found")
	assert.Empty(t, latestPath, "Should return empty path")

	// Verify all bundles were renamed
	for _, name := range invalidBundles {
		originalPath := filepath.Join(tempDir, name)
		_, err = os.Stat(originalPath)
		assert.True(t, os.IsNotExist(err), "Original bundle %s should not exist", name)

		invalidPath := originalPath + ".invalid"
		_, err = os.Stat(invalidPath)
		assert.NoError(t, err, "Bundle %s should be renamed with .invalid", name)
	}
}

// TestCheckBundleIsDuplicateWithInvalidBundles tests that duplicate checking
// works when existing bundles have invalid timestamps
func TestCheckBundleIsDuplicateWithInvalidBundles(t *testing.T) {
	// Create temporary directories
	workingDir, err := os.MkdirTemp("", "working-test-")
	require.NoError(t, err)
	defer os.RemoveAll(workingDir)

	backupDir, err := os.MkdirTemp("", "backup-test-")
	require.NoError(t, err)
	defer os.RemoveAll(backupDir)

	// Create an invalid bundle in backup directory
	invalidBundleName := "repo.202501028122009.bundle"
	invalidBundlePath := filepath.Join(backupDir, invalidBundleName)
	err = os.WriteFile(invalidBundlePath, []byte("invalid bundle"), 0644)
	require.NoError(t, err)

	// Create a valid bundle in working directory
	validBundleName := "repo.20250128122009.bundle"
	validBundlePath := filepath.Join(workingDir, validBundleName)
	err = os.WriteFile(validBundlePath, []byte("valid bundle"), 0644)
	require.NoError(t, err)

	// Check for duplicate
	bundleFile, isDuplicate, shouldReplace, err := checkBundleIsDuplicate(workingDir, backupDir, "")
	assert.NoError(t, err, "checkBundleIsDuplicate should not fail")
	assert.Equal(t, validBundleName, bundleFile)
	assert.False(t, isDuplicate, "Should not be duplicate when no valid bundles exist")
	assert.False(t, shouldReplace)

	// Verify the invalid bundle was renamed
	_, err = os.Stat(invalidBundlePath)
	assert.True(t, os.IsNotExist(err), "Invalid bundle should be renamed")

	invalidPath := invalidBundlePath + ".invalid"
	_, err = os.Stat(invalidPath)
	assert.NoError(t, err, "Invalid bundle should have .invalid extension")
}