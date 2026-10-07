//nolint:wsl_v5 // extensive whitespace linting would require significant refactoring
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
	envVarSourcehutWorkerDelay   = "SOURCEHUT_WORKER_DELAY"
	sourcehutDomain              = "sourcehut"
	sourcehutProviderName        = "sourcehut"
	sourcehutDefaultWorkerDelay  = 500
	envSourcehutAPIURL           = "SOURCEHUT_APIURL"
	envSourcehutToken            = "SOURCEHUT_PAT" // nolint:gosec
	sourcehutRepoCountPerPage    = 20
	sourcehutMaxConcurrency      = defaultMaxConcurrentSourcehut
	sourcehutEnvVarMaxConcurrent = "SOURCEHUT_MAX_CONCURRENT"
	sourcehutGitHost             = "https://git.sr.ht/"
	sourcehutSSHHost             = "git@git.sr.ht:"
	sourcehutVisibilityPublic    = "public"
	sourcehutTildePrefix         = "~"
)

type NewSourcehutHostInput struct {
	HTTPClient           *retryablehttp.Client
	Caller               string
	APIURL               string
	DiffRemoteMethod     string
	BackupDir            string
	PersonalAccessToken  string
	LimitUserOwned       bool
	SkipUserRepos        bool
	Orgs                 []string
	BackupsToRetain      int
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
}

type SourcehutHost struct {
	Caller               string
	HttpClient           *retryablehttp.Client
	Provider             string
	APIURL               string
	DiffRemoteMethod     string
	BackupDir            string
	SkipUserRepos        bool
	LimitUserOwned       bool
	BackupsToRetain      int
	PersonalAccessToken  string
	Orgs                 []string
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
}

type sourcehutRepository struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Owner       struct {
		Username string `json:"username"`
	} `json:"owner"`
	// HEAD is only populated by queries that select it. git.sr.ht returns
	// null when HEAD does not resolve, i.e. for a repository without commits.
	HEAD *struct {
		Name string `json:"name"`
	} `json:"HEAD"`
}

// sourcehutRepositoryCursor is a page of git.sr.ht's RepositoryCursor.
type sourcehutRepositoryCursor struct {
	Results []sourcehutRepository `json:"results"`
	Cursor  *string               `json:"cursor"`
}

type sourcehutGraphQLErrors []struct {
	Message string `json:"message"`
}

type sourcehutRepositoriesResponse struct {
	Data struct {
		Repositories sourcehutRepositoryCursor `json:"repositories"`
	} `json:"data"`
	Errors sourcehutGraphQLErrors `json:"errors"`
}

// sourcehutRepoResultFields selects what describeSourcehutUserRepos needs
// from each repository.
const sourcehutRepoResultFields = `id name description visibility owner { ... on User { username } }`

// sourcehutRepoPageArgs are the arguments of a paged repositories field.
var sourcehutRepoPageArgs = `cursor: $cursor, filter: {count: ` + strconv.Itoa(sourcehutRepoCountPerPage) + `}`

// sourcehutOwnReposQuery lists the authenticated user's repositories a page
// at a time, selecting extraFields as well as sourcehutRepoResultFields.
func sourcehutOwnReposQuery(extraFields string) string {
	return `query ($cursor: Cursor) { repositories(` + sourcehutRepoPageArgs + `) { results { ` +
		sourcehutRepoResultFields + extraFields + ` } cursor } }`
}

func (sh *SourcehutHost) getAPIURL() string {
	return sh.APIURL
}

