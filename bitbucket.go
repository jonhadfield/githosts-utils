package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gitlab.com/tozd/go/errors"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	BitbucketProviderName = "BitBucket"
	// OAuth2
	bitbucketEnvVarKey    = "BITBUCKET_KEY"
	bitbucketEnvVarSecret = "BITBUCKET_SECRET"
	bitbucketEnvVarUser   = "BITBUCKET_USER"
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
)

type NewBitBucketHostInput struct {
	Caller           string
	HTTPClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	// API Token
	Email     string
	BasicAuth BasicAuth
	AuthType  string
	// API Token
	APIToken string
	// OAuth2
	User            string
	Key             string
	Secret          string
	Token           string
	Username        string
	BackupsToRetain int
	LogLevel        int
	BackupLFS       bool
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
		HttpClient:       httpClient,
		Provider:         BitbucketProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToRetain:  input.BackupsToRetain,
		OAuthToken:       input.Token,
		APIToken:         input.APIToken,
		AuthType:         input.AuthType,
		BasicAuth:        input.BasicAuth,
		Email:            input.Email,
		BackupLFS:        input.BackupLFS,
		User:             input.User,
		Key:              input.Key,
		Secret:           input.Secret,
	}

	// If key and secret are provided, get OAuth token
	if input.AuthType == AuthTypeBitbucketOAuth2 {
		if input.Key == "" || input.Secret == "" {
			return nil, errors.New("key and secret must be provided for BitBucket OAuth2 authentication")
		}

		oauthToken, err := auth(input.Key, input.Secret)
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

func auth(key, secret string) (string, error) {
	b, _, _, err := httpRequest(httpRequestInput{
		client: retryablehttp.NewClient(),
		url:    fmt.Sprintf("https://%s:%s@bitbucket.org/site/oauth2/access_token", key, secret),
		method: http.MethodPost,
		headers: http.Header{
			"Host":         []string{"bitbucket.org"},
			"Content-Type": []string{"application/x-www-form-urlencoded"},
			"Accept":       []string{"*/*"},
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
func (bb BitbucketHost) auth(key, secret string) (string, error) {
	b, _, _, err := httpRequest(httpRequestInput{
		client: bb.HttpClient,
		url:    fmt.Sprintf("https://%s:%s@bitbucket.org/site/oauth2/access_token", key, secret),
		method: http.MethodPost,
		headers: http.Header{
			"Host":         []string{"bitbucket.org"},
			"Content-Type": []string{"application/x-www-form-urlencoded"},
			"Accept":       []string{"*/*"},
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

func urlWithBasicAuth(httpsURL, user, password string) string {
	parts := strings.SplitN(httpsURL, "//", 2)
	if len(parts) != 2 {
		return httpsURL
	}

	return fmt.Sprintf("%s//%s:%s@%s", parts[0], user, password, parts[1])
}

func (bb BitbucketHost) describeRepos() (describeReposOutput, errors.E) {

	logger.Println("listing BitBucket repositories")

	var err error

	var repos []repository

	if bb.AuthType != AuthTypeBitbucketOAuth2 && bb.AuthType != AuthTypeBitbucketAPIToken {
		return describeReposOutput{}, errors.New("no authentication method available - need either OAuth key/secret or API token/email")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	rawRequestURL := bb.APIURL + "/repositories?role=member"

	for {
		req, errNewReq := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, rawRequestURL, nil)
		if errNewReq != nil {
			logger.Println(errNewReq)

			return describeReposOutput{}, errors.Wrap(errNewReq, "failed to create new request")
		}

		var requestUrl string

		switch bb.AuthType {
		case AuthTypeBitbucketAPIToken:
			req.SetBasicAuth(bb.Email, bb.APIToken)

			requestUrl = rawRequestURL

			var u *url.URL

			u, err = url.Parse(requestUrl)
			if err != nil {
				logger.Println(err)

				return describeReposOutput{}, errors.Wrap(err, "failed to parse request URL")
			}

			req.URL = u
		case AuthTypeBearerToken:
			// if it's auth url, then it's the API token
			requestUrl = rawRequestURL

			var u *url.URL

			u, err = url.Parse(requestUrl)
			if err != nil {
				logger.Println(err)

				return describeReposOutput{}, errors.Wrap(err, "failed to parse request URL")
			}

			req.URL = u
			req.Header.Set("Authorization", "Bearer "+bb.OAuthToken)
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

		if resp.StatusCode != http.StatusOK {
			logger.Printf("unexpected status code: %s (%d)", resp.Status, resp.StatusCode)

			_ = resp.Body.Close()

			return describeReposOutput{}, errors.Errorf("unexpected status code: %s (%d)", resp.Status, resp.StatusCode)
		}

		var bodyB []byte

		bodyB, err = io.ReadAll(resp.Body)
		if err != nil {
			return describeReposOutput{}, errors.Errorf("failed to read response body: %s", err)
		}

		bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errResp bitbucketErrorResponse
			if err = json.Unmarshal([]byte(bodyStr), &errResp); err != nil {
				logger.Println(err)

				return describeReposOutput{}, errors.Wrap(err, "failed to unmarshall bitbucket error json response")
			}

			return describeReposOutput{}, errors.Errorf("bitbucket request failed: %s", errResp.Error.Message)
		}

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

func bitBucketWorker(logLevel int, email, token, apiToken, backupDIR, diffRemoteMethod string, backupsToKeep int, backupLFS bool, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		var fUser string

		var fToken string

		if token != "" {
			fUser = "x-token-auth"
			fToken = token

			logger.Printf("BitBucket clone: using OAuth token for repository %s", repo.PathWithNameSpace)
		} else if apiToken != "" {
			fUser = bitbucketStaticUserName
			fToken = apiToken
		} else {
			logger.Printf("BitBucket clone: no authentication available for repository %s", repo.PathWithNameSpace)
			results <- repoBackupResult(repo, errors.New("no authentication available for cloning"))

			continue
		}

		repo.URLWithBasicAuth = urlWithBasicAuthURL(repo.HTTPSUrl, fUser, fToken)

		err := processBackup(logLevel, repo, backupDIR, backupsToKeep, diffRemoteMethod, backupLFS)
		results <- repoBackupResult(repo, err)
	}
}

func (bb BitbucketHost) Backup() ProviderBackupResult {
	if bb.BackupDir == "" {
		logger.Printf(msgBackupSkippedNoDir)

		return ProviderBackupResult{}
	}

	maxConcurrent := 5

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
		go bitBucketWorker(bb.LogLevel, bb.Email, bb.OAuthToken, bb.APIToken, bb.BackupDir, bb.diffRemoteMethod(), bb.BackupsToRetain, bb.BackupLFS, jobs, results)
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
	User       string
	OAuthToken string
	Key        string
	Secret     string
	LogLevel   int
	BackupLFS  bool
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
