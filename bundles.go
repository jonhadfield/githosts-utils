package githosts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"
)

const (
	bundleExtension     = ".bundle"
	lfsArchiveExtension = ".lfs.tar.gz"
	manifestExtension   = ".manifest"
	// invalidBundleStringCheck checks for a portion of the following in the command output
	// to determine if valid: "does not look like a v2 or v3 bundle file".
	invalidBundleStringCheck = "does not look like"
	bundleTimestampChars     = 14
	minBundleFileNameTokens  = 3
)

func getLatestBundlePath(backupPath string) (string, error) {
	bFiles, err := getBundleFiles(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to get bundle files: %w", err)
	}

	if len(bFiles) == 0 {
		return "", errors.New("no bundle files found in path")
	}

	// get timestamps in filenames for sorting
	fNameTimes := map[string]int{}

	for _, f := range bFiles {
		var ts int

		if ts, err = getTimeStampPartFromFileName(f.info.Name()); err == nil {
			fNameTimes[f.info.Name()] = ts

			continue
		}
	}

	type kv struct {
		Key   string
		Value int
	}

	ss := make([]kv, 0, len(fNameTimes))

	for k, v := range fNameTimes {
		ss = append(ss, kv{k, v})
	}

	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})

	return filepath.Join(backupPath, ss[0].Key), nil
}

func getBundleRefs(bundlePath string) (gitRefs, error) {
	bundleRefsCmd := exec.Command("git", "bundle", "list-heads", bundlePath)

	out, bundleRefsCmdErr := bundleRefsCmd.CombinedOutput()
	if bundleRefsCmdErr != nil {
		gitErr := parseGitError(out)
		if gitErr != "" {
			return nil, errors.Errorf("git bundle list-heads failed: %s", gitErr)
		}

		return nil, errors.Wrap(bundleRefsCmdErr, "git bundle list-heads failed")
	}

	refs := generateMapFromRefsCmdOutput(out)

	return refs, nil
}

func dirHasBundles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		name := entry.Name()
		// Check for both regular and encrypted bundles
		if strings.HasSuffix(name, bundleExtension) ||
			strings.HasSuffix(name, bundleExtension+encryptedBundleExtension) {
			return true
		}
	}

	return false
}

// lfsArchiveExistsForLatestBundle checks if an LFS archive exists for the latest bundle
func lfsArchiveExistsForLatestBundle(backupPath, repoName string) (bool, error) {
	// Get the latest bundle path to extract its timestamp
	latestBundlePath, err := getLatestBundlePath(backupPath)
	if err != nil {
		return false, fmt.Errorf("failed to get latest bundle path: %w", err)
	}

	// Extract timestamp from bundle filename
	bundleBasename := filepath.Base(latestBundlePath)

	timestamp, err := getTimeStampPartFromFileName(bundleBasename)
	if err != nil {
		return false, fmt.Errorf("failed to extract timestamp from bundle name: %w", err)
	}

	// Construct expected LFS archive filename
	timestampStr := fmt.Sprintf("%014d", timestamp)
	expectedLFSArchive := repoName + "." + timestampStr + lfsArchiveExtension
	expectedLFSPath := filepath.Join(backupPath, expectedLFSArchive)

	// Check if LFS archive exists
	_, err = os.Stat(expectedLFSPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check LFS archive existence: %w", err)
	}

	return true, nil
}

