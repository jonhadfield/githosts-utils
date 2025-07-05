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

func remoteRefsMatchLocalRefs(cloneURL, backupPath string) bool {
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

	lHeads, err = getLatestBundleRefs(backupPath)
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

func generateMapFromRefsCmdOutput(in []byte) (refs gitRefs, err error) {
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

	refs, err = generateMapFromRefsCmdOutput(out)

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

func processBackup(logLevel int, repo repository, backupDIR string, backupsToKeep int, diffRemoteMethod string, backupLFS bool, secrets []string) errors.E {
	// create backup path
	workingPath := filepath.Join(backupDIR, workingDIRName, repo.Domain, repo.PathWithNameSpace)
	backupPath := filepath.Join(backupDIR, repo.Domain, repo.PathWithNameSpace)
	// clean existing working directory
	delErr := os.RemoveAll(workingPath)
	if delErr != nil {
		return errors.Errorf("failed to remove working directory: %s: %s", workingPath, delErr)
	}

	cloneURL := getCloneURL(repo)

	// Check if existing, latest bundle refs, already match the remote
	if diffRemoteMethod == refsMethod {
		// check backup path exists before attempting to compare remote and local heads
		if remoteRefsMatchLocalRefs(cloneURL, backupPath) {
			logger.Printf("skipping clone of %s repo '%s' as refs match existing bundle", repo.Domain, repo.PathWithNameSpace)

			return nil
		}
	}

	// clone repo
	logger.Printf("cloning: %s to: %s", maskSecrets(repo.HTTPSUrl, secrets), workingPath)
	if logLevel == 0 {
		logger.Printf("git clone command will use URL: %s", maskSecrets(cloneURL, secrets))
	}

	// For SourceHut, add multiple configs to handle redirect and trailing slash issues
	var cloneCmd *exec.Cmd
	if strings.Contains(cloneURL, "git.sr.ht") {
		// Multiple git configs to prevent various redirect and normalization issues
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

	cloneOut, cloneErr := cloneCmd.CombinedOutput()
	cloneOutLines := strings.Split(string(cloneOut), "\n")

	if cloneErr != nil {
		gitErr := parseGitError(cloneOut)
		
		// Always log the full git output for clone failures to help with debugging
		logger.Printf("Git clone failed for repository: %s", repo.Name)
		logger.Printf("Clone command exit code: %v", cloneErr)
		logger.Printf("Full git output: %s", string(cloneOut))

		if os.Getenv(envVarGitHostsLog) == "debug" {
			fmt.Printf("debug: cloning failed for repository: %s - %s\n", repo.Name, strings.Join(cloneOutLines, ", "))
		}

		// Improve error message to include both git error and raw output
		if gitErr != "" {
			return errors.Wrapf(cloneErr, "cloning failed for repository: %s - %s. Full output: %s", repo.Name, gitErr, strings.TrimSpace(string(cloneOut)))
		}
		
		// If no specific git error found, include the full output in the error
		trimmedOutput := strings.TrimSpace(string(cloneOut))
		if trimmedOutput != "" {
			return errors.Wrapf(cloneErr, "cloning failed for repository: %s. Git output: %s", repo.Name, trimmedOutput)
		}
		
		return errors.Wrapf(cloneErr, "cloning failed for repository: %s - exit status: %v", repo.Name, cloneErr)
	}

	// create bundle
	if err := createBundle(logLevel, workingPath, backupPath, repo); err != nil {
		if strings.HasSuffix(err.Error(), "is empty") {
			logger.Printf("skipping empty %s repository %s", repo.Domain, repo.PathWithNameSpace)

			return nil
		}

		return err
	}

	isUpdated := removeBundleIfDuplicate(backupPath)

	if backupLFS {
		// Check if we need to create an LFS backup
		needsLFSBackup := isUpdated
		var useExistingTimestamp string
		if !isUpdated {
			// Repository wasn't updated, but check if LFS archive exists for the latest bundle
			lfsExists, lfsErr := lfsArchiveExistsForLatestBundle(backupPath, repo.Name)
			if lfsErr != nil {
				logger.Printf("failed to check LFS archive existence for %s: %s", repo.PathWithNameSpace, lfsErr)
			} else if !lfsExists {
				logger.Printf("LFS archive missing for latest bundle of %s repository %s, creating it", repo.Domain, repo.PathWithNameSpace)
				needsLFSBackup = true
				// Get timestamp from the latest bundle to use for LFS archive
				if latestBundlePath, err := getLatestBundlePath(backupPath); err == nil {
					bundleBasename := filepath.Base(latestBundlePath)
					if timestamp, err := getTimeStampPartFromFileName(bundleBasename); err == nil {
						useExistingTimestamp = fmt.Sprintf("%014d", timestamp)
					}
				}
			}
		}

		if needsLFSBackup {
			lfsFilesCmd := exec.Command("git", "lfs", "ls-files")
			lfsFilesCmd.Dir = workingPath
			lfsFilesOut, lfsFilesErr := lfsFilesCmd.CombinedOutput()
			if lfsFilesErr != nil {
				return errors.Errorf("git lfs ls-files failed: %s: %s", strings.TrimSpace(string(lfsFilesOut)), lfsFilesErr)
			}
			if len(strings.TrimSpace(string(lfsFilesOut))) > 0 {
				lfsCmd := exec.Command("git", "lfs", "fetch", "--all")
				lfsCmd.Dir = workingPath
				if out, err := lfsCmd.CombinedOutput(); err != nil {
					return errors.Errorf("git lfs fetch failed: %s: %s", strings.TrimSpace(string(out)), err)
				}

				if useExistingTimestamp != "" {
					if err := createLFSArchiveWithTimestamp(logLevel, workingPath, backupPath, repo, useExistingTimestamp); err != nil {
						return err
					}
				} else {
					if err := createLFSArchive(logLevel, workingPath, backupPath, repo); err != nil {
						return err
					}
				}
			} else {
				logger.Printf("no LFS files found in %s repository %s", repo.Domain, repo.PathWithNameSpace)
			}
		}
	}

	if backupsToKeep > 0 {
		if err := pruneBackups(backupPath, backupsToKeep); err != nil {
			return err
		}
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
	rc.RetryWaitMin = 60 * time.Second
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
