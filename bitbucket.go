//nolint:wsl_v5 // extensive whitespace linting would require significant refactoring
package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gitlab.com/tozd/go/errors"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	BitbucketProviderName = "BitBucket"
	// OAuth2
	bitbucketEnvVarKey    = "BITBUCKET_KEY"
	bitbucketEnvVarSecret = "BITBUCKET_SECRET"
	// URL parsing constants
	urlProtocolParts    = 2
	bitbucketEnvVarUser = "BITBUCKET_USER"
	// API OAuthToken
	bitbucketEnvVarAPIToken = "BITBUCKET_API_TOKEN"
	bitbucketEnvVarEmail    = "BITBUCKET_EMAIL"
	bitbucketDomain         = "bitbucket.com"
	bitbucketStaticUserName = "x-bitbucket-api-token-auth"
	// Auth Type
	AuthTypeBitbucketOAuth2   = AuthTypeBearerToken
	AuthTypeBitbucketAPIToken = AuthTypeBasicAuthHeader
	AuthTypeBasicAuthHeader   = "basic-auth-header"
	AuthTypeBearerToken       = "bearer-token"
	// Worker delay
	bitbucketEnvVarWorkerDelay  = "BITBUCKET_WORKER_DELAY"
	bitbucketDefaultWorkerDelay = 500
)

type NewBitBucketHostInput struct {
	Caller           string
	HTTPClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	// API OAuthToken
	Email     string
	BasicAuth BasicAuth
	AuthType  string
	// API OAuthToken
	APIToken string
	// OAuth2
	User                 string
	Key                  string
	Secret               string
	OAuthToken           string
	Username             string
	BackupsToRetain      int
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
	Workspaces           []string
}

func NewBitBucketHost(input NewBitBucketHostInput) (*BitbucketHost, error) {
	setLoggerPrefix(input.Caller)

	if input.AuthType == "" {
		return nil, errors.New("auth type must be specified")
	}

	apiURL := bitbucketAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod, err := getDiffRemoteMethod(input.DiffRemoteMethod)
	if err != nil {
		return nil, errors.Errorf("failed to get diff remote method: %s", err)
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

	bitbucketHost := &BitbucketHost{
		HttpClient:           httpClient,
		Provider:             BitbucketProviderName,
		APIURL:               apiURL,
		DiffRemoteMethod:     diffRemoteMethod,
		BackupDir:            input.BackupDir,
		BackupsToRetain:      input.BackupsToRetain,
		OAuthToken:           input.OAuthToken,
		APIToken:             input.APIToken,
		AuthType:             input.AuthType,
		BasicAuth:            input.BasicAuth,
		Email:                input.Email,
		BackupLFS:            input.BackupLFS,
		User:                 input.User,
		Key:                  input.Key,
		Secret:               input.Secret,
		EncryptionPassphrase: input.EncryptionPassphrase,
		Workspaces:           input.Workspaces,
	}

	// If key and secret are provided, get OAuth token
	if input.AuthType == AuthTypeBitbucketOAuth2 {
		if input.Key == "" || input.Secret == "" {
			return nil, errors.New("key and secret must be provided for BitBucket OAuth2 authentication")
		}

		oauthToken, err := auth(httpClient, input.Key, input.Secret)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get BitBucket OAuth token")
		}

		logger.Printf("BitBucket OAuth: successfully obtained access token")
		bitbucketHost.OAuthToken = oauthToken
		// Set user to empty when using OAuth token
		bitbucketHost.User = ""
	}

	return bitbucketHost, nil
}

func auth(httpClient *retryablehttp.Client, key, secret string) (string, error) {
	// Disable debug logging to prevent credential exposure
	httpClient.Logger = log.New(io.Discard, "", 0)

	b, _, _, err := httpRequest(httpRequestInput{
		client: httpClient,
		url:    fmt.Sprintf("https://%s:%s@bitbucket.org/site/oauth2/access_token", key, secret),
		method: http.MethodPost,
		headers: http.Header{
			"Host":            []string{"bitbucket.org"},
			HeaderContentType: []string{ContentTypeFormEncoded},
			HeaderAccept:      []string{ContentTypeAny},
		},
		reqBody:           []byte("grant_type=client_credentials"),
		basicAuthUser:     key,
		basicAuthPassword: secret,
		secrets:           []string{key, secret},
		timeout:           defaultHttpRequestTimeout,
	})
	if err != nil {
		return "", errors.Errorf("failed to get bitbucket auth token: %s", err)
	}

	bodyStr := string(bytes.ReplaceAll(b, []byte("\r"), []byte("\r\n")))

	var authResp bitbucketAuthResponse

	if err = json.Unmarshal([]byte(bodyStr), &authResp); err != nil {
		return "", errors.Errorf("failed to unmarshall bitbucket json response: %s", err)
	}

	// check for any errors
	if authResp.AccessToken == "" {
		var authErrResp bitbucketAuthErrorResponse

		if err = json.Unmarshal([]byte(bodyStr), &authErrResp); err != nil {
			return "", errors.Errorf("failed to unmarshall bitbucket json error response: %s", err)
		}

		return "", errors.Errorf("failed to get bitbucket auth token: %s - %s", authErrResp.Error, authErrResp.ErrorDescription)
	}

	return authResp.AccessToken, nil
}