func getLatestBundleRefs(backupPath, encryptionPassphrase string) (gitRefs, error) {
	// if we encounter an invalid bundle, then we need to repeat until we find a valid one or run out
	for {
		path, err := getLatestBundlePath(backupPath)
		if err != nil {
			return nil, err
		}

		// Check if this is an encrypted bundle
		if isEncryptedBundle(path) {
			// For encrypted bundles, try to read refs from manifest if passphrase is available
			if encryptionPassphrase == "" {
				// No passphrase provided but bundle is encrypted - force creation of unencrypted bundle
				return nil, fmt.Errorf("encrypted bundle found but no passphrase provided - will create unencrypted bundle")
			}

			// Try to read refs from encrypted manifest
			manifest, manifestErr := readBundleManifestWithPassphrase(path, encryptionPassphrase)
			if manifestErr == nil && manifest != nil && len(manifest.GitRefs) > 0 {
				// Successfully read refs from encrypted manifest
				return manifest.GitRefs, nil
			}

			// If manifest reading fails, fall back to decrypting bundle and reading refs directly
			logger.Printf("could not read refs from encrypted manifest for %s, will decrypt bundle temporarily", path)

			// Create temporary file for decryption
			tempFile, tempErr := os.CreateTemp("", "bundle-decrypt-*.bundle")
			if tempErr != nil {
				return nil, fmt.Errorf("failed to create temp file for bundle decryption: %w", tempErr)
			}
			tempPath := tempFile.Name()
			tempFile.Close()
			defer os.Remove(tempPath)

			// Decrypt the bundle temporarily
			if decryptErr := decryptFile(path, tempPath, encryptionPassphrase); decryptErr != nil {
				return nil, fmt.Errorf("failed to decrypt bundle for ref reading: %w", decryptErr)
			}

			// Read refs from decrypted bundle
			if refs, refsErr := getBundleRefs(tempPath); refsErr == nil {
				return refs, nil
			} else {
				// Check if it's an invalid bundle
				if strings.Contains(refsErr.Error(), invalidBundleStringCheck) {
					// rename the invalid bundle
					logger.Printf("renaming invalid encrypted bundle to %s.invalid", path)

					if err = os.Rename(path, path+".invalid"); err != nil {
						// failed to rename, meaning a filesystem or permissions issue
						return nil, fmt.Errorf("failed to rename invalid bundle %w", err)
					}

					// invalid bundle rename, so continue to check for the next latest bundle
					continue
				}
				return nil, refsErr
			}
		} else {
			// Unencrypted bundle - use existing logic
			var refs gitRefs

			if refs, err = getBundleRefs(path); err != nil {
				// failed to get refs
				if strings.Contains(err.Error(), invalidBundleStringCheck) {
					// rename the invalid bundle
					logger.Printf("renaming invalid bundle to %s.invalid", path)

					if err = os.Rename(path, path+".invalid"); err != nil {
						// failed to rename, meaning a filesystem or permissions issue
						return nil, fmt.Errorf("failed to rename invalid bundle %w", err)
					}

					// invalid bundle rename, so continue to check for the next latest bundle
					continue
				}
				return nil, err
			}

			// otherwise return the refs
			return refs, nil
		}
	}
}

func createBundle(logLevel int, workingPath string, repo repository, encryptionPassphrase string) errors.E {
	objectsPath := filepath.Join(workingPath, "objects")

	dirs, readErr := os.ReadDir(objectsPath)
	if readErr != nil {
		return errors.Errorf("failed to read objectsPath: %s: %s", objectsPath, readErr)
	}

	emptyClone, err := isEmpty(workingPath)
	if err != nil {
		return errors.Errorf("failed to check if clone is empty: %s", err)
	}

	if len(dirs) == 2 && emptyClone {
		return errors.Errorf("%s is empty", repo.PathWithNameSpace)
	}

	timestamp := getTimestamp()
	backupFile := repo.Name + "." + timestamp + bundleExtension
	// Create bundle in working directory first
	workingBundlePath := filepath.Join(workingPath, backupFile)

	logger.Printf("creating bundle for: %s", repo.Name)

	bundleCmd := exec.Command("git", "bundle", "create", workingBundlePath, "--all")
	bundleCmd.Dir = workingPath

	var bundleOut bytes.Buffer

	bundleCmd.Stdout = &bundleOut
	bundleCmd.Stderr = &bundleOut

	startBundle := time.Now()

	if bundleErr := bundleCmd.Run(); bundleErr != nil {
		return errors.Errorf("failed to create bundle: %s: %s", repo.Name, bundleErr)
	}

	if logLevel > 0 {
		logger.Printf("git bundle create time for %s %s: %s", repo.Domain, repo.Name, time.Since(startBundle).String())
	}

	// Encrypt the bundle if a passphrase is provided
	if encryptionPassphrase != "" {
		// Create manifest file in working directory (only for encrypted bundles)
		if manifestErr := createBundleManifest(workingBundlePath, workingPath, timestamp); manifestErr != nil {
			logger.Printf("warning: failed to create manifest for bundle %s: %s", backupFile, manifestErr)
			// Don't fail the bundle creation if manifest fails
		}
		encryptedBundlePath := workingBundlePath + encryptedBundleExtension
		logger.Printf("encrypting bundle: %s", backupFile)

		if err := encryptFile(workingBundlePath, encryptedBundlePath, encryptionPassphrase); err != nil {
			return errors.Errorf("failed to encrypt bundle: %s", err)
		}

		// Remove the unencrypted bundle after successful encryption
		if err := os.Remove(workingBundlePath); err != nil {
			logger.Printf("warning: failed to remove unencrypted bundle: %s", err)
			// Don't fail - we have the encrypted version
		}

		// Also encrypt the manifest if it exists
		manifestPath := strings.TrimSuffix(workingBundlePath, bundleExtension) + manifestExtension
		if _, err := os.Stat(manifestPath); err == nil {
			encryptedManifestPath := manifestPath + encryptedBundleExtension
			if err := encryptFile(manifestPath, encryptedManifestPath, encryptionPassphrase); err != nil {
				logger.Printf("warning: failed to encrypt manifest: %s", err)
				// Don't fail the bundle creation if manifest encryption fails
			} else {
				// Remove unencrypted manifest after successful encryption
				if err := os.Remove(manifestPath); err != nil {
					logger.Printf("warning: failed to remove unencrypted manifest: %s", err)
				}
			}
		}
	}

	return nil
}

