package githosts

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gitlab.com/tozd/go/errors"
)

const (
	envVarGitBackupDir              = "GIT_BACKUP_DIR"
	envVarGitHostsLog               = "GITHOSTS_LOG"
	refsMethod                      = "refs"
	cloneMethod                     = "clone"
	defaultRemoteMethod             = cloneMethod
	logEntryPrefix                  = "githosts-utils: "
	statusOk                        = "ok"
	statusFailed                    = "failed"
	msgUsingDiffRemoteMethod        = "using diff remote method"
	msgUsingDefaultDiffRemoteMethod = "using default diff remote method"
	msgBackupSkippedNoDir           = "backup skipped as backup directory not specified"
	msgBackupDirNotSpecified        = "backup directory not specified"
	defaultRetryWait                = 60
)

type repository struct {
	Name              string
	Owner             string
	PathWithNameSpace string
	Domain            string
	HTTPSUrl          string
	SSHUrl            string
	URLWithToken      string
	URLWithBasicAuth  string
	BasicAuthUser     string
	BasicAuthPass     string
}

type describeReposOutput struct {
	Repos []repository
}

type RepoBackupResults struct {
	Repo   string   `json:"repo,omitempty"`
	Status string   `json:"status,omitempty"` // ok, failed
	Error  errors.E `json:"error,omitempty"`
}

type ProviderBackupResult struct {
	BackupResults []RepoBackupResults
	Error         errors.E
}

func repoBackupResult(repo repository, err errors.E) RepoBackupResults {
	result := RepoBackupResults{Repo: repo.PathWithNameSpace, Status: statusOk}
	if err != nil {
		result.Status = statusFailed
		result.Error = err
	}

	return result
}

type BasicAuth struct {
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

type gitProvider interface {
	getAPIURL() string
	describeRepos() (describeReposOutput, errors.E)
	Backup() ProviderBackupResult
	diffRemoteMethod() string
}

// gitRefs is a mapping of references to SHAs.
type gitRefs map[string]string

func remoteRefsMatchLocalRefs(cloneURL, backupPath, encryptionPassphrase string) bool {
	// if there's no backup path then return false
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return false
	}

	// if there are no backups
	if !dirHasBundles(backupPath) {
		return false
	}

	var rHeads, lHeads gitRefs

	var err error

	lHeads, err = getLatestBundleRefs(backupPath, encryptionPassphrase)
	if err != nil {
		logger.Printf("failed to get latest bundle refs for %s", backupPath)

		return false
	}

	rHeads, err = getRemoteRefs(cloneURL)
	if err != nil {
		logger.Printf("failed to get remote refs")

		return false
	}

	if reflect.DeepEqual(lHeads, rHeads) {
		return true
	}

	return false
}

func cutBySpaceAndTrimOutput(in string) (before, after string, found bool) {
	// remove leading and trailing space
	in = strings.TrimSpace(in)
	// try cutting by tab
	b, a, f := strings.Cut(in, "\t")
	if f {
		b = strings.TrimSpace(b)
		a = strings.TrimSpace(a)

		if len(a) > 0 && len(b) > 0 {
			return b, a, true
		}
	}

	// try cutting by tab
	b, a, f = strings.Cut(in, " ")
	if f {
		b = strings.TrimSpace(b)
		a = strings.TrimSpace(a)

		if len(a) > 0 && len(b) > 0 {
			return b, a, true
		}
	}

	return
}

func generateMapFromRefsCmdOutput(in []byte) (refs gitRefs) {
	refs = make(map[string]string)
	lines := strings.Split(string(in), "\n")

	for x := range lines {
		// if empty (final line perhaps) then skip
		if len(strings.TrimSpace(lines[x])) == 0 {
			continue
		}

		// try cutting ref by both space and tab as its possible for both to be used
		sha, ref, found := cutBySpaceAndTrimOutput(lines[x])

		// expect only a sha and a ref
		if !found {
			logger.Printf("skipping invalid ref: %s", strings.TrimSpace(lines[x]))

			continue
		}

		// git bundle list-heads returns pseudo-refs but not peeled tags
		// this is required for comparison with remote references
		if slices.Contains([]string{"HEAD", "FETCH_HEAD", "ORIG_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD"}, ref) {
			continue
		}

		refs[ref] = sha
	}

	return
}

