package githosts

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
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
}

type describeReposOutput struct {
	Repos []repository
}

type gitProvider interface {
	getAPIURL() string
	describeRepos() describeReposOutput
	Backup(string)
}

type newHostInput struct {
	ProviderName string
	APIURL       string
}

func createHost(input newHostInput) (gitProvider, error) {
	switch strings.ToLower(input.ProviderName) {
	case "bitbucket":
		return bitbucketHost{
			Provider: "BitBucket",
			APIURL:   input.APIURL,
		}, nil
	case "github":
		return githubHost{
			Provider: "Github",
			APIURL:   input.APIURL,
		}, nil
	case "gitlab":
		return gitlabHost{
			Provider: "Gitlab",
			APIURL:   input.APIURL,
		}, nil
	default:
		return nil, errors.New("provider invalid or not implemented")
	}
}

// gitRefs is a mapping of references to SHAs
type gitRefs map[string]string

func getLatestBundlePath(backupPath string) (path string, err error) {
	bFiles, err := getBundleFiles(backupPath)
	if err != nil {

		return
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

		// ignoring error output
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

	return backupPath + pathSep + ss[0].Key, nil
}

func getBundleRefs(bundlePath string) (heads gitRefs, err error) {
	bundleHeadsCmd := exec.Command("git", "bundle", "list-heads", bundlePath)
	out, bundleHeadsErr := bundleHeadsCmd.CombinedOutput()
	if bundleHeadsErr != nil {

		return heads, errors.New(string(out))
	}

	heads = make(map[string]string)
	lines := strings.Split(string(out), "\n")

	for x := range lines {
		// if empty (final line perhaps) then skip
		if len(strings.TrimSpace(lines[x])) == 0 {
			continue
		}

		fields := strings.Fields(lines[x])
		// expect only a sha and a ref
		if len(fields) != 2 {
			logger.Printf("invalid ref: %s", lines[x])
		}

		heads[fields[1]] = fields[0]
	}

	return
}

func dirHasBundles(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return false
	}

	defer f.Close()

	names, err := f.Readdirnames(1)
	if err == io.EOF {
		return false
	}

	if err != nil {
		logger.Printf("failed to read bundle directory contents: %s", err.Error())
	}

	for _, name := range names {
		if strings.HasSuffix(name, ".bundle") {
			return true
		}
	}

	return false
}

const (
	// invalidBundleStringCheck checks for a portion of the following in the command output
	// to determine if valid: "does not look like a v2 or v3 bundle file"
	invalidBundleStringCheck = "does not look like"
)

func getLatestBundleRefs(backupPath string) (refs gitRefs, err error) {
	// if we encounter an invalid bundle, then we need to repeat until we find a valid one or run out
	for {
		var path string
		path, err = getLatestBundlePath(backupPath)
		if err != nil {
			return nil, err
		}

		if refs, err = getBundleRefs(path); err != nil {
			logger.Print(err.Error())
			if strings.Contains(err.Error(), invalidBundleStringCheck) {
				// rename and try again
				if err = os.Rename(path, fmt.Sprintf("%s.invalid", path)); err != nil {
					return nil, fmt.Errorf("failed to rename invalid bundle %w", err)
				}
			}

			// otherwise, we should fail
			return refs, err

		}

		return refs, err
	}

}

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

func getRemoteRefs(cloneURL string) (refs gitRefs, err error) {
	remoteHeadsCmd := exec.Command("git", "ls-remote", cloneURL)

	out, err := remoteHeadsCmd.CombinedOutput()
	if err != nil {
		return refs, errors.Wrap(err, "failed to retrieve remote heads")
	}

	refs = make(map[string]string)
	lines := strings.Split(string(out), "\n")

	for x := range lines {
		// if empty (final line perhaps) then skip
		if len(strings.TrimSpace(lines[x])) == 0 {
			continue
		}
		fields := strings.Fields(lines[x])
		// expect only a sha and a ref
		if len(fields) != 2 {
			logger.Printf("invalid ref: %s", lines[x])
		}

		refs[fields[1]] = fields[0]
	}

	return
}

func createBundle(workingPath, backupPath string, repo repository) error {
	objectsPath := workingPath + pathSep + "objects"

	dirs, err := os.ReadDir(objectsPath)
	if err != nil {
		return errors.Wrapf(err, "failed to read objectsPath: %s", objectsPath)
	}

	emptyPack, checkEmptyErr := isEmpty(objectsPath + pathSep + "pack")
	if checkEmptyErr != nil {
		logger.Printf("failed to check if: '%s' is empty", objectsPath+pathSep+"pack")
	}

	if len(dirs) == 2 && emptyPack {
		logger.Printf("%s is empty, so not creating bundle", repo.Name)

		return nil
	}

	backupFile := repo.Name + "." + getTimestamp() + bundleExtension
	backupFilePath := backupPath + pathSep + backupFile

	createErr := createDirIfAbsent(backupPath)
	if createErr != nil {
		logger.Fatal(createErr)
	}

	logger.Printf("creating bundle for: %s", repo.Name)

	bundleCmd := exec.Command("git", "bundle", "create", backupFilePath, "--all")
	bundleCmd.Dir = workingPath

	var bundleOut bytes.Buffer

	bundleCmd.Stdout = &bundleOut
	bundleCmd.Stderr = &bundleOut

	if bundleErr := bundleCmd.Run(); bundleErr != nil {
		logger.Fatal(bundleErr)
	}

	return nil
}