func createLFSArchive(logLevel int, workingPath, backupPath string, repo repository) errors.E {
	timestamp := getTimestamp()
	archiveFile := repo.Name + "." + timestamp + lfsArchiveExtension
	archiveFilePath := filepath.Join(backupPath, archiveFile)

	createErr := createDirIfAbsent(backupPath)
	if createErr != nil {
		return errors.Errorf("failed to create backup path: %s: %s", backupPath, createErr)
	}

	logger.Printf("creating git lfs archive for: %s", repo.Name)

	tarCmd := exec.Command("tar", "-czf", archiveFilePath, "lfs")
	tarCmd.Dir = workingPath

	var tarOut bytes.Buffer
	tarCmd.Stdout = &tarOut
	tarCmd.Stderr = &tarOut

	startTar := time.Now()

	if tarErr := tarCmd.Run(); tarErr != nil {
		tarErr = fmt.Errorf("repo name: %s: %s: %w", repo.Name, strings.TrimSpace(tarOut.String()), tarErr)

		return errors.Errorf("failed to create git lfs archive: %s: %s", repo.Name, tarErr)
	}

	if logLevel > 0 {
		logger.Printf("git lfs archive create time for %s %s: %s", repo.Domain, repo.Name, time.Since(startTar).String())
	}

	// Create manifest file for LFS archive
	if manifestErr := createLFSManifest(archiveFilePath, timestamp); manifestErr != nil {
		logger.Printf("warning: failed to create manifest for LFS archive %s: %s", archiveFile, manifestErr)
		// Don't fail the archive creation if manifest fails
	}

	return nil
}

func createLFSArchiveWithTimestamp(logLevel int, workingPath, backupPath string, repo repository, timestamp string) errors.E {
	archiveFile := repo.Name + "." + timestamp + lfsArchiveExtension
	archiveFilePath := filepath.Join(backupPath, archiveFile)

	createErr := createDirIfAbsent(backupPath)
	if createErr != nil {
		return errors.Errorf("failed to create backup path: %s: %s", backupPath, createErr)
	}

	logger.Printf("creating git lfs archive for: %s", repo.Name)

	tarCmd := exec.Command("tar", "-czf", archiveFilePath, "lfs")
	tarCmd.Dir = workingPath

	var tarOut bytes.Buffer
	tarCmd.Stdout = &tarOut
	tarCmd.Stderr = &tarOut

	startTar := time.Now()

	if tarErr := tarCmd.Run(); tarErr != nil {
		tarErr = fmt.Errorf("repo name: %s: %s: %w", repo.Name, strings.TrimSpace(tarOut.String()), tarErr)

		return errors.Errorf("failed to create git lfs archive: %s: %s", repo.Name, tarErr)
	}

	if logLevel > 0 {
		logger.Printf("git lfs archive create time for %s %s: %s", repo.Domain, repo.Name, time.Since(startTar).String())
	}

	// Create manifest file for LFS archive
	if manifestErr := createLFSManifest(archiveFilePath, timestamp); manifestErr != nil {
		logger.Printf("warning: failed to create manifest for LFS archive %s: %s", archiveFile, manifestErr)
		// Don't fail the archive creation if manifest fails
	}

	return nil
}

