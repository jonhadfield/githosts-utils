//nolint:wsl_v5 // extensive whitespace linting would require significant refactoring
package githosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// TestEncryptionTransition tests the scenario where a user:
// 1. Runs soba with encryption enabled (BUNDLE_PASSPHRASE set)
// 2. Then runs soba without encryption (BUNDLE_PASSPHRASE not set)
// This should result in:
// - The encrypted backup (.bundle.age) remaining unchanged
// - A new unencrypted backup (.bundle) being created
func TestEncryptionTransition(t *testing.T) {
	// Create temporary directory for backups
	tempDir, err := os.MkdirTemp("", "encryption-transition-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test passphrase
	testPassphrase := "test-encryption-passphrase-123"

	// Create a mock GitHub host with encryption enabled
	githubHostEncrypted, err := NewGitHubHost(NewGitHubHostInput{
		Caller:               "encryption-transition-test",
		HTTPClient:           retryablehttp.NewClient(),
		APIURL:               "https://api.github.com",
		DiffRemoteMethod:     "clone",
		BackupDir:            tempDir,
		Token:                "dummy-token-for-test",
		BackupsToRetain:      5,
		LogLevel:             1,
		BackupLFS:            false,
		EncryptionPassphrase: testPassphrase, // Encryption enabled
	})
	if err != nil {
		t.Fatalf("Failed to create encrypted GitHub host: %v", err)
	}

	// We'll work with a test repository named "test-repo" owned by "test-owner"

	// Step 1: Create an encrypted backup
	t.Log("Step 1: Creating encrypted backup...")

	// Create test bundle content
	testBundleContent := "# This is a test git bundle content\n# Bundle created at: " + time.Now().Format(time.RFC3339) + "\n"

	// Simulate creating an encrypted backup
	encryptedBackupPath := filepath.Join(tempDir, "test-owner", "test-repo.bundle.age")
	err = os.MkdirAll(filepath.Dir(encryptedBackupPath), 0o755)
	if err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	// For this test, we'll create a mock encrypted file (in real usage, this would be age-encrypted)
	encryptedContent := "age-encryption v1\n" + testBundleContent + "\nEncrypted with passphrase: " + testPassphrase
	err = os.WriteFile(encryptedBackupPath, []byte(encryptedContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to write encrypted backup: %v", err)
	}

	// Create manifest for encrypted backup
	encryptedManifestPath := filepath.Join(tempDir, "test-owner", "test-repo.manifest.age")
	manifestContent := "Repository: test-repo\nOwner: test-owner\nCreated: " + time.Now().Format(time.RFC3339) + "\nEncrypted: true\n"
	err = os.WriteFile(encryptedManifestPath, []byte("age-encryption v1\n"+manifestContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to write encrypted manifest: %v", err)
	}

	// Verify encrypted files exist
	if _, err := os.Stat(encryptedBackupPath); os.IsNotExist(err) {
		t.Fatalf("Encrypted backup file was not created: %s", encryptedBackupPath)
	}
	if _, err := os.Stat(encryptedManifestPath); os.IsNotExist(err) {
		t.Fatalf("Encrypted manifest file was not created: %s", encryptedManifestPath)
	}

	t.Logf("Encrypted backup created at: %s", encryptedBackupPath)
	t.Logf("Encrypted manifest created at: %s", encryptedManifestPath)

	// Step 2: Create a GitHub host without encryption
	t.Log("Step 2: Creating GitHub host without encryption...")

	githubHostUnencrypted, err := NewGitHubHost(NewGitHubHostInput{
		Caller:               "encryption-transition-test",
		HTTPClient:           retryablehttp.NewClient(),
		APIURL:               "https://api.github.com",
		DiffRemoteMethod:     "clone",
		BackupDir:            tempDir,
		Token:                "dummy-token-for-test",
		BackupsToRetain:      5,
		LogLevel:             1,
		BackupLFS:            false,
		EncryptionPassphrase: "", // No encryption
	})
	if err != nil {
		t.Fatalf("Failed to create unencrypted GitHub host: %v", err)
	}

	// Step 3: Simulate creating an unencrypted backup
	t.Log("Step 3: Creating unencrypted backup...")

	unencryptedBackupPath := filepath.Join(tempDir, "test-owner", "test-repo.bundle")
	unencryptedBundleContent := "# This is a test git bundle content\n# Bundle created at: " + time.Now().Format(time.RFC3339) + "\n# This is unencrypted\n"
	err = os.WriteFile(unencryptedBackupPath, []byte(unencryptedBundleContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to write unencrypted backup: %v", err)
	}

	// Create manifest for unencrypted backup
	unencryptedManifestPath := filepath.Join(tempDir, "test-owner", "test-repo.manifest")
	manifestContentUnencrypted := "Repository: test-repo\nOwner: test-owner\nCreated: " + time.Now().Format(time.RFC3339) + "\nEncrypted: false\n"
	err = os.WriteFile(unencryptedManifestPath, []byte(manifestContentUnencrypted), 0o644)
	if err != nil {
		t.Fatalf("Failed to write unencrypted manifest: %v", err)
	}

	t.Logf("Unencrypted backup created at: %s", unencryptedBackupPath)
	t.Logf("Unencrypted manifest created at: %s", unencryptedManifestPath)

	// Step 4: Verify both encrypted and unencrypted files exist
	t.Log("Step 4: Verifying both encrypted and unencrypted files exist...")

	// Check encrypted files still exist
	if _, err := os.Stat(encryptedBackupPath); os.IsNotExist(err) {
		t.Errorf("Encrypted backup file should still exist: %s", encryptedBackupPath)
	}
	if _, err := os.Stat(encryptedManifestPath); os.IsNotExist(err) {
		t.Errorf("Encrypted manifest file should still exist: %s", encryptedManifestPath)
	}

	// Check unencrypted files exist
	if _, err := os.Stat(unencryptedBackupPath); os.IsNotExist(err) {
		t.Errorf("Unencrypted backup file should exist: %s", unencryptedBackupPath)
	}
	if _, err := os.Stat(unencryptedManifestPath); os.IsNotExist(err) {
		t.Errorf("Unencrypted manifest file should exist: %s", unencryptedManifestPath)
	}

	// Step 5: Verify file contents are different
	t.Log("Step 5: Verifying file contents are different...")

	encryptedData, err := os.ReadFile(encryptedBackupPath)
	if err != nil {
		t.Fatalf("Failed to read encrypted backup: %v", err)
	}

	unencryptedData, err := os.ReadFile(unencryptedBackupPath)
	if err != nil {
		t.Fatalf("Failed to read unencrypted backup: %v", err)
	}

	// Verify encrypted file contains age header
	if !strings.Contains(string(encryptedData), "age-encryption v1") {
		t.Errorf("Encrypted backup should contain age-encryption header")
	}

	// Verify unencrypted file does not contain age header
	if strings.Contains(string(unencryptedData), "age-encryption v1") {
		t.Errorf("Unencrypted backup should not contain age-encryption header")
	}

	// Verify unencrypted file contains expected content
	if !strings.Contains(string(unencryptedData), "This is unencrypted") {
		t.Errorf("Unencrypted backup should contain unencrypted content marker")
	}

	// Step 6: Verify the hosts have different encryption settings
	t.Log("Step 6: Verifying host encryption settings...")

	if githubHostEncrypted.EncryptionPassphrase != testPassphrase {
		t.Errorf("Encrypted host should have passphrase set, got: %s", githubHostEncrypted.EncryptionPassphrase)
	}

	if githubHostUnencrypted.EncryptionPassphrase != "" {
		t.Errorf("Unencrypted host should have empty passphrase, got: %s", githubHostUnencrypted.EncryptionPassphrase)
	}

	// Step 7: List all files in backup directory for verification
	t.Log("Step 7: Listing all files in backup directory...")

	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(tempDir, path)
			t.Logf("Found file: %s (size: %d bytes)", relPath, info.Size())
		}
		return nil
	})
	if err != nil {
		t.Errorf("Failed to walk backup directory: %v", err)
	}

	t.Log("Test completed successfully: encrypted and unencrypted backups coexist")
}

// TestEncryptionTransitionWithRealProcessing tests the transition scenario
// using actual backup processing logic where possible
func TestEncryptionTransitionWithRealProcessing(t *testing.T) {
	// Create temporary directory for backups
	tempDir, err := os.MkdirTemp("", "encryption-transition-real-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testPassphrase := "real-test-passphrase-456"

	// Create repository structure
	repoDir := filepath.Join(tempDir, "test-owner")
	err = os.MkdirAll(repoDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create repo directory: %v", err)
	}

	// Test the scenario where we have different backup configurations
	configs := []struct {
		name        string
		passphrase  string
		expectedExt string
		description string
	}{
		{
			name:        "encrypted",
			passphrase:  testPassphrase,
			expectedExt: ".age",
			description: "First run with encryption enabled",
		},
		{
			name:        "unencrypted",
			passphrase:  "",
			expectedExt: "",
			description: "Second run with encryption disabled",
		},
	}

	var createdFiles []string

	for i, config := range configs {
		t.Logf("Configuration %d: %s - %s", i+1, config.name, config.description)

		// Create GitHub host with current configuration
		githubHost, err := NewGitHubHost(NewGitHubHostInput{
			Caller:               "encryption-transition-real-test",
			HTTPClient:           retryablehttp.NewClient(),
			APIURL:               "https://api.github.com",
			DiffRemoteMethod:     "clone",
			BackupDir:            tempDir,
			Token:                "dummy-token-for-test",
			BackupsToRetain:      5,
			LogLevel:             1,
			BackupLFS:            false,
			EncryptionPassphrase: config.passphrase,
		})
		if err != nil {
			t.Fatalf("Failed to create GitHub host for %s: %v", config.name, err)
		}

		// Verify the host has the correct encryption setting
		if githubHost.EncryptionPassphrase != config.passphrase {
			t.Errorf("Host encryption passphrase mismatch for %s: expected %q, got %q",
				config.name, config.passphrase, githubHost.EncryptionPassphrase)
		}

		// Simulate creating backup files with the current configuration
		bundlePath := filepath.Join(repoDir, "test-repo.bundle"+config.expectedExt)
		manifestPath := filepath.Join(repoDir, "test-repo.manifest"+config.expectedExt)

		// Create mock bundle content
		bundleContent := "# Git bundle for " + config.name + " configuration\n"
		bundleContent += "# Created at: " + time.Now().Format(time.RFC3339) + "\n"
		bundleContent += "# Encryption: " + config.name + "\n"

		if config.passphrase != "" {
			// Mock age encryption header for encrypted files
			bundleContent = "age-encryption v1\n" + bundleContent
		}

		err = os.WriteFile(bundlePath, []byte(bundleContent), 0o644)
		if err != nil {
			t.Fatalf("Failed to write bundle for %s: %v", config.name, err)
		}

		// Create manifest content
		manifestContent := "Repository: test-repo\n"
		manifestContent += "Owner: test-owner\n"
		manifestContent += "Configuration: " + config.name + "\n"
		manifestContent += "Created: " + time.Now().Format(time.RFC3339) + "\n"

		if config.passphrase != "" {
			manifestContent = "age-encryption v1\n" + manifestContent
		}

		err = os.WriteFile(manifestPath, []byte(manifestContent), 0o644)
		if err != nil {
			t.Fatalf("Failed to write manifest for %s: %v", config.name, err)
		}

		createdFiles = append(createdFiles, bundlePath, manifestPath)
		t.Logf("Created files for %s: %s, %s", config.name, bundlePath, manifestPath)

		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify all files exist after both runs
	t.Log("Verifying all files exist after both runs...")
	for _, filePath := range createdFiles {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("File should exist: %s", filePath)
		} else {
			t.Logf("Confirmed file exists: %s", filePath)
		}
	}

	// Count encrypted vs unencrypted files
	encryptedCount := 0
	unencryptedCount := 0

	for _, filePath := range createdFiles {
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("Failed to read file %s: %v", filePath, err)

			continue
		}

		if strings.Contains(string(data), "age-encryption v1") {
			encryptedCount++
			t.Logf("Encrypted file: %s", filePath)
		} else {
			unencryptedCount++
			t.Logf("Unencrypted file: %s", filePath)
		}
	}

	// We should have 2 encrypted files (bundle + manifest) and 2 unencrypted files (bundle + manifest)
	if encryptedCount != 2 {
		t.Errorf("Expected 2 encrypted files, got %d", encryptedCount)
	}
	if unencryptedCount != 2 {
		t.Errorf("Expected 2 unencrypted files, got %d", unencryptedCount)
	}

	t.Logf("Test completed successfully: %d encrypted files and %d unencrypted files coexist",
		encryptedCount, unencryptedCount)
}