func getRemoteRefs(cloneURL string) (refs gitRefs, err error) {
	// --refs ignores pseudo-refs like HEAD and FETCH_HEAD, and also peeled tags that reference other objects
	// this enables comparison with refs from existing bundles
	remoteHeadsCmd := exec.Command("git", "ls-remote", "--refs", cloneURL)

	out, err := remoteHeadsCmd.CombinedOutput()
	if err != nil {
		gitErr := parseGitError(out)
		if gitErr != "" {
			return refs, errors.Wrapf(err, "failed to retrieve remote heads: %s", gitErr)
		}

		return refs, errors.Wrap(err, "failed to retrieve remote heads")
	}

	refs = generateMapFromRefsCmdOutput(out)

	return
}

func getCloneURL(repo repository) string {
	if repo.URLWithToken != "" {
		return repo.URLWithToken
	}

	if repo.URLWithBasicAuth != "" {
		return repo.URLWithBasicAuth
	}

	if repo.BasicAuthUser != "" && repo.BasicAuthPass != "" {
		return fmt.Sprintf("https://%s:%s@%s", bitbucketStaticUserName, repo.BasicAuthPass, repo.HTTPSUrl)
	}

	if repo.SSHUrl != "" {
		return repo.SSHUrl
	}

	return repo.HTTPSUrl
}

type processBackupInput struct {
	LogLevel             int
	Repo                 repository
	BackupDIR            string
	BackupsToKeep        int
	DiffRemoteMethod     string
	BackupLFS            bool
	Secrets              []string
	EncryptionPassphrase string // Optional passphrase for age encryption
}

