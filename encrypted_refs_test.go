package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLatestBundleRefsWithEncryption(t *testing.T) {
	// Create temporary directories
	tempDir, err := os.MkdirTemp("", "test-encrypted-refs")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	setupTestRepo(t, repoDir)

	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	testPassphrase := "test-refs-passphrase-123"

	// Create a test repository
	repo := repository{
		Name:              "test-repo",
		Owner:             "test-owner",
		PathWithNameSpace: "test-owner/test-repo",
		Domain:            "test.com",
		HTTPSUrl:          "file://" + repoDir,
	}

	// Test 1: Create encrypted backup and test reading refs with passphrase
	err = processBackup(processBackupInput{
		LogLevel:             1,
		Repo:                 repo,
		BackupDIR:            backupDir,
		BackupsToKeep:        5,
		DiffRemoteMethod:     "clone",
		BackupLFS:            false,
		Secrets:              []string{},
		EncryptionPassphrase: testPassphrase,
	})
	require.NoError(t, err)

	// Verify encrypted bundle was created
	backupRepoDir := filepath.Join(backupDir, repo.Domain, repo.PathWithNameSpace)
	encryptedBundleFiles, err := filepath.Glob(filepath.Join(backupRepoDir, "*.bundle.age"))
	require.NoError(t, err)
	require.Len(t, encryptedBundleFiles, 1, "Should have one encrypted bundle")

	// Test reading refs from encrypted bundle with correct passphrase
	refs, err := getLatestBundleRefs(backupRepoDir, testPassphrase)
	require.NoError(t, err)
	assert.NotEmpty(t, refs, "Should be able to read refs from encrypted bundle with passphrase")

	// Verify we got actual git refs
	found := false
	for refName := range refs {
		if refName == "refs/heads/master" || refName == "refs/heads/main" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find master or main ref")

	// Test 2: Try reading refs from encrypted bundle without passphrase
	refs, err = getLatestBundleRefs(backupRepoDir, "")
	assert.Error(t, err, "Should fail to read refs from encrypted bundle without passphrase")
	assert.Contains(t, err.Error(), "encrypted bundle found but no passphrase provided", "Error should indicate passphrase needed")

	// Test 3: Try reading refs with wrong passphrase
	refs, err = getLatestBundleRefs(backupRepoDir, "wrong-passphrase")
	assert.Error(t, err, "Should fail to read refs from encrypted bundle with wrong passphrase")
}

func TestRemoteRefsMatchWithEncryptedBundle(t *testing.T) {
	// Create temporary directories
	tempDir, err := os.MkdirTemp("", "test-encrypted-refs-match")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	setupTestRepo(t, repoDir)

	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	testPassphrase := "test-refs-match-passphrase-456"

	// Create a test repository
	repo := repository{
		Name:              "test-repo",
		Owner:             "test-owner",
		PathWithNameSpace: "test-owner/test-repo",
		Domain:            "test.com",
		HTTPSUrl:          "file://" + repoDir,
	}

	// Create encrypted backup
	err = processBackup(processBackupInput{
		LogLevel:             1,
		Repo:                 repo,
		BackupDIR:            backupDir,
		BackupsToKeep:        5,
		DiffRemoteMethod:     "refs", // Use refs method
		BackupLFS:            false,
		Secrets:              []string{},
		EncryptionPassphrase: testPassphrase,
	})
	require.NoError(t, err)

	backupRepoDir := filepath.Join(backupDir, repo.Domain, repo.PathWithNameSpace)

	// Test refs matching with encrypted bundle and correct passphrase
	repoURL := "file://" + repoDir
	matches := remoteRefsMatchLocalRefs(repoURL, backupRepoDir, testPassphrase)
	assert.True(t, matches, "Refs should match when passphrase is provided for encrypted bundle")

	// Test refs matching with encrypted bundle but no passphrase
	matches = remoteRefsMatchLocalRefs(repoURL, backupRepoDir, "")
	assert.False(t, matches, "Refs should not match when no passphrase provided for encrypted bundle")
}