func processBackup(repo repository, backupDIR string, backupsToKeep int) error {
	// create backup path
	workingPath := backupDIR + pathSep + workingDIRName + pathSep + repo.Domain + pathSep + repo.PathWithNameSpace
	backupPath := backupDIR + pathSep + repo.Domain + pathSep + repo.PathWithNameSpace
	// clean existing working directory
	delErr := os.RemoveAll(workingPath + pathSep)
	if delErr != nil {
		logger.Fatal(delErr)
	}

	var cloneURL string
	if repo.URLWithToken != "" {
		cloneURL = repo.URLWithToken
	} else if repo.URLWithBasicAuth != "" {
		cloneURL = repo.URLWithBasicAuth
	}

	// Check if existing, latest bundle refs, already match the remote
	if os.Getenv("SOBA_DEV") != "" {
		// check backup path exists before attempting to compare remote and local heads
		if remoteRefsMatchLocalRefs(cloneURL, backupPath) {
			logger.Printf("skipping clone of %s repo '%s' as refs match existing bundle", repo.Domain, repo.PathWithNameSpace)

			return nil
		}
	}

	// clone repo
	logger.Printf("cloning: %s", repo.HTTPSUrl)
	cloneCmd := exec.Command("git", "clone", "-v", "--mirror", cloneURL, workingPath)
	cloneCmd.Dir = backupDIR

	_, cloneErr := cloneCmd.CombinedOutput()
	if cloneErr != nil {
		return errors.Wrap(cloneErr, "cloning failed")
	}

	// create bundle
	if err := createBundle(workingPath, backupPath, repo); err != nil {
		return err
	}

	removeBundleIfDuplicate(backupPath)

	if backupsToKeep > 0 {
		if err := pruneBackups(backupPath, backupsToKeep); err != nil {
			return err
		}
	}

	return nil
}

func getBundleFiles(backupPath string) (bfs bundleFiles, err error) {
	files, err := os.ReadDir(backupPath)
	if err != nil {
		return nil, errors.Wrap(err, "backup path read failed")
	}

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".bundle") {
			logger.Printf("skipping non bundle file '%s'", f.Name())

			continue
		}

		var ts time.Time

		ts, err = timeStampFromBundleName(f.Name())
		if err != nil {
			return nil, err
		}

		var info os.FileInfo

		info, err = f.Info()
		if err != nil {
			return nil, err
		}

		bfs = append(bfs, bundleFile{
			info:    info,
			created: ts,
		})
	}

	sort.Sort(bfs)

	return bfs, err
}

func pruneBackups(backupPath string, keep int) error {
	logger.Printf("pruning %s to keep %d newest only", backupPath, keep)

	files, err := os.ReadDir(backupPath)
	if err != nil {
		return errors.Wrap(err, "backup path read failed")
	}

	var bfs bundleFiles

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".bundle") {
			logger.Printf("skipping non bundle file '%s'", f.Name())

			continue
		}

		var ts time.Time

		ts, err = timeStampFromBundleName(f.Name())
		if err != nil {
			return err
		}

		var info os.FileInfo

		info, err = f.Info()
		if err != nil {
			return err
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
			if err = os.Remove(backupPath + pathSep + f.Name()); err != nil {
				return err
			}

			continue
		}

		break
	}

	return err
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

func timeStampFromBundleName(i string) (t time.Time, err error) {
	tokens := strings.Split(i, ".")
	if len(tokens) < minBundleFileNameTokens {
		return time.Time{}, errors.New("invalid bundle name")
	}

	sTime := tokens[len(tokens)-2]
	if len(sTime) != bundleTimestampChars {
		return time.Time{}, fmt.Errorf("bundle '%s' has an invalid timestamp", i)
	}

	return timeStampToTime(sTime)
}

func getTimeStampPartFromFileName(name string) (timeStamp int, err error) {
	if strings.Count(name, ".") >= minBundleFileNameTokens-1 {
		parts := strings.Split(name, ".")
		strTimestamp := parts[len(parts)-2]
		return strconv.Atoi(strTimestamp)

	}

	return 0, fmt.Errorf("filename '%s' does not match bundle format <repo-name>.<timestamp>.bundle",
		name)
}

func filesIdentical(path1, path2 string) bool {
	// check if file sizes are same
	latestBundleSize := getFileSize(path1)

	previousBundleSize := getFileSize(path2)

	if latestBundleSize == previousBundleSize {
		// check if hashes match
		latestBundleHash, latestHashErr := getSHA2Hash(path1)
		if latestHashErr != nil {
			logger.Printf("failed to get sha2 hash for: %s", path1)
		}

		previousBundleHash, previousHashErr := getSHA2Hash(path2)

		if previousHashErr != nil {
			logger.Printf("failed to get sha2 hash for: %s", path2)
		}

		if reflect.DeepEqual(latestBundleHash, previousBundleHash) {
			return true
		}
	}

	return false
}

func removeBundleIfDuplicate(dir string) {
	files, err := getBundleFiles(dir)
	if err != nil {
		logger.Println(err)

		return
	}

	if len(files) == 1 {
		return
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

	latestBundleFilePath := dir + pathSep + ss[0].Key
	previousBundleFilePath := dir + pathSep + ss[0].Key

	if filesIdentical(latestBundleFilePath, previousBundleFilePath) {
		logger.Printf("no change since previous bundle: %s", ss[1].Key)
		logger.Printf("deleting duplicate bundle: %s", ss[0].Key)

		if deleteFile(dir+pathSep+ss[0].Key) != nil {
			logger.Println("failed to remove duplicate bundle")
		}
	}
}

func deleteFile(path string) (err error) {
	err = os.Remove(path)

	return
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