func processBackup(in processBackupInput) errors.E {
	workingPath, backupPath, err := setupBackupPaths(in.Repo, in.BackupDIR)
	if err != nil {
		return err
	}

	cloneURL := getCloneURL(in.Repo)

	if shouldSkipBackup(in.DiffRemoteMethod, cloneURL, backupPath, in.Repo, in.EncryptionPassphrase) {
		return nil
	}

	if err = cloneRepository(cloneRepositoryInput{
		Repo:        in.Repo,
		CloneURL:    cloneURL,
		WorkingPath: workingPath,
		BackupDIR:   in.BackupDIR,
		LogLevel:    in.LogLevel,
		Secrets:     in.Secrets,
	}); err != nil {
		return err
	}

	if err = createBundle(in.LogLevel, workingPath, in.Repo, in.EncryptionPassphrase); err != nil {
		if strings.HasSuffix(err.Error(), "is empty") {
			logger.Printf("skipping empty %s repository %s", in.Repo.Domain, in.Repo.PathWithNameSpace)

			return nil
		}

		return err
	}

	// Check if the bundle is a duplicate before moving
	bundleFileName, isDuplicate, shouldReplace, checkErr := checkBundleIsDuplicate(workingPath, backupPath, in.EncryptionPassphrase)
	if checkErr != nil {
		return errors.Errorf("failed to check for duplicate bundle: %s", checkErr)
	}

	isUpdated := true
	if isDuplicate && !shouldReplace {
		// Bundle is a duplicate and doesn't need replacement, don't move it
		logger.Printf("bundle is duplicate, not moving to backup directory")
		isUpdated = false
	} else {
		// Bundle is not a duplicate OR needs to replace existing (encrypted replacing unencrypted)
		createErr := createDirIfAbsent(backupPath)
		if createErr != nil {
			return errors.Errorf("failed to create backup path: %s: %s", backupPath, createErr)
		}

		workingBundlePath := filepath.Join(workingPath, bundleFileName)
		backupBundlePath := filepath.Join(backupPath, bundleFileName)

		// If replacing, remove the old unencrypted bundle first
		if shouldReplace {
			// Find and remove the old unencrypted bundle
			oldBundlePath, err := getLatestBundlePath(backupPath)
			if err == nil && !isEncryptedBundle(oldBundlePath) {
				logger.Printf("removing unencrypted bundle to replace with encrypted: %s", filepath.Base(oldBundlePath))
				if removeErr := os.Remove(oldBundlePath); removeErr != nil {
					logger.Printf("warning: failed to remove old unencrypted bundle: %s", removeErr)
				}
				// Also remove old manifest if it exists
				oldManifestPath := strings.TrimSuffix(oldBundlePath, bundleExtension) + manifestExtension
				if _, err := os.Stat(oldManifestPath); err == nil {
					if removeErr := os.Remove(oldManifestPath); removeErr != nil {
						logger.Printf("warning: failed to remove old manifest: %s", removeErr)
					}
				}
			}
		}

		if moveErr := os.Rename(workingBundlePath, backupBundlePath); moveErr != nil {
			return errors.Errorf("failed to move bundle to backup directory: %s", moveErr)
		}

		// Handle manifest files - they might be encrypted too
		baseWorkingName := getOriginalBundleName(bundleFileName)
		workingManifestPath := strings.TrimSuffix(filepath.Join(workingPath, baseWorkingName), bundleExtension) + manifestExtension
		backupManifestPath := strings.TrimSuffix(filepath.Join(backupPath, baseWorkingName), bundleExtension) + manifestExtension

		// Check for encrypted manifest first
		if isEncryptedBundle(bundleFileName) {
			workingManifestPath = workingManifestPath + encryptedBundleExtension
			backupManifestPath = backupManifestPath + encryptedBundleExtension
		}

		// Check if manifest exists and move it (don't fail if it doesn't exist)
		if _, err := os.Stat(workingManifestPath); err == nil {
			if moveErr := os.Rename(workingManifestPath, backupManifestPath); moveErr != nil {
				logger.Printf("warning: failed to move manifest file: %s", moveErr)
			}
		}
	}

	if in.BackupLFS {
		if err := handleLFSBackup(in.LogLevel, workingPath, backupPath, in.Repo, isUpdated); err != nil {
			return err
		}
	}

	if in.BackupsToKeep > 0 {
		if err := pruneBackups(backupPath, in.BackupsToKeep); err != nil {
			return err
		}
	}

	return nil
}

func setupBackupPaths(repo repository, backupDIR string) (workingPath, backupPath string, err errors.E) {
	workingPath = filepath.Join(backupDIR, workingDIRName, repo.Domain, repo.PathWithNameSpace)
	backupPath = filepath.Join(backupDIR, repo.Domain, repo.PathWithNameSpace)

	delErr := os.RemoveAll(workingPath)
	if delErr != nil {
		return "", "", errors.Errorf("failed to remove working directory: %s: %s", workingPath, delErr)
	}

	return workingPath, backupPath, nil
}

func shouldSkipBackup(diffRemoteMethod, cloneURL, backupPath string, repo repository, encryptionPassphrase string) bool {
	if diffRemoteMethod == refsMethod {
		if remoteRefsMatchLocalRefs(cloneURL, backupPath, encryptionPassphrase) {
			logger.Printf("skipping clone of %s repo '%s' as refs match existing bundle", repo.Domain, repo.PathWithNameSpace)

			return true
		}
	}

	return false
}

type cloneRepositoryInput struct {
	Repo        repository
	CloneURL    string
	WorkingPath string
	BackupDIR   string
	LogLevel    int
	Secrets     []string
}