func getBundleFiles(backupPath string) (bundleFiles, error) {
	files, err := os.ReadDir(backupPath)
	if err != nil {
		return nil, errors.Wrap(err, "backup path read failed")
	}

	var bfs bundleFiles

	for _, f := range files {
		name := f.Name()
		// Check for both regular and encrypted bundles
		isBundleFile := strings.HasSuffix(name, bundleExtension)
		isEncryptedBundleFile := strings.HasSuffix(name, bundleExtension+encryptedBundleExtension)

		if !isBundleFile && !isEncryptedBundleFile {
			continue
		}

		var ts time.Time

		// For encrypted bundles, we need to get the timestamp from the original bundle name
		bundleName := name
		if isEncryptedBundleFile {
			bundleName = getOriginalBundleName(name)
		}

		ts, err = timeStampFromBundleName(bundleName)
		if err != nil {
			return nil, err
		}

		var info os.FileInfo

		info, err = f.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to get info for file %s: %w", f.Name(), err)
		}

		bfs = append(bfs, bundleFile{
			info:    info,
			created: ts,
		})
	}

	sort.Sort(bfs)

	return bfs, nil
}

func pruneBackups(backupPath string, keep int) errors.E {
	files, readErr := os.ReadDir(backupPath)
	if readErr != nil {
		return errors.Wrap(readErr, "backup path read failed")
	}

	if len(files) > 0 {
		logger.Printf("pruning %s to keep %d newest only", backupPath, keep)
	}

	var bfs bundleFiles

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), bundleExtension) {
			// no need to mention skipping lfs archives, as they are not bundles
			if !strings.HasSuffix(f.Name(), lfsArchiveExtension) && !strings.HasSuffix(f.Name(), manifestExtension) {
				logger.Printf("skipping non bundle, non lfs, and non-manifest archive file '%s'", f.Name())
			}

			continue
		}

		var ts time.Time

		ts, err := timeStampFromBundleName(f.Name())
		if err != nil {
			return err
		}

		var info os.FileInfo

		info, infoErr := f.Info()
		if infoErr != nil {
			return errors.Wrap(infoErr, "failed to get file info")
		}

		bfs = append(bfs, bundleFile{
			info:    info,
			created: ts,
		})
	}

	sort.Sort(bfs)

	firstFilesToDelete := len(bfs) - keep

	for x, f := range files {
		if x < firstFilesToDelete {
			if removeErr := os.Remove(filepath.Join(backupPath, f.Name())); removeErr != nil {
				return errors.Wrap(removeErr, "failed to remove file")
			}

			continue
		}

		break
	}

	return nil
}

type bundleFile struct {
	info    os.FileInfo
	created time.Time
}

type bundleFiles []bundleFile

func (b bundleFiles) Len() int {
	return len(b)
}

func (b bundleFiles) Less(i, j int) bool {
	return b[i].created.Before(b[j].created)
}

func (b bundleFiles) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func timeStampFromBundleName(i string) (time.Time, errors.E) {
	tokens := strings.Split(i, ".")
	if len(tokens) < minBundleFileNameTokens {
		return time.Time{}, errors.New("invalid bundle name")
	}

	sTime := tokens[len(tokens)-2]
	if len(sTime) != bundleTimestampChars {
		return time.Time{}, errors.Errorf("bundle '%s' has an invalid timestamp", i)
	}

	return timeStampToTime(sTime)
}

func getTimeStampPartFromFileName(name string) (int, error) {
	// Handle encrypted bundles by removing .age extension first
	originalName := name
	if isEncryptedBundle(name) {
		originalName = getOriginalBundleName(name)
	}

	if strings.Count(originalName, ".") >= minBundleFileNameTokens-1 {
		parts := strings.Split(originalName, ".")

		strTimestamp := parts[len(parts)-2]

		l, err := strconv.Atoi(strTimestamp)
		if err != nil {
			return 0, fmt.Errorf("invalid timestamp '%s': %w", name, err)
		}

		return l, nil
	}

	return 0, fmt.Errorf("filename '%s' does not match bundle format <repo-name>.<timestamp>.bundle",
		name)
}

