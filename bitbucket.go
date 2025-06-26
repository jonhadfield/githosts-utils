package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"gitlab.com/tozd/go/errors"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	BitbucketProviderName   = "BitBucket"
	bitbucketEnvVarKey      = "BITBUCKET_KEY"
	bitbucketEnvVarSecret   = "BITBUCKET_SECRET"
	bitbucketEnvVarUser     = "BITBUCKET_USER"
	bitbucketDomain         = "bitbucket.com"
	bitbucketStaticUserName = "x-bitbucket-api-token-auth"
)

type NewBitBucketHostInput struct {
	Caller           string
	HTTPClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
	Email            string
	Username         string
	BackupsToRetain  int
	LogLevel         int
	BackupLFS        bool
}

func NewBitBucketHost(input NewBitBucketHostInput) (*BitbucketHost, error) {
	setLoggerPrefix(input.Caller)

	apiURL := bitbucketAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod, err := getDiffRemoteMethod(input.DiffRemoteMethod)
	if err != nil {
		return nil, errors.Errorf("failed to get diff remote method: %s", err)
	}

	if diffRemoteMethod == "" {
		logger.Print("using default diff remote method: " + defaultRemoteMethod)
		diffRemoteMethod = defaultRemoteMethod
	} else {
		logger.Print("using diff remote method: " + diffRemoteMethod)
	}

	httpClient := input.HTTPClient
	if httpClient == nil {
		httpClient = getHTTPClient()
	}

	return &BitbucketHost{
		HttpClient:       httpClient,
		Provider:         BitbucketProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToRetain:  input.BackupsToRetain,
		Token:            input.Token,
		Email:            input.Email,
		BackupLFS:        input.BackupLFS,
	}, nil
}

func (bb BitbucketHost) auth() (string, error) {
	return bb.Token, nil
}

type bitbucketAuthResponse struct {
	AccessToken  string `json:"access_token"`
	Scopes       string `json:"scopes"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func (bb BitbucketHost) describeRepos() (describeReposOutput, errors.E) {
	logger.Println("listing BitBucket repositories")

	var err error

	var repos []repository

	rawRequestURL := bb.APIURL + "/repositories?role=member"
	rawRequestURL = urlWithBasicAuth(rawRequestURL, bb.Email, bb.Token)

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	for {
		req, errNewReq := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, rawRequestURL, nil)
		if errNewReq != nil {
			logger.Println(errNewReq)

			return describeReposOutput{}, errors.Wrap(errNewReq, "failed to create new request")
		}

		req.Method = http.MethodGet
		req.Header.Set("Content-Type", contentTypeApplicationJSON)
		req.Header.Set("Accept", contentTypeApplicationJSON)

		var resp *http.Response

		resp, err = bb.HttpClient.Do(req)
		if err != nil {
			logger.Println(err)

			return describeReposOutput{}, errors.Wrap(err, "failed to make request")
		}

		var bodyB []byte

		bodyB, err = io.ReadAll(resp.Body)
		if err != nil {
			return describeReposOutput{}, errors.Errorf("failed to read response body: %s", err)
		}

		bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))
		_ = resp.Body.Close()

		var respObj bitbucketGetProjectsResponse
		if err = json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
			logger.Println(err)

			return describeReposOutput{}, errors.Wrap(err, "failed to unmarshall bitbucket json response")
		}

		for _, r := range respObj.Values {
			if r.Scm == "git" {
				repo := repository{
					Name:              r.Name,
					HTTPSUrl:          "https://bitbucket.org/" + r.FullName + ".git",
					PathWithNameSpace: r.FullName,
					Domain:            bitbucketDomain,
				}

				repos = append(repos, repo)
			}
		}

		if respObj.Next != "" {
			rawRequestURL = respObj.Next

			continue
		}

		break
	}

	return describeReposOutput{
		Repos: repos,
	}, nil
}

func (bb BitbucketHost) getAPIURL() string {
	return bb.APIURL
}

func bitBucketWorker(logLevel int, email, token, backupDIR, diffRemoteMethod string, backupsToKeep int, backupLFS bool, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		repo.URLWithBasicAuth = urlWithBasicAuth(repo.HTTPSUrl, bitbucketStaticUserName, token)
		err := processBackup(logLevel, repo, backupDIR, backupsToKeep, diffRemoteMethod, backupLFS)
		results <- repoBackupResult(repo, err)
	}
}

func (bb BitbucketHost) Backup() ProviderBackupResult {
	if bb.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return ProviderBackupResult{}
	}

	maxConcurrent := 5

	var err error

	drO, err := bb.describeRepos()
	if err != nil {
		return ProviderBackupResult{}
	}

	jobs := make(chan repository, len(drO.Repos))

	results := make(chan RepoBackupResults, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go bitBucketWorker(bb.LogLevel, bb.Email, bb.Token, bb.BackupDir, bb.diffRemoteMethod(), bb.BackupsToRetain, bb.BackupLFS, jobs, results)
	}

	for x := range drO.Repos {
		repo := drO.Repos[x]
		jobs <- repo
	}

	close(jobs)

	var providerBackupResults ProviderBackupResult

	for a := 1; a <= len(drO.Repos); a++ {
		res := <-results
		if res.Error != nil {
			logger.Printf("backup failed: %+v\n", res.Error)

			providerBackupResults.Error = res.Error

			return providerBackupResults
		}

		providerBackupResults.BackupResults = append(providerBackupResults.BackupResults, res)
	}

	return providerBackupResults
}

type BitbucketHost struct {
	Caller           string
	HttpClient       *retryablehttp.Client
	Provider         string
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	BackupsToRetain  int
	Token            string
	Email            string
	LogLevel         int
	BackupLFS        bool
}

type bitbucketOwner struct {
	DisplayName string `json:"display_name"`
}

type bitbucketProject struct {
	Scm       string `json:"scm"`
	Owner     bitbucketOwner
	Name      string            `json:"name"`
	FullName  string            `json:"full_name"`
	IsPrivate bool              `json:"is_private"`
	Links     bitbucketRepoLink `json:"links"`
}

type bitbucketCloneDetail struct {
	Href string `json:"href"`
	Name string `json:"name"`
}

type bitbucketRepoLink struct {
	Clone []bitbucketCloneDetail `json:"clone"`
}

type bitbucketGetProjectsResponse struct {
	Pagelen int                `json:"pagelen"`
	Values  []bitbucketProject `json:"values"`
	Next    string             `json:"next"`
}

// return normalised method.
func (bb BitbucketHost) diffRemoteMethod() string {
	return canonicalDiffRemoteMethod(bb.DiffRemoteMethod)
}