func cloneRepository(in cloneRepositoryInput) errors.E {
	logger.Printf("cloning: %s to: %s", maskURLCredentials(in.CloneURL), in.WorkingPath)

	//if in.LogLevel == 0 {
	//	logger.Printf("git clone command will use URL: %s", maskSecrets(in.CloneURL, in.Secrets))
	//}

	cloneCmd := buildCloneCommand(in.CloneURL, in.WorkingPath, in.BackupDIR)

	// Log the command being executed for debugging, with URL credentials masked
	//maskedCmd := maskGitCommand(cloneCmd.Args)
	//logger.Printf("executing git command: %s", maskedCmd)
	//logger.Printf("working directory: %s", cloneCmd.Dir)

	cloneOut, cloneErr := cloneCmd.CombinedOutput()

	if cloneErr != nil {
		return handleCloneError(in.Repo, cloneOut, cloneErr, in.CloneURL, in.Secrets)
	}

	return nil
}

func buildCloneCommand(cloneURL, workingPath, backupDIR string) *exec.Cmd {
	var cloneCmd *exec.Cmd
	if strings.Contains(cloneURL, "git.sr.ht") {
		cloneCmd = exec.Command("git",
			"-c", "http.followRedirects=false",
			"-c", "http.postBuffer=524288000",
			"-c", "http.maxRequestBuffer=100M",
			"-c", "url.https://git.sr.ht/.insteadOf=https://git.sr.ht/",
			"-c", "http.extraHeader=User-Agent: git/2.39.0",
			"clone", "-v", "--mirror", cloneURL, workingPath)
	} else {
		cloneCmd = exec.Command("git", "clone", "-v", "--mirror", cloneURL, workingPath)
	}

	cloneCmd.Dir = backupDIR

	return cloneCmd
}

func handleCloneError(repo repository, cloneOut []byte, cloneErr error, cloneURL string, secrets []string) errors.E {
	gitErr := parseGitError(cloneOut)
	cloneOutLines := strings.Split(string(cloneOut), "\n")

	logger.Printf("====== Git Clone Failed ======")
	logger.Printf("Repository: %s", repo.Name)
	logger.Printf("Repository Path: %s", repo.PathWithNameSpace)
	logger.Printf("Clone URL (masked): %s", maskSecrets(cloneURL, secrets))
	logger.Printf("Exit error: %v", cloneErr)

	// Extract exit code if available
	if exitError, ok := cloneErr.(*exec.ExitError); ok {
		logger.Printf("Exit code: %d", exitError.ExitCode())
	}

	logger.Printf("Git output (last 50 lines):")
	outputLines := strings.Split(string(cloneOut), "\n")
	startLine := 0
	if len(outputLines) > 50 {
		startLine = len(outputLines) - 50
	}
	for i := startLine; i < len(outputLines); i++ {
		if outputLines[i] != "" {
			logger.Printf("  > %s", maskSecrets(outputLines[i], secrets))
		}
	}
	logger.Printf("==============================")

	if os.Getenv(envVarGitHostsLog) == "debug" {
		fmt.Printf("debug: cloning failed for repository: %s - %s\n", repo.Name, strings.Join(cloneOutLines, ", "))
	}

	if gitErr != "" {
		return errors.Wrapf(cloneErr, "cloning failed for repository: %s - %s. Full output: %s", repo.Name, gitErr, strings.TrimSpace(string(cloneOut)))
	}

	trimmedOutput := strings.TrimSpace(string(cloneOut))
	if trimmedOutput != "" {
		return errors.Wrapf(cloneErr, "cloning failed for repository: %s. Git output: %s", repo.Name, trimmedOutput)
	}

	return errors.Wrapf(cloneErr, "cloning failed for repository: %s - exit status: %v", repo.Name, cloneErr)
}

