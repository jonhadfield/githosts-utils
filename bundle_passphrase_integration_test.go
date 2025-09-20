package githosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBundlePassphraseEnvironmentIntegration verifies encryption passphrase functionality
func TestBundlePassphraseEnvironmentIntegration(t *testing.T) {
	testPassphrase := "integration-test-passphrase-secure"

	// Test 1: Verify processBackup with empty passphrase creates unencrypted bundles
	t.Run("Without_Encryption", func(t *testing.T) {
		// Create temporary directories
		tempDir, err := os.MkdirTemp("", "bundle-no-encryption")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		repoDir := filepath.Join(tempDir, "test-repo")
		require.NoError(t, os.MkdirAll(repoDir, 0755))
		setupTestRepo(t, repoDir)

		backupDir := filepath.Join(tempDir, "backup")
		require.NoError(t, os.MkdirAll(backupDir, 0755))

		repo := repository{
			Name:              "test-repo",
			Owner:             "test-owner",
			PathWithNameSpace: "test-owner/test-repo",
			Domain:            "test.com",
			HTTPSUrl:          "file://" + repoDir,
		}

		// Process backup without encryption
		err = processBackup(processBackupInput{
			LogLevel:             1,
			Repo:                 repo,
			BackupDIR:            backupDir,
			BackupsToKeep:        5,
			DiffRemoteMethod:     "clone",
			BackupLFS:            false,
			Secrets:              []string{},
			EncryptionPassphrase: "", // No encryption
		})
		require.NoError(t, err)

		// Verify unencrypted bundle was created
		backupRepoDir := filepath.Join(backupDir, repo.Domain, repo.PathWithNameSpace)
		bundleFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.bundle"))
		require.NoError(t, err)

		var plainBundles []string
		for _, f := range bundleFiles {
			if !strings.HasSuffix(f, ".bundle.age") {
				plainBundles = append(plainBundles, f)
			}
		}
		assert.Len(t, plainBundles, 1, "Should have exactly one unencrypted bundle")

		// Verify no encrypted bundles
		encryptedBundleFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.bundle.age"))
		require.NoError(t, err)
		assert.Len(t, encryptedBundleFiles, 0, "Should have no encrypted bundles")
	})

	// Test 2: Verify processBackup with passphrase creates encrypted bundles
	t.Run("With_Encryption", func(t *testing.T) {
		// Create temporary directories
		tempDir, err := os.MkdirTemp("", "bundle-with-encryption")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		repoDir := filepath.Join(tempDir, "test-repo")
		require.NoError(t, os.MkdirAll(repoDir, 0755))
		setupTestRepo(t, repoDir)

		backupDir := filepath.Join(tempDir, "backup")
		require.NoError(t, os.MkdirAll(backupDir, 0755))

		repo := repository{
			Name:              "test-repo",
			Owner:             "test-owner",
			PathWithNameSpace: "test-owner/test-repo",
			Domain:            "test.com",
			HTTPSUrl:          "file://" + repoDir,
		}

		// Process backup with encryption
		err = processBackup(processBackupInput{
			LogLevel:             1,
			Repo:                 repo,
			BackupDIR:            backupDir,
			BackupsToKeep:        5,
			DiffRemoteMethod:     "clone",
			BackupLFS:            false,
			Secrets:              []string{},
			EncryptionPassphrase: testPassphrase, // With encryption
		})
		require.NoError(t, err)

		// Verify encrypted bundle was created
		backupRepoDir := filepath.Join(backupDir, repo.Domain, repo.PathWithNameSpace)
		encryptedBundleFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.bundle.age"))
		require.NoError(t, err)
		assert.Len(t, encryptedBundleFiles, 1, "Should have exactly one encrypted bundle")

		// Verify no unencrypted bundles
		bundleFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.bundle"))
		require.NoError(t, err)
		var plainBundles []string
		for _, f := range bundleFiles {
			if !strings.HasSuffix(f, ".bundle.age") {
				plainBundles = append(plainBundles, f)
			}
		}
		assert.Len(t, plainBundles, 0, "Should have no unencrypted bundles")

		// Verify encrypted manifest
		encryptedManifestFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.manifest.age"))
		require.NoError(t, err)
		assert.Len(t, encryptedManifestFiles, 1, "Should have exactly one encrypted manifest")
	})

	// Test 3: Verify that hosts created with passphrase have correct EncryptionPassphrase
	t.Run("Host_With_EncryptionPassphrase", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "bundle-host-test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		backupDir := filepath.Join(tempDir, "backup")

		// Create a GitHub host with encryption passphrase
		ghHost, err := NewGitHubHost(NewGitHubHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			Token:                "dummy-token",
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, ghHost.EncryptionPassphrase)

		// Similarly for other hosts
		glHost, err := NewGitLabHost(NewGitLabHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			Token:                "dummy-token",
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, glHost.EncryptionPassphrase)

		giteaHost, err := NewGiteaHost(NewGiteaHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			APIURL:               "https://dummy-gitea.com/api/v1",
			Token:                "dummy-token",
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, giteaHost.EncryptionPassphrase)

		bbHost, err := NewBitBucketHost(NewBitBucketHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			AuthType:             AuthTypeBitbucketAPIToken,
			Email:                "test@example.com",
			APIToken:             "dummy-token",
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, bbHost.EncryptionPassphrase)

		adHost, err := NewAzureDevOpsHost(NewAzureDevOpsHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			UserName:             "test-user",
			PAT:                  "dummy-pat",
			Orgs:                 []string{"test-org"},
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, adHost.EncryptionPassphrase)

		shHost, err := NewSourcehutHost(NewSourcehutHostInput{
			Caller:               "test",
			BackupDir:            backupDir,
			APIURL:               "https://dummy-sourcehut.com/api",
			PersonalAccessToken:  "dummy-token",
			EncryptionPassphrase: testPassphrase,
			DiffRemoteMethod:     "clone",
			BackupsToRetain:      5,
		})
		require.NoError(t, err)
		assert.Equal(t, testPassphrase, shHost.EncryptionPassphrase)
	})
}

