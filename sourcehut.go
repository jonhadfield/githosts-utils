package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"gitlab.com/tozd/go/errors"
)

const (
	envVarSourcehutWorkerDelay  = "SOURCEHUT_WORKER_DELAY"
	sourcehutDomain             = "sourcehut"
	sourcehutProviderName       = "sourcehut"
	sourcehutDefaultWorkerDelay = 500
	envSourcehutAPIURL          = "SOURCEHUT_APIURL"
	envSourcehutToken           = "SOURCEHUT_PAT"
)

type NewSourcehutHostInput struct {
	HTTPClient          *retryablehttp.Client
	Caller              string
	APIURL              string
	DiffRemoteMethod    string
	BackupDir           string
	PersonalAccessToken string
	LimitUserOwned      bool
	SkipUserRepos       bool
	Orgs                []string
	BackupsToRetain     int
	LogLevel            int
	BackupLFS           bool
}

type SourcehutHost struct {
	Caller              string
	HttpClient          *retryablehttp.Client
	Provider            string
	APIURL              string
	DiffRemoteMethod    string
	BackupDir           string
	SkipUserRepos       bool
	LimitUserOwned      bool
	BackupsToRetain     int
	PersonalAccessToken string
	Orgs                []string
	LogLevel            int
	BackupLFS           bool
}

type sourcehutRepository struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Owner       struct {
		Username string `json:"username"`
	} `json:"owner"`
}