func filesIdentical(path1, path2 string) bool {
	// First check if file sizes are same
	latestBundleSize := getFileSize(path1)
	previousBundleSize := getFileSize(path2)

	// If sizes are different, files are definitely not identical
	if latestBundleSize != previousBundleSize {
		return false
	}

	// Try to use manifests for comparison if these are encrypted bundle files
	// (manifests are only created for encrypted bundles)
	if strings.HasSuffix(path1, bundleExtension+encryptedBundleExtension) &&
	   strings.HasSuffix(path2, bundleExtension+encryptedBundleExtension) {
		manifest1, _ := readBundleManifest(path1)
		manifest2, _ := readBundleManifest(path2)

		// If both manifests exist and have hashes, use them for comparison
		if manifest1 != nil && manifest2 != nil &&
			manifest1.BundleHash != "" && manifest2.BundleHash != "" {
			return manifest1.BundleHash == manifest2.BundleHash
		}
	}

	// Fall back to computing hashes directly
	latestBundleHash, latestHashErr := getSHA2Hash(path1)
	if latestHashErr != nil {
		logger.Printf("failed to get sha2 hash for: %s", path1)
		return false
	}

	previousBundleHash, previousHashErr := getSHA2Hash(path2)
	if previousHashErr != nil {
		logger.Printf("failed to get sha2 hash for: %s", path2)
		return false
	}

	return reflect.DeepEqual(latestBundleHash, previousBundleHash)
}

// checkBundleIsDuplicate checks if the bundle in workingPath is identical to the latest bundle in backupPath
// Returns the bundle filename from workingPath, whether it's a duplicate, and whether to replace existing with encrypted
func checkBundleIsDuplicate(workingPath, backupPath, encryptionPassphrase string) (string, bool, bool, error) {
	// Find the bundle file in working directory (could be encrypted or not)
	workingFiles, err := os.ReadDir(workingPath)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to read working directory: %w", err)
	}

	var workingBundleFile string
	var workingIsEncrypted bool
	for _, f := range workingFiles {
		name := f.Name()
		if strings.HasSuffix(name, bundleExtension+encryptedBundleExtension) {
			workingBundleFile = name
			workingIsEncrypted = true
			break
		} else if strings.HasSuffix(name, bundleExtension) {
			workingBundleFile = name
			workingIsEncrypted = false
			// Don't break - prefer encrypted if both exist
		}
	}

	if workingBundleFile == "" {
		return "", false, false, errors.New("no bundle file found in working directory")
	}

	workingBundlePath := filepath.Join(workingPath, workingBundleFile)

	// Check if backup directory exists and has bundles
	if !dirHasBundles(backupPath) {
		// No existing bundles, so this is not a duplicate
		return workingBundleFile, false, false, nil
	}

	// Get the latest bundle in backup directory
	latestBackupPath, err := getLatestBundlePath(backupPath)
	if err != nil {
		// If we can't find a latest bundle, assume it's not a duplicate
		return workingBundleFile, false, false, nil
	}

	backupIsEncrypted := isEncryptedBundle(latestBackupPath)

	// Determine if bundles are identical
	var isDuplicate bool
	var shouldReplace bool

	// Case 1: Both encrypted - try manifest comparison first, then file comparison
	if workingIsEncrypted && backupIsEncrypted {
		// Try to use manifest files for comparison if they exist
		workingManifest, _ := readBundleManifestWithPassphrase(workingBundlePath, encryptionPassphrase)
		backupManifest, _ := readBundleManifestWithPassphrase(latestBackupPath, encryptionPassphrase)

		if workingManifest != nil && backupManifest != nil &&
			workingManifest.BundleHash != "" && backupManifest.BundleHash != "" {
			isDuplicate = workingManifest.BundleHash == backupManifest.BundleHash
		} else {
			// Fall back to file comparison if manifests are not available
			isDuplicate = filesIdentical(workingBundlePath, latestBackupPath)
		}
		shouldReplace = false
	} else if workingIsEncrypted && !backupIsEncrypted {
		// Case 2: Working is encrypted, backup is not encrypted
		// Need to decrypt working bundle to compare
		if encryptionPassphrase != "" {
			identical, err := compareEncryptedWithPlain(workingBundlePath, latestBackupPath, encryptionPassphrase)
			if err != nil {
				logger.Printf("warning: failed to compare encrypted with plain bundle: %s", err)
				isDuplicate = false
			} else {
				isDuplicate = identical
			}
			// If identical, we should replace the unencrypted with encrypted
			shouldReplace = isDuplicate
		} else {
			// Can't decrypt to compare, assume not duplicate
			isDuplicate = false
			shouldReplace = false
		}
	} else if !workingIsEncrypted && backupIsEncrypted {
		// Case 3: Working is not encrypted, backup is encrypted
		// This shouldn't happen in normal flow (we encrypted in createBundle)
		// but handle it anyway - can't compare without passphrase
		isDuplicate = false
		shouldReplace = false
	} else {
		// Case 4: Both unencrypted - direct file comparison
		// No manifests are created for unencrypted bundles
		isDuplicate = filesIdentical(workingBundlePath, latestBackupPath)
		shouldReplace = false
	}

	if isDuplicate {
		if shouldReplace {
			logger.Printf("bundle content unchanged but will replace unencrypted with encrypted version: %s",
				filepath.Base(latestBackupPath))
		} else {
			logger.Printf("no change since previous bundle: %s", filepath.Base(latestBackupPath))
		}
	}

	return workingBundleFile, isDuplicate, shouldReplace, nil
}

