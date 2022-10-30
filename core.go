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

func processBackup(repo repository, backupDIR string, backupsToKeep int) error {
	// CREATE BACKUP PATH
	workingPath := backupDIR + pathSep + workingDIRName + pathSep + repo.Domain + pathSep + repo.PathWithNameSpace
	backupPath := backupDIR + pathSep + repo.Domain + pathSep + repo.PathWithNameSpace
	// CLEAN EXISTING WORKING DIRECTORY
	delErr := os.RemoveAll(workingPath + pathSep)
	if delErr != nil {
		logger.Fatal(delErr)
	}
	// CLONE REPO
	logger.Printf("cloning: %s", repo.HTTPSUrl)

	var cloneURL string
	if repo.URLWithToken != "" {
		cloneURL = repo.URLWithToken
	} else if repo.URLWithBasicAuth != "" {
		cloneURL = repo.URLWithBasicAuth
	}

	cloneCmd := exec.Command("git", "clone", "-v", "--mirror", cloneURL, workingPath)
	cloneCmd.Dir = backupDIR

	_, cloneErr := cloneCmd.CombinedOutput()
	if cloneErr != nil {
		return errors.Wrap(cloneErr, "cloning failed")
	}

	// CREATE BUNDLE
	objectsPath := workingPath + pathSep + "objects"

	dirs, err := os.ReadDir(objectsPath)
	if err != nil {
		return errors.Wrapf(cloneErr, "failed to read objectsPath: %s", objectsPath)
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

	removeBundleIfDuplicate(backupPath)

	if backupsToKeep > 0 {
		if err = pruneBackups(backupPath, backupsToKeep); err != nil {
			return err
		}
	}

	return nil
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
			if err := os.Remove(backupPath + pathSep + f.Name()); err != nil {
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

func removeBundleIfDuplicate(dir string) {
	files, err := os.ReadDir(dir)
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
		if ts, err = getTimeStampPartFromFileName(f.Name()); err == nil {
			fNameTimes[f.Name()] = ts
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

	// check if file sizes are same
	latestBundleSize := getFileSize(dir + pathSep + ss[0].Key)

	previousBundleSize := getFileSize(dir + pathSep + ss[1].Key)

	if latestBundleSize == previousBundleSize {
		// check if hashes match
		latestBundleHash, latestHashErr := getSHA2Hash(dir + pathSep + ss[0].Key)
		if latestHashErr != nil {
			logger.Printf("failed to get sha2 hash for: %s", dir+pathSep+ss[0].Key)
		}

		previousBundleHash, previousHashErr := getSHA2Hash(dir + pathSep + ss[1].Key)

		if previousHashErr != nil {
			logger.Printf("failed to get sha2 hash for: %s", dir+pathSep+ss[1].Key)
		}

		if reflect.DeepEqual(latestBundleHash, previousBundleHash) {
			logger.Printf("no change since previous bundle: %s", ss[1].Key)
			logger.Printf("deleting duplicate bundle: %s", ss[0].Key)

			if deleteFile(dir+pathSep+ss[0].Key) != nil {
				logger.Println("failed to remove duplicate bundle")
			}
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
		if cErr := file.Close(); cErr != nil {
			logger.Printf("warn: failed to close: %s", filePath)
		}
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
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