func handleLFSBackup(logLevel int, workingPath, backupPath string, repo repository, isUpdated bool) errors.E {
	needsLFSBackup, useExistingTimestamp := determineLFSBackupNeeds(backupPath, repo, isUpdated)

	if !needsLFSBackup {
		return nil
	}

	hasLFSFiles, err := checkForLFSFiles(workingPath)
	if err != nil {
		return err
	}

	if !hasLFSFiles {
		logger.Printf("no LFS files found in %s repository %s", repo.Domain, repo.PathWithNameSpace)

		return nil
	}

	if err := fetchLFSFiles(workingPath); err != nil {
		return err
	}

	if useExistingTimestamp != "" {
		return createLFSArchiveWithTimestamp(logLevel, workingPath, backupPath, repo, useExistingTimestamp)
	}

	return createLFSArchive(logLevel, workingPath, backupPath, repo)
}

func determineLFSBackupNeeds(backupPath string, repo repository, isUpdated bool) (needsBackup bool, timestamp string) {
	if isUpdated {
		return true, ""
	}

	lfsExists, lfsErr := lfsArchiveExistsForLatestBundle(backupPath, repo.Name)
	if lfsErr != nil {
		logger.Printf("failed to check LFS archive existence for %s: %s", repo.PathWithNameSpace, lfsErr)

		return false, ""
	}

	if lfsExists {
		return false, ""
	}

	logger.Printf("LFS archive missing for latest bundle of %s repository %s, creating it", repo.Domain, repo.PathWithNameSpace)

	if latestBundlePath, err := getLatestBundlePath(backupPath); err == nil {
		bundleBasename := filepath.Base(latestBundlePath)
		if ts, err := getTimeStampPartFromFileName(bundleBasename); err == nil {
			return true, fmt.Sprintf("%014d", ts)
		}
	}

	return true, ""
}

func checkForLFSFiles(workingPath string) (bool, errors.E) {
	lfsFilesCmd := exec.Command("git", "lfs", "ls-files")
	lfsFilesCmd.Dir = workingPath
	lfsFilesOut, lfsFilesErr := lfsFilesCmd.CombinedOutput()

	if lfsFilesErr != nil {
		return false, errors.Errorf("git lfs ls-files failed: %s: %s", strings.TrimSpace(string(lfsFilesOut)), lfsFilesErr)
	}

	return len(strings.TrimSpace(string(lfsFilesOut))) > 0, nil
}

func fetchLFSFiles(workingPath string) errors.E {
	lfsCmd := exec.Command("git", "lfs", "fetch", "--all")
	lfsCmd.Dir = workingPath

	if out, err := lfsCmd.CombinedOutput(); err != nil {
		return errors.Errorf("git lfs fetch failed: %s: %s", strings.TrimSpace(string(out)), err)
	}

	return nil
}

func getHTTPClient() *retryablehttp.Client {
	tr := &http.Transport{
		DisableKeepAlives:  false,
		DisableCompression: true,
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		ForceAttemptHTTP2:  false,
	}

	rc := retryablehttp.NewClient()
	rc.HTTPClient = &http.Client{
		Transport: tr,
		Timeout:   backupTimeout,
	}

	rc.Logger = nil
	rc.RetryWaitMax = backupTimeout
	rc.RetryWaitMin = defaultRetryWait * time.Second
	rc.RetryMax = 2

	return rc
}

func validDiffRemoteMethod(method string) error {
	if !slices.Contains([]string{cloneMethod, refsMethod}, method) {
		return fmt.Errorf("invalid diff remote method: %s", method)
	}

	return nil
}

func setLoggerPrefix(prefix string) {
	if prefix != "" {
		logger.SetPrefix(fmt.Sprintf("%s: ", prefix))
	}
}

func allTrue(in ...bool) bool {
	for _, v := range in {
		if !v {
			return false
		}
	}

	return true
}

func ToPtr[T any](v T) *T {
	return &v
}

func TrimInPlace(s *string) {
	*s = strings.TrimSpace(*s)
}