func removeBundleIfDuplicate(dir string) bool {
	files, err := getBundleFiles(dir)
	if err != nil {
		logger.Println(err)

		return false
	}

	if len(files) == 1 {
		return false
	}
	// get timestamps in filenames for sorting
	fNameTimes := map[string]int{}

	for _, f := range files {
		var ts int
		if ts, err = getTimeStampPartFromFileName(f.info.Name()); err == nil {
			fNameTimes[f.info.Name()] = ts
		}
	}

	type kv struct {
		Key   string
		Value int
	}

	ss := make([]kv, 0, len(fNameTimes))

	for k, v := range fNameTimes {
		ss = append(ss, kv{k, v})
	}

	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})

	latestBundleFilePath := filepath.Join(dir, ss[0].Key)
	previousBundleFilePath := filepath.Join(dir, ss[1].Key)

	if filesIdentical(latestBundleFilePath, previousBundleFilePath) {
		logger.Printf("no change since previous bundle: %s", ss[1].Key)
		logger.Printf("deleting duplicate bundle: %s", ss[0].Key)

		if deleteFile(filepath.Join(dir, ss[0].Key)) != nil {
			logger.Println("failed to remove duplicate bundle")
		}

		return false
	}

	return true
}

func deleteFile(path string) error {
	if err := os.Remove(path); err != nil {
		return errors.Wrap(err, "failed to remove file")
	}

	return nil
}

func getSHA2Hash(filePath string) ([]byte, error) {
	var result []byte

	file, err := os.Open(filePath)
	if err != nil {
		return result, errors.Wrap(err, "failed to open file")
	}

	defer func() {
		if err = file.Close(); err != nil {
			logger.Printf("warn: failed to close: %s", filePath)
		}
	}()

	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return result, errors.Wrap(err, "failed to get hash")
	}

	return hash.Sum(result), nil
}

func getFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		logger.Println(err)

		return 0
	}

	return fi.Size()
}

// BundleManifest represents the metadata for a bundle
type BundleManifest struct {
	CreationTime string            `json:"creation_time"`
	BundleHash   string            `json:"bundle_hash"`
	BundleFile   string            `json:"bundle_file"`
	GitRefs      map[string]string `json:"git_refs"`
}

// readBundleManifest reads a bundle manifest file and returns the manifest data
func readBundleManifest(bundlePath string) (*BundleManifest, error) {
	var manifestPath string

	// Handle encrypted bundles
	if isEncryptedBundle(bundlePath) {
		// For encrypted bundles, the manifest is also encrypted
		// e.g., test-repo.20250920100845.bundle.age -> test-repo.20250920100845.manifest.age
		originalBundlePath := getOriginalBundleName(bundlePath)
		manifestPath = strings.TrimSuffix(originalBundlePath, bundleExtension) + manifestExtension + encryptedBundleExtension
	} else {
		// For regular bundles
		manifestPath = strings.TrimSuffix(bundlePath, bundleExtension) + manifestExtension
	}

	// Check if manifest file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, nil // No manifest file exists
	}

	var manifestData []byte
	var err error

	// If it's an encrypted manifest, we need the passphrase to decrypt it
	if strings.HasSuffix(manifestPath, encryptedBundleExtension) {
		// For encrypted manifests, we can't read them without the passphrase
		// This function doesn't have access to the passphrase, so return nil
		// The caller should handle encrypted manifests separately if needed
		return nil, nil
	}

	// Read the manifest file
	manifestData, err = os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	// Unmarshal the JSON
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return &manifest, nil
}

