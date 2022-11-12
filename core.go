package githosts

import (
	"github.com/pkg/errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
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
	diffRemoteMethod() string
}

type newHostInput struct {
	ProviderName  string
	APIURL        string
	CompareMethod string
}

func createHost(input newHostInput) (gitProvider, error) {
	// default compare method to clone
	if input.CompareMethod == "" {
		input.CompareMethod = cloneMethod
	}

	switch strings.ToLower(input.ProviderName) {
	case "bitbucket":
		return bitbucketHost{
			Provider:         "BitBucket",
			APIURL:           input.APIURL,
			DiffRemoteMethod: input.CompareMethod,
		}, nil
	case "github":
		return githubHost{
			Provider:         "Github",
			APIURL:           input.APIURL,
			DiffRemoteMethod: input.CompareMethod,
		}, nil
	case "gitlab":
		return gitlabHost{
			Provider:         "Gitlab",
			APIURL:           input.APIURL,
			DiffRemoteMethod: input.CompareMethod,
		}, nil
	default:
		return nil, errors.New("provider invalid or not implemented")
	}
}

// gitRefs is a mapping of references to SHAs
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

func processBackup(repo repository, backupDIR string, backupsToKeep int, diffRemoteMethod string) error {
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
	if diffRemoteMethod == refsMethod {
		// check backup path exists before attempting to compare remote and local heads
		if remoteRefsMatchLocalRefs(cloneURL, backupPath) {
			logger.Printf("skipping clone of %s repo '%s' as refs match existing bundle", repo.Domain, repo.PathWithNameSpace)

			return nil
		}
	}

	// clone repo
	logger.Printf("cloning: %s to: %s", repo.HTTPSUrl, workingPath)
	cloneCmd := exec.Command("git", "clone", "-v", "--mirror", cloneURL, workingPath)
	cloneCmd.Dir = backupDIR

	cloneOut, cloneErr := cloneCmd.CombinedOutput()
	cloneOutLines := strings.Split(string(cloneOut), "\n")
	if cloneErr != nil {
		return errors.Wrapf(cloneErr, "cloning failed: %s", strings.Join(cloneOutLines, ", "))
	}

	// create bundle
	if err := createBundle(workingPath, backupPath, repo); err != nil {
		if strings.HasSuffix(err.Error(), "is empty") {
			logger.Printf("skipping empty %s repository %s", repo.Domain, repo.PathWithNameSpace)
			return nil
		}

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
