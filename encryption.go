package githosts

import (
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"gitlab.com/tozd/go/errors"
)

const encryptedBundleExtension = ".age"

// encryptFile encrypts a file using age encryption with a passphrase
func encryptFile(inputPath, outputPath, passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase cannot be empty")
	}

	// Create recipient from passphrase
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return errors.Errorf("failed to create age recipient: %s", err)
	}

	// Open input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return errors.Errorf("failed to open input file: %s", err)
	}
	defer inputFile.Close()

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return errors.Errorf("failed to create output file: %s", err)
	}
	defer outputFile.Close()

	// Create age encryptor
	encryptor, err := age.Encrypt(outputFile, recipient)
	if err != nil {
		return errors.Errorf("failed to create age encryptor: %s", err)
	}

	// Copy input to encrypted output
	if _, err = io.Copy(encryptor, inputFile); err != nil {
		return errors.Errorf("failed to encrypt file: %s", err)
	}

	// Close the encryptor to finalize encryption
	if err = encryptor.Close(); err != nil {
		return errors.Errorf("failed to finalize encryption: %s", err)
	}

	return nil
}

// decryptFile decrypts a file using age encryption with a passphrase
func decryptFile(inputPath, outputPath, passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase cannot be empty")
	}

	// Create identity from passphrase
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return errors.Errorf("failed to create age identity: %s", err)
	}

	// Open encrypted input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return errors.Errorf("failed to open encrypted file: %s", err)
	}
	defer inputFile.Close()

	// Create age decryptor
	decryptor, err := age.Decrypt(inputFile, identity)
	if err != nil {
		return errors.Errorf("failed to create age decryptor: %s", err)
	}

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return errors.Errorf("failed to create output file: %s", err)
	}
	defer outputFile.Close()

	// Copy decrypted content to output
	if _, err = io.Copy(outputFile, decryptor); err != nil {
		return errors.Errorf("failed to decrypt file: %s", err)
	}

	return nil
}

// isEncryptedBundle checks if a bundle file is encrypted (has .bundle.age extension)
func isEncryptedBundle(bundlePath string) bool {
	return strings.HasSuffix(bundlePath, bundleExtension+encryptedBundleExtension)
}

// getOriginalBundleName removes the .age extension to get the original bundle name
func getOriginalBundleName(encryptedBundlePath string) string {
	if isEncryptedBundle(encryptedBundlePath) {
		return strings.TrimSuffix(encryptedBundlePath, encryptedBundleExtension)
	}
	return encryptedBundlePath
}

// compareEncryptedWithPlain compares an encrypted bundle with a plain bundle
// by decrypting the encrypted one temporarily and comparing hashes
func compareEncryptedWithPlain(encryptedPath, plainPath, passphrase string) (bool, error) {
	// Create a temporary file for decryption
	tempFile, err := os.CreateTemp("", "decrypted-bundle-*.bundle")
	if err != nil {
		return false, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Decrypt the encrypted bundle to temp file
	if err = decryptFile(encryptedPath, tempPath, passphrase); err != nil {
		return false, fmt.Errorf("failed to decrypt bundle for comparison: %w", err)
	}

	// Compare the decrypted bundle with the plain bundle
	return filesIdentical(tempPath, plainPath), nil
}