func NewSourcehutHost(input NewSourcehutHostInput) (*SourcehutHost, error) { //nolint:dupl // similar pattern across providers is intentional
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
		Caller:               input.Caller,
		HttpClient:           httpClient,
		Provider:             sourcehutProviderName,
		APIURL:               apiURL,
		DiffRemoteMethod:     diffRemoteMethod,
		BackupDir:            input.BackupDir,
		SkipUserRepos:        input.SkipUserRepos,
		LimitUserOwned:       input.LimitUserOwned,
		BackupsToRetain:      input.BackupsToRetain,
		PersonalAccessToken:  input.PersonalAccessToken,
		Orgs:                 input.Orgs,
		LogLevel:             input.LogLevel,
		BackupLFS:            input.BackupLFS,
		EncryptionPassphrase: input.EncryptionPassphrase,
	}, nil
}

func (sh *SourcehutHost) makeSourcehutRequest(ctx context.Context, payload string) (string, errors.E) {
	contentReader := bytes.NewReader([]byte(payload))

	ctx, cancel := context.WithTimeout(ctx, defaultHttpRequestTimeout)
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
func (sh *SourcehutHost) describeSourcehutUserRepos(ctx context.Context) ([]repository, errors.E) {
	logger.Println("listing SourceHut user's owned repositories")

	results, err := sh.pageSourcehutRepos(ctx, sourcehutOwnReposQuery(""), nil, decodeSourcehutOwnRepos)
	if err != nil {
		return nil, err
	}

	var repos []repository

	for _, repo := range results {
		// SourceHut private repositories cannot be cloned via HTTPS with personal access tokens
		// Only backup public repositories due to authentication limitations
		if !isSourcehutPublic(repo) {
			logger.Printf("Skipping private SourceHut repository %s (visibility: %s) - HTTPS cloning not supported for private repos", repo.Name, repo.Visibility)

			continue
		}

		repos = append(repos, sourcehutRepoToRepository(repo))
	}

	logger.Printf("Found %d public SourceHut repositories for backup", len(repos))
	return repos, nil
}

// asError logs each GraphQL error and returns an error if there were any.
func (errs sourcehutGraphQLErrors) asError() errors.E {
	if len(errs) == 0 {
		return nil
	}

	for _, err := range errs {
		logger.Printf("SourceHut API error: %s", err.Message)
	}

	return errors.New("SourceHut API returned errors")
}

// pageSourcehutRepos runs query, which takes a $cursor variable as well as
// vars, a page at a time until decode reports no further cursor, and returns
// the repositories from every page. decode extracts the page's repository
// cursor from a response body.
func (sh *SourcehutHost) pageSourcehutRepos(ctx context.Context, query string, vars map[string]any,
	decode func(body []byte) (sourcehutRepositoryCursor, errors.E),
) ([]sourcehutRepository, errors.E) {
	variables := make(map[string]any, len(vars)+1)
	for k, v := range vars {
		variables[k] = v
	}

	var (
		repos  []sourcehutRepository
		cursor *string
	)

	for {
		variables["cursor"] = cursor

		payload, mErr := json.Marshal(map[string]any{"query": query, "variables": variables})
		if mErr != nil {
			return nil, errors.Wrap(mErr, "failed to marshal SourceHut request")
		}

		bodyStr, err := sh.makeSourcehutRequest(ctx, string(payload))
		if err != nil {
			return nil, errors.Wrap(err, "SourceHut request failed")
		}

		page, err := decode([]byte(bodyStr))
		if err != nil {
			return nil, err
		}

		repos = append(repos, page.Results...)

		cursor = page.Cursor
		if cursor == nil {
			return repos, nil
		}
	}
}

// decodeSourcehutOwnRepos extracts the page from a response to
// sourcehutOwnReposQuery.
func decodeSourcehutOwnRepos(body []byte) (sourcehutRepositoryCursor, errors.E) {
	var respObj sourcehutRepositoriesResponse
	if uErr := json.Unmarshal(body, &respObj); uErr != nil {
		logger.Print(uErr)

		return sourcehutRepositoryCursor{}, errors.Wrap(uErr, "failed to unmarshal response")
	}

	if err := respObj.Errors.asError(); err != nil {
		return sourcehutRepositoryCursor{}, err
	}

	return respObj.Data.Repositories, nil
}

func isSourcehutPublic(repo sourcehutRepository) bool {
	return strings.ToLower(repo.Visibility) == sourcehutVisibilityPublic
}

// sourcehutRepoToRepository converts a git.sr.ht repository to the internal
// form. git.sr.ht's API does not return clone URLs, so they are constructed
// from SourceHut's conventions:
// https://git.sr.ht/~username/repository and git@git.sr.ht:~username/repository.
func sourcehutRepoToRepository(repo sourcehutRepository) repository {
	// Ensure canonical name has the ~ prefix if it doesn't already
	canonicalName := repo.Owner.Username
	if !strings.HasPrefix(canonicalName, sourcehutTildePrefix) {
		canonicalName = sourcehutTildePrefix + canonicalName
	}

	// For PathWithNameSpace, use the canonical name without ~ for file paths
	pathCanonicalName := strings.TrimPrefix(canonicalName, sourcehutTildePrefix)

	// Construct URLs following SourceHut convention (no .git suffix)
	return repository{
		Name:              repo.Name,
		Owner:             pathCanonicalName,
		SSHUrl:            sourcehutSSHHost + canonicalName + "/" + repo.Name,
		HTTPSUrl:          sourcehutGitHost + canonicalName + "/" + repo.Name,
		PathWithNameSpace: pathCanonicalName + "/" + repo.Name,
		Domain:            sourcehutDomain,
		IsPrivate:         !isSourcehutPublic(repo),
	}
}

func (sh *SourcehutHost) describeRepos() (describeReposOutput, errors.E) {
	var repos []repository

	if !sh.SkipUserRepos {
		// get authenticated user's owned repos
		var err errors.E

		repos, err = sh.describeSourcehutUserRepos(context.Background())
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

func sourcehutWorker(config WorkerConfig, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		// Set up authentication for the repo
		if config.SetupRepo != nil {
			config.SetupRepo(&repo)
		}

		err := processBackup(processBackupInput{
			LogLevel:             config.LogLevel,
			Repo:                 repo,
			BackupDIR:            config.BackupDir,
			BackupsToKeep:        config.BackupsToKeep,
			DiffRemoteMethod:     config.DiffRemoteMethod,
			BackupLFS:            config.BackupLFS,
			Secrets:              config.Secrets,
			EncryptionPassphrase: config.EncryptionPassphrase,
		})

		results <- repoBackupResult(repo, err)

		// Add delay between repository backups to prevent rate limiting
		delay := config.DefaultDelay
		if config.DelayEnvVar != "" {
			if envDelay, sErr := strconv.Atoi(os.Getenv(config.DelayEnvVar)); sErr == nil {
				delay = envDelay
			}
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

	// Lower default concurrency for SourceHut to be respectful of their API.
	maxConcurrent := maxConcurrentFromEnv(sourcehutEnvVarMaxConcurrent, sourcehutMaxConcurrency)

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
		go sourcehutWorker(WorkerConfig{
			LogLevel:         sh.LogLevel,
			BackupDir:        sh.BackupDir,
			DiffRemoteMethod: sh.DiffRemoteMethod,
			BackupsToKeep:    sh.BackupsToRetain,
			BackupLFS:        sh.BackupLFS,
			DefaultDelay:     sourcehutDefaultWorkerDelay,
			DelayEnvVar:      envVarSourcehutWorkerDelay,
			Secrets:          []string{sh.PersonalAccessToken},
			SetupRepo: func(repo *repository) {
				// Use HTTPS with token for SourceHut (no SSH due to firewall restrictions)
				repo.HTTPSUrl = strings.TrimSuffix(repo.HTTPSUrl, "/")
				// Try SourceHut-specific token format: just token as username with empty password
				cleanToken := stripTrailing(sh.PersonalAccessToken, "\n")
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
			},
			EncryptionPassphrase: sh.EncryptionPassphrase,
		}, jobs, results)

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
