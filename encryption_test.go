package githosts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "encryption-test")
	require.NoError(t, err)

	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.bundle")
	testContent := []byte("This is a test bundle content")
	err = os.WriteFile(testFile, testContent, 0o644)
	require.NoError(t, err)

	// Test encryption
	passphrase := "test-passphrase-123"
	encryptedFile := testFile + ".age"

	err = encryptFile(testFile, encryptedFile, passphrase)
	assert.NoError(t, err)

	// Verify encrypted file exists
	_, err = os.Stat(encryptedFile)
	assert.NoError(t, err)

	// Test decryption
	decryptedFile := filepath.Join(tempDir, "decrypted.bundle")
	err = decryptFile(encryptedFile, decryptedFile, passphrase)
	assert.NoError(t, err)

	// Verify decrypted content matches original
	decryptedContent, err := os.ReadFile(decryptedFile)
	assert.NoError(t, err)
	assert.Equal(t, testContent, decryptedContent)

	// Test wrong passphrase
	wrongPassphrase := "wrong-passphrase"
	wrongDecryptedFile := filepath.Join(tempDir, "wrong-decrypted.bundle")
	err = decryptFile(encryptedFile, wrongDecryptedFile, wrongPassphrase)
	assert.Error(t, err)
}

func TestIsEncryptedBundle(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"repo.20240101000000.bundle", false},
		{"repo.20240101000000.bundle.age", true},
		{"repo.bundle", false},
		{"repo.bundle.age", true},
		{"not-a-bundle.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isEncryptedBundle(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOriginalBundleName(t *testing.T) {
	tests := []struct {
		encrypted string
		expected  string
	}{
		{"repo.20240101000000.bundle.age", "repo.20240101000000.bundle"},
		{"repo.20240101000000.bundle", "repo.20240101000000.bundle"},
		{"test.bundle.age", "test.bundle"},
	}

	for _, tt := range tests {
		t.Run(tt.encrypted, func(t *testing.T) {
			result := getOriginalBundleName(tt.encrypted)
			assert.Equal(t, tt.expected, result)
		})
	}
}
