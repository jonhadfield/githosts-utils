package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidBundleHandling(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "invalid-bundle-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testCases := []struct {
		name                 string
		bundleFileName       string
		manifestFileName     string
		expectRename         bool
		expectedBundleName   string
		expectedManifestName string
	}{
		{
			name:               "valid unencrypted bundle",
			bundleFileName:     "test-repo.20241231235959.bundle",
			expectRename:       false,
			expectedBundleName: "test-repo.20241231235959.bundle",
		},
		{
			name:                 "valid encrypted bundle with manifest",
			bundleFileName:       "test-repo.20241231235959.bundle.age",
			manifestFileName:     "test-repo.20241231235959.manifest.age",
			expectRename:         false,
			expectedBundleName:   "test-repo.20241231235959.bundle.age",
			expectedManifestName: "test-repo.20241231235959.manifest.age",
		},
		{
			name:               "invalid date - wrong format",
			bundleFileName:     "test-repo.2024123.bundle",
			expectRename:       true,
			expectedBundleName: "test-repo.2024123.bundle.invalid",
		},
		{
			name:               "invalid date - impossible date",
			bundleFileName:     "test-repo.20241332235959.bundle",
			expectRename:       true,
			expectedBundleName: "test-repo.20241332235959.bundle.invalid",
		},
		{
			name:                 "invalid encrypted bundle with manifest",
			bundleFileName:       "test-repo.20249999999999.bundle.age",
			manifestFileName:     "test-repo.20249999999999.manifest.age",
			expectRename:         true,
			expectedBundleName:   "test-repo.20249999999999.bundle.age.invalid",
			expectedManifestName: "test-repo.20249999999999.manifest.age.invalid",
		},
		{
			name:               "invalid date - malformed timestamp",
			bundleFileName:     "test-repo.notadate.bundle",
			expectRename:       true,
			expectedBundleName: "test-repo.notadate.bundle.invalid",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test directory for this case
			testPath := filepath.Join(tempDir, tc.name)
			err := os.MkdirAll(testPath, 0755)
			require.NoError(t, err)

			// Create bundle file
			bundlePath := filepath.Join(testPath, tc.bundleFileName)
			err = os.WriteFile(bundlePath, []byte("test bundle content"), 0644)
			require.NoError(t, err)

			// Create manifest if specified
			if tc.manifestFileName != "" {
				manifestPath := filepath.Join(testPath, tc.manifestFileName)
				err = os.WriteFile(manifestPath, []byte("test manifest content"), 0644)
				require.NoError(t, err)
			}

			// Call getBundleFiles which should handle invalid dates
			_, err = getBundleFiles(testPath)
			// We don't check error as getBundleFiles returns bundles it can parse

			// Check if files were renamed as expected
			if tc.expectRename {
				// Original files should not exist
				_, err = os.Stat(bundlePath)
				assert.True(t, os.IsNotExist(err), "original bundle should not exist after rename")

				// Invalid-marked files should exist
				invalidBundlePath := filepath.Join(testPath, tc.expectedBundleName)
				_, err = os.Stat(invalidBundlePath)
				assert.NoError(t, err, "renamed bundle with .invalid should exist")

				if tc.manifestFileName != "" {
					manifestPath := filepath.Join(testPath, tc.manifestFileName)
					_, err = os.Stat(manifestPath)
					assert.True(t, os.IsNotExist(err), "original manifest should not exist after rename")

					invalidManifestPath := filepath.Join(testPath, tc.expectedManifestName)
					_, err = os.Stat(invalidManifestPath)
					assert.NoError(t, err, "renamed manifest with .invalid should exist")
				}
			} else {
				// Files should remain unchanged
				_, err = os.Stat(bundlePath)
				assert.NoError(t, err, "valid bundle should still exist")

				if tc.manifestFileName != "" {
					manifestPath := filepath.Join(testPath, tc.manifestFileName)
					_, err = os.Stat(manifestPath)
					assert.NoError(t, err, "valid manifest should still exist")
				}
			}
		})
	}
}

func TestRenameBundleAsInvalid(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "rename-invalid-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Run("rename unencrypted bundle", func(t *testing.T) {
		bundleName := "test-repo.20241231235959.bundle"
		bundlePath := filepath.Join(tempDir, bundleName)

		// Create bundle file
		err := os.WriteFile(bundlePath, []byte("test content"), 0644)
		require.NoError(t, err)

		// Rename as invalid
		err = renameBundleAsInvalid(tempDir, bundleName)
		assert.NoError(t, err)

		// Check original doesn't exist
		_, err = os.Stat(bundlePath)
		assert.True(t, os.IsNotExist(err))

		// Check renamed file exists
		invalidPath := bundlePath + ".invalid"
		_, err = os.Stat(invalidPath)
		assert.NoError(t, err)
	})

	t.Run("rename encrypted bundle with manifest", func(t *testing.T) {
		bundleName := "test-repo.20241231235959.bundle.age"
		manifestName := "test-repo.20241231235959.manifest.age"
		bundlePath := filepath.Join(tempDir, bundleName)
		manifestPath := filepath.Join(tempDir, manifestName)

		// Create bundle and manifest files
		err := os.WriteFile(bundlePath, []byte("encrypted bundle"), 0644)
		require.NoError(t, err)
		err = os.WriteFile(manifestPath, []byte("encrypted manifest"), 0644)
		require.NoError(t, err)

		// Rename as invalid
		err = renameBundleAsInvalid(tempDir, bundleName)
		assert.NoError(t, err)

		// Check originals don't exist
		_, err = os.Stat(bundlePath)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(manifestPath)
		assert.True(t, os.IsNotExist(err))

		// Check renamed files exist
		invalidBundlePath := bundlePath + ".invalid"
		_, err = os.Stat(invalidBundlePath)
		assert.NoError(t, err)

		invalidManifestPath := manifestPath + ".invalid"
		_, err = os.Stat(invalidManifestPath)
		assert.NoError(t, err)
	})

	t.Run("rename encrypted bundle without manifest", func(t *testing.T) {
		bundleName := "test-repo.20241231235959.bundle.age"
		bundlePath := filepath.Join(tempDir, bundleName)

		// Create only bundle file (no manifest)
		err := os.WriteFile(bundlePath, []byte("encrypted bundle"), 0644)
		require.NoError(t, err)

		// Rename as invalid
		err = renameBundleAsInvalid(tempDir, bundleName)
		assert.NoError(t, err)

		// Check original doesn't exist
		_, err = os.Stat(bundlePath)
		assert.True(t, os.IsNotExist(err))

		// Check renamed file exists
		invalidBundlePath := bundlePath + ".invalid"
		_, err = os.Stat(invalidBundlePath)
		assert.NoError(t, err)
	})
}