// readBundleManifestWithPassphrase reads a bundle manifest file, decrypting if necessary
func readBundleManifestWithPassphrase(bundlePath, passphrase string) (*BundleManifest, error) {
	var manifestPath string

	// Handle encrypted bundles
	if isEncryptedBundle(bundlePath) {
		// For encrypted bundles, the manifest is also encrypted
		originalBundlePath := getOriginalBundleName(bundlePath)
		manifestPath = strings.TrimSuffix(originalBundlePath, bundleExtension) + manifestExtension + encryptedBundleExtension
	} else {
		// For regular bundles
		manifestPath = strings.TrimSuffix(bundlePath, bundleExtension) + manifestExtension
	}

	// Check if manifest file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, nil // No manifest file exists
	}

	var manifestData []byte
	var err error

	// If it's an encrypted manifest, decrypt it first
	if strings.HasSuffix(manifestPath, encryptedBundleExtension) {
		if passphrase == "" {
			return nil, nil // Can't decrypt without passphrase
		}

		// Create temporary file for decrypted manifest
		tempFile, err := os.CreateTemp("", "decrypted-manifest-*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		tempPath := tempFile.Name()
		tempFile.Close()
		defer os.Remove(tempPath)

		// Decrypt the manifest
		if err := decryptFile(manifestPath, tempPath, passphrase); err != nil {
			return nil, fmt.Errorf("failed to decrypt manifest: %w", err)
		}

		// Read the decrypted manifest
		manifestData, err = os.ReadFile(tempPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read decrypted manifest: %w", err)
		}
	} else {
		// Read the manifest file directly
		manifestData, err = os.ReadFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read manifest file: %w", err)
		}
	}

	// Unmarshal the JSON
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return &manifest, nil
}

// createBundleManifest creates a manifest file for the bundle with metadata
func createBundleManifest(bundlePath, workingPath, timestamp string) error {
	// Get the hash of the bundle file
	hashBytes, err := getSHA2Hash(bundlePath)
	if err != nil {
		return fmt.Errorf("failed to get bundle hash: %w", err)
	}
	hashStr := hex.EncodeToString(hashBytes)

	// Get git refs from the bundle
	refs, err := getBundleRefs(bundlePath)
	if err != nil {
		return fmt.Errorf("failed to get bundle refs: %w", err)
	}

	// Parse timestamp to get creation time
	creationTime, err := timeStampToTime(timestamp)
	if err != nil {
		return fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Create manifest struct
	manifest := BundleManifest{
		CreationTime: creationTime.Format(time.RFC3339),
		BundleHash:   hashStr,
		BundleFile:   filepath.Base(bundlePath),
		GitRefs:      refs,
	}

	// Marshal to JSON
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Write manifest file
	manifestPath := strings.TrimSuffix(bundlePath, bundleExtension) + ".manifest"
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	logger.Printf("created manifest: %s", filepath.Base(manifestPath))
	return nil
}

// LFSManifest represents the metadata for an LFS archive
type LFSManifest struct {
	CreationTime string `json:"creation_time"`
	ArchiveHash  string `json:"archive_hash"`
	ArchiveFile  string `json:"archive_file"`
}

// createLFSManifest creates a manifest file for the LFS archive with metadata
func createLFSManifest(archivePath, timestamp string) error {
	// Get the hash of the archive file
	hashBytes, err := getSHA2Hash(archivePath)
	if err != nil {
		return fmt.Errorf("failed to get archive hash: %w", err)
	}
	hashStr := hex.EncodeToString(hashBytes)

	// Parse timestamp to get creation time
	creationTime, err := timeStampToTime(timestamp)
	if err != nil {
		return fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Create manifest struct
	manifest := LFSManifest{
		CreationTime: creationTime.Format(time.RFC3339),
		ArchiveHash:  hashStr,
		ArchiveFile:  filepath.Base(archivePath),
	}

	// Marshal to JSON
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Write manifest file
	manifestPath := strings.TrimSuffix(archivePath, lfsArchiveExtension) + ".manifest"
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	logger.Printf("created LFS manifest: %s", filepath.Base(manifestPath))
	return nil
}