type sourcehutRepositoriesResponse struct {
	Data struct {
		Repositories struct {
			Results []sourcehutRepository `json:"results"`
			Cursor  *string               `json:"cursor"`
		} `json:"repositories"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (sh *SourcehutHost) getAPIURL() string {
	return sh.APIURL
}

func NewSourcehutHost(input NewSourcehutHostInput) (*SourcehutHost, error) {
	setLoggerPrefix(input.Caller)

	apiURL := sourcehutAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod, err := getDiffRemoteMethod(input.DiffRemoteMethod)
	if err != nil {
		return nil, err
	}

	if diffRemoteMethod == "" {
		logger.Print(msgUsingDefaultDiffRemoteMethod + ": " + defaultRemoteMethod)
		diffRemoteMethod = defaultRemoteMethod
	} else {
		logger.Print(msgUsingDiffRemoteMethod + ": " + diffRemoteMethod)
	}

	httpClient := input.HTTPClient
	if httpClient == nil {
		httpClient = getHTTPClient()
	}

	return &SourcehutHost{
		Caller:              input.Caller,
		HttpClient:          httpClient,
		Provider:            sourcehutProviderName,
		APIURL:              apiURL,
		DiffRemoteMethod:    diffRemoteMethod,
		BackupDir:           input.BackupDir,
		SkipUserRepos:       input.SkipUserRepos,
		LimitUserOwned:      input.LimitUserOwned,
		BackupsToRetain:     input.BackupsToRetain,
		PersonalAccessToken: input.PersonalAccessToken,
		Orgs:                input.Orgs,
		LogLevel:            input.LogLevel,
		BackupLFS:           input.BackupLFS,
	}, nil
}

func (sh *SourcehutHost) makeSourcehutRequest(payload string) (string, errors.E) {
	contentReader := bytes.NewReader([]byte(payload))

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	req, newReqErr := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, sh.APIURL, contentReader)

	if newReqErr != nil {
		logger.Println(newReqErr)

		return "", errors.Wrap(newReqErr, "failed to create request")
	}

	req.Header.Set(HeaderAuthorization, AuthPrefixBearer+sh.PersonalAccessToken)
	req.Header.Set(HeaderContentType, contentTypeApplicationJSON)
	req.Header.Set(HeaderAccept, contentTypeApplicationJSON)

	resp, reqErr := sh.HttpClient.Do(req)
	if reqErr != nil {
		logger.Print(reqErr)

		return "", errors.Wrap(reqErr, "failed to make request")
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Printf("failed to close response body: %s", closeErr.Error())
		}
	}()

	bodyB, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Print(err)

		return "", errors.Wrap(err, "failed to read response body")
	}

	bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))

	// check response for errors
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		logger.Printf("SourceHut authorisation failed: %s", bodyStr)
		return "", errors.Errorf("SourceHut authorisation failed: %s", bodyStr)
	case http.StatusForbidden:
		logger.Printf("SourceHut access forbidden: %s", bodyStr)
		return "", errors.Errorf("SourceHut access forbidden: %s", bodyStr)
	case http.StatusOK:
		// authorisation successful
	default:
		logger.Printf("SourceHut request failed with status %d: %s", resp.StatusCode, bodyStr)
		return "", errors.Errorf("SourceHut request failed with status %d: %s", resp.StatusCode, bodyStr)
	}

	return bodyStr, nil
}

// describeSourcehutUserRepos returns a list of repositories owned by authenticated user.
func (sh *SourcehutHost) describeSourcehutUserRepos() ([]repository, errors.E) {
	logger.Println("listing SourceHut user's owned repositories")

	var repos []repository

	var cursor *string

	for {
		var reqBody string
		if cursor == nil {
			reqBody = `{"query": "query { repositories(filter: {count: 20}) { results { id name description visibility owner { ... on User { username } } } cursor } }"}`
		} else {
			reqBody = `{"query": "query { repositories(cursor: \"` + *cursor + `\", filter: {count: 20}) { results { id name description visibility owner { ... on User { username } } } cursor } }"}`
		}

		bodyStr, err := sh.makeSourcehutRequest(reqBody)
		if err != nil {
			return nil, errors.Wrap(err, "SourceHut request failed")
		}

		var respObj sourcehutRepositoriesResponse
		if uErr := json.Unmarshal([]byte(bodyStr), &respObj); uErr != nil {
			logger.Print(uErr)
			return nil, errors.Wrap(uErr, "failed to unmarshal response")
		}

		if len(respObj.Errors) > 0 {
			for _, err := range respObj.Errors {
				logger.Printf("SourceHut API error: %s", err.Message)
			}
			return nil, errors.New("SourceHut API returned errors")
		}

		for _, repo := range respObj.Data.Repositories.Results {
			// SourceHut private repositories cannot be cloned via HTTPS with personal access tokens
			// Only backup public repositories due to authentication limitations
			if strings.ToLower(repo.Visibility) != "public" {
				logger.Printf("Skipping private SourceHut repository %s (visibility: %s) - HTTPS cloning not supported for private repos", repo.Name, repo.Visibility)

				continue
			}

			// Construct clone URLs manually based on SourceHut conventions
			// Format: https://git.sr.ht/~username/repository and git@git.sr.ht:~username/repository

			// Ensure canonical name has the ~ prefix if it doesn't already
			canonicalName := repo.Owner.Username
			if !strings.HasPrefix(canonicalName, "~") {
				canonicalName = "~" + canonicalName
			}

			// Construct URLs following SourceHut convention (no .git suffix)
			httpsURL := "https://git.sr.ht/" + canonicalName + "/" + repo.Name
			sshURL := "git@git.sr.ht:" + canonicalName + "/" + repo.Name

			// For PathWithNameSpace, use the canonical name without ~ for file paths
			pathCanonicalName := strings.TrimPrefix(canonicalName, "~")

			repos = append(repos, repository{
				Name:              repo.Name,
				Owner:             pathCanonicalName,
				SSHUrl:            sshURL,
				HTTPSUrl:          httpsURL,
				PathWithNameSpace: pathCanonicalName + "/" + repo.Name,
				Domain:            sourcehutDomain,
			})
		}

		cursor = respObj.Data.Repositories.Cursor
		if cursor == nil {
			break
		}
	}

	logger.Printf("Found %d public SourceHut repositories for backup", len(repos))
	return repos, nil
}

func (sh *SourcehutHost) describeRepos() (describeReposOutput, errors.E) {
	var repos []repository

	if !sh.SkipUserRepos {
		// get authenticated user's owned repos
		var err errors.E

		repos, err = sh.describeSourcehutUserRepos()
		if err != nil {
			logger.Print("failed to get SourceHut user repos")

			return describeReposOutput{}, err
		}
	}

	// SourceHut doesn't have organizations like GitHub/GitLab
	// If specific usernames are provided, we could potentially query their public repos
	// but this functionality is not currently supported by this implementation
	if len(sh.Orgs) > 0 {
		logger.Printf("Warning: SourceHut organization support not implemented, ignoring %d org(s)", len(sh.Orgs))
	}

	return describeReposOutput{
		Repos: repos,
	}, nil
}

func sourcehutWorker(logLevel int, token, backupDIR, diffRemoteMethod string, backupsToKeep int, backupLFS bool, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		// Use HTTPS with token for SourceHut (no SSH due to firewall restrictions)
		repo.HTTPSUrl = strings.TrimSuffix(repo.HTTPSUrl, "/")

		// Try SourceHut-specific token format: just token as username with empty password
		cleanToken := stripTrailing(token, "\n")
		httpsURL := repo.HTTPSUrl

		// Try different SourceHut authentication formats
		if strings.HasPrefix(httpsURL, "https://") {
			urlPart := httpsURL[8:] // Remove "https://"
			// Try token as username with empty password (SourceHut specific)
			repo.URLWithToken = "https://" + cleanToken + ":@" + urlPart
		} else {
			// Fallback to standard method
			repo.URLWithToken = urlWithToken(repo.HTTPSUrl, cleanToken)
		}

		repo.URLWithToken = strings.TrimSuffix(repo.URLWithToken, "/")

		logger.Printf("SourceHut worker processing repo: %s", repo.Name)
		logger.Printf("SourceHut worker base URL: %s", repo.HTTPSUrl)
		logger.Printf("SourceHut worker using token auth format")

		err := processBackup(processBackupInput{
			LogLevel:         logLevel,
			Repo:             repo,
			BackupDIR:        backupDIR,
			BackupsToKeep:    backupsToKeep,
			DiffRemoteMethod: diffRemoteMethod,
			BackupLFS:        backupLFS,
			Secrets:          []string{token},
		})

		results <- repoBackupResult(repo, err)

		// Add delay between repository backups to prevent rate limiting
		delay := sourcehutDefaultWorkerDelay
		if envDelay, sErr := strconv.Atoi(os.Getenv(envVarSourcehutWorkerDelay)); sErr == nil {
			delay = envDelay
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

func (sh *SourcehutHost) Backup() ProviderBackupResult {
	if sh.BackupDir == "" {
		logger.Print(msgBackupSkippedNoDir)

		return ProviderBackupResult{
			BackupResults: nil,
			Error:         errors.New(msgBackupDirNotSpecified),
		}
	}

	maxConcurrent := 5 // Lower concurrency for SourceHut to be respectful

	repoDesc, err := sh.describeRepos()
	if err != nil {
		return ProviderBackupResult{
			BackupResults: nil,
			Error:         err,
		}
	}

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan RepoBackupResults, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go sourcehutWorker(sh.LogLevel, sh.PersonalAccessToken, sh.BackupDir, sh.DiffRemoteMethod, sh.BackupsToRetain, sh.BackupLFS, jobs, results)

		delay := sourcehutDefaultWorkerDelay
		if envDelay, sErr := strconv.Atoi(os.Getenv(envVarSourcehutWorkerDelay)); sErr == nil {
			delay = envDelay
		}

		time.Sleep(time.Duration(delay) * time.Millisecond)
	}

	for x := range repoDesc.Repos {
		repo := repoDesc.Repos[x]
		jobs <- repo
	}

	close(jobs)

	var providerBackupResults ProviderBackupResult

	for a := 1; a <= len(repoDesc.Repos); a++ {
		res := <-results
		if res.Error != nil {
			logger.Printf("backup failed: %+v\n", res.Error)
		}

		providerBackupResults.BackupResults = append(providerBackupResults.BackupResults, res)
	}

	return providerBackupResults
}

// return normalised method.
func (sh *SourcehutHost) diffRemoteMethod() string {
	if sh.DiffRemoteMethod == "" {
		logger.Printf("diff remote method not specified. defaulting to:%s", cloneMethod)
	}

	return canonicalDiffRemoteMethod(sh.DiffRemoteMethod)
}