// auth gets the OAuth2 access token for Bitbucket using the provided key and secret
//
//nolint:unused // Kept for potential future use
func (bb BitbucketHost) auth(key, secret string) (string, error) {
	// Ensure the HTTP client has secure logging to prevent credential exposure
	client := bb.HttpClient
	if client.Logger != nil {
		client.Logger = log.New(io.Discard, "", 0)
	}

	b, _, _, err := httpRequest(httpRequestInput{
		client: client,
		url:    fmt.Sprintf("https://%s:%s@bitbucket.org/site/oauth2/access_token", key, secret),
		method: http.MethodPost,
		headers: http.Header{
			"Host":            []string{"bitbucket.org"},
			HeaderContentType: []string{ContentTypeFormEncoded},
			HeaderAccept:      []string{ContentTypeAny},
		},
		reqBody:           []byte("grant_type=client_credentials"),
		basicAuthUser:     key,
		basicAuthPassword: secret,
		secrets:           []string{key, secret},
		timeout:           defaultHttpRequestTimeout,
	})
	if err != nil {
		return "", errors.Errorf("failed to get bitbucket auth token: %s", err)
	}

	bodyStr := string(bytes.ReplaceAll(b, []byte("\r"), []byte("\r\n")))

	var authResp bitbucketAuthResponse

	if err = json.Unmarshal([]byte(bodyStr), &authResp); err != nil {
		return "", errors.Errorf("failed to unmarshall bitbucket json response: %s", err)
	}

	// check for any errors
	if authResp.AccessToken == "" {
		var authErrResp bitbucketAuthErrorResponse

		if err = json.Unmarshal([]byte(bodyStr), &authErrResp); err != nil {
			return "", errors.Errorf("failed to unmarshall bitbucket json error response: %s", err)
		}

		return "", errors.Errorf("failed to get bitbucket auth token: %s - %s", authErrResp.Error, authErrResp.ErrorDescription)
	}

	return authResp.AccessToken, nil
}

type bitbucketAuthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type bitbucketAuthResponse struct {
	AccessToken  string `json:"access_token"`
	Scopes       string `json:"scopes"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

type bitbucketErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

//nolint:unused // Kept for potential future use
func urlWithBasicAuth(httpsURL, user, password string) string {
	parts := strings.SplitN(httpsURL, "//", urlProtocolParts)
	if len(parts) != urlProtocolParts {
		return httpsURL
	}

	return fmt.Sprintf("%s//%s:%s@%s", parts[0], user, password, parts[1])
}

// bitbucketAuthenticatedGet performs an authenticated GET request to the Bitbucket API,
// returning the response body. It handles both API token and OAuth2 bearer token auth.
func (bb BitbucketHost) bitbucketAuthenticatedGet(ctx context.Context, rawURL string) ([]byte, errors.E) {
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new request")
	}

	u, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return nil, errors.Wrap(parseErr, "failed to parse request URL")
	}

	req.URL = u

	switch bb.AuthType {
	case AuthTypeBitbucketAPIToken:
		req.SetBasicAuth(bb.Email, bb.APIToken)
	case AuthTypeBearerToken:
		req.Header.Set(HeaderAuthorization, AuthPrefixBearer+bb.OAuthToken)
	}

	req.Header.Set(HeaderContentType, contentTypeApplicationJSON)
	req.Header.Set(HeaderAccept, contentTypeApplicationJSON)

	resp, doErr := bb.HttpClient.Do(req)
	if doErr != nil {
		return nil, errors.Wrap(doErr, "failed to make request")
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Printf("unexpected status code: %s (%d)", resp.Status, resp.StatusCode)

		return nil, errors.Errorf("unexpected status code: %s (%d)", resp.Status, resp.StatusCode)
	}

	bodyB, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, errors.Errorf("failed to read response body: %s", readErr)
	}

	return bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")), nil
}

// getWorkspaces returns the list of workspace slugs to query for repositories.
// If explicit workspaces were configured, those are returned directly.
// Otherwise, it auto-discovers workspaces via the Bitbucket API.
func (bb BitbucketHost) getWorkspaces() ([]string, errors.E) {
	if len(bb.Workspaces) > 0 {
		logger.Printf("using %d configured BitBucket workspace(s)", len(bb.Workspaces))

		return bb.Workspaces, nil
	}

	logger.Println("discovering BitBucket workspaces")

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	var workspaces []string

	rawRequestURL := bb.APIURL + "/workspaces?role=member"

	for {
		body, err := bb.bitbucketAuthenticatedGet(ctx, rawRequestURL)
		if err != nil {
			return nil, errors.Wrap(err, "failed to list BitBucket workspaces")
		}

		var respObj bitbucketGetWorkspacesResponse
		if jErr := json.Unmarshal(body, &respObj); jErr != nil {
			return nil, errors.Wrap(jErr, "failed to unmarshal BitBucket workspaces response")
		}

		for _, w := range respObj.Values {
			workspaces = append(workspaces, w.Slug)
		}

		if respObj.Next != "" {
			rawRequestURL = respObj.Next

			continue
		}

		break
	}

	if len(workspaces) == 0 {
		return nil, errors.New("no BitBucket workspaces found for the authenticated user")
	}

	logger.Printf("found %d BitBucket workspace(s)", len(workspaces))

	return workspaces, nil
}

func (bb BitbucketHost) describeRepos() (describeReposOutput, errors.E) {
	logger.Println("listing BitBucket repositories")

	if bb.AuthType != AuthTypeBitbucketOAuth2 && bb.AuthType != AuthTypeBitbucketAPIToken {
		return describeReposOutput{}, errors.New("no authentication method available - need either OAuth key/secret or API token/email")
	}

	workspaces, wsErr := bb.getWorkspaces()
	if wsErr != nil {
		return describeReposOutput{}, errors.Wrap(wsErr, "failed to get BitBucket workspaces")
	}

	var repos []repository

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	for _, workspace := range workspaces {
		logger.Printf("listing repositories in BitBucket workspace: %s", workspace)

		rawRequestURL := bb.APIURL + "/repositories/" + url.PathEscape(workspace) + "?role=member"

		for {
			body, err := bb.bitbucketAuthenticatedGet(ctx, rawRequestURL)
			if err != nil {
				return describeReposOutput{}, errors.Errorf("failed to list repositories in workspace %s: %s", workspace, err)
			}

			var respObj bitbucketGetProjectsResponse
			if jErr := json.Unmarshal(body, &respObj); jErr != nil {
				return describeReposOutput{}, errors.Wrap(jErr, "failed to unmarshal BitBucket repositories response")
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
	}

	return describeReposOutput{
		Repos: repos,
	}, nil
}

func (bb BitbucketHost) getAPIURL() string {
	return bb.APIURL
}

func bitBucketWorker(config WorkerConfig, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		// Set up authentication for the repo
		if config.SetupRepo != nil {
			config.SetupRepo(&repo)
		}

		// Check if authentication was properly set up
		if repo.URLWithBasicAuth == "" {
			logger.Printf("BitBucket clone: no authentication available for repository %s", repo.PathWithNameSpace)
			results <- repoBackupResult(repo, errors.New("no authentication available for cloning"))

			continue
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

func (bb BitbucketHost) Backup() ProviderBackupResult {
	if bb.BackupDir == "" {
		logger.Print(msgBackupSkippedNoDir)

		return ProviderBackupResult{}
	}

	maxConcurrent := defaultMaxConcurrentGitLab

	drO, err := bb.describeRepos()
	if err != nil {
		return ProviderBackupResult{
			BackupResults: nil,
			Error:         err,
		}
	}

	jobs := make(chan repository, len(drO.Repos))
	results := make(chan RepoBackupResults, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go bitBucketWorker(WorkerConfig{
			LogLevel:         bb.LogLevel,
			BackupDir:        bb.BackupDir,
			DiffRemoteMethod: bb.diffRemoteMethod(),
			BackupsToKeep:    bb.BackupsToRetain,
			BackupLFS:        bb.BackupLFS,
			DefaultDelay:     bitbucketDefaultWorkerDelay,
			DelayEnvVar:      bitbucketEnvVarWorkerDelay,
			Secrets:          []string{bb.OAuthToken, bb.APIToken},
			SetupRepo: func(repo *repository) {
				var fUser, fToken string
				switch {
				case bb.OAuthToken != "":
					fUser = "x-token-auth"
					fToken = bb.OAuthToken
					logger.Printf("BitBucket clone: using OAuth token for repository %s", repo.PathWithNameSpace)
				case bb.APIToken != "":
					fUser = bitbucketStaticUserName
					fToken = bb.APIToken
				default:
					logger.Printf("BitBucket clone: no authentication available for repository %s", repo.PathWithNameSpace)

					return
				}
				repo.URLWithBasicAuth = urlWithBasicAuthURL(repo.HTTPSUrl, fUser, fToken)
			},
			EncryptionPassphrase: bb.EncryptionPassphrase,
		}, jobs, results)
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
	AuthType         string
	// API OAuthToken
	Email     string
	APIToken  string
	BasicAuth BasicAuth
	// OAuth2
	User                 string
	OAuthToken           string
	Key                  string
	Secret               string
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
	Workspaces           []string
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

type bitbucketWorkspace struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

type bitbucketGetWorkspacesResponse struct {
	Pagelen int                  `json:"pagelen"`
	Values  []bitbucketWorkspace `json:"values"`
	Next    string               `json:"next"`
}

// return normalised method.
func (bb BitbucketHost) diffRemoteMethod() string {
	return canonicalDiffRemoteMethod(bb.DiffRemoteMethod)
}
