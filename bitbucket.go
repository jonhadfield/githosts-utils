package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	BitbucketProviderName = "BitBucket"
	bitbucketEnvVarKey    = "BITBUCKET_KEY"
	bitbucketEnvVarSecret = "BITBUCKET_SECRET"
	bitbucketEnvVarUser   = "BITBUCKET_USER"
)

type NewBitBucketHostInput struct {
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	User             string
	Key              string
	Secret           string
	BackupsToRetain  int
}

func NewBitBucketHost(input NewBitBucketHostInput) (host *BitbucketHost, err error) {
	apiURL := bitbucketAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod := cloneMethod
	if input.DiffRemoteMethod != "" {
		if !validDiffRemoteMethod(input.DiffRemoteMethod) {
			return nil, errors.Errorf("invalid diff remote method: %s", input.DiffRemoteMethod)
		}

		diffRemoteMethod = input.DiffRemoteMethod
	}

	return &BitbucketHost{
		httpClient:       getHTTPClient(),
		Provider:         BitbucketProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToRetain:  input.BackupsToRetain,
		User:             input.User,
		Key:              input.Key,
		Secret:           input.Secret,
	}, nil
}

func (bb BitbucketHost) auth(key, secret string) (token string, err error) {
	b, _, _, err := httpRequest(httpRequestInput{
		client: bb.httpClient,
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
		return
	}

	bodyStr := string(bytes.ReplaceAll(b, []byte("\r"), []byte("\r\n")))

	var respObj bitbucketAuthResponse

	if err = json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
		return "", errors.Wrap(err, "failed to unmarshall bitbucket json response")
	}

	return respObj.AccessToken, err
}

type bitbucketAuthResponse struct {
	AccessToken  string `json:"access_token"`
	Scopes       string `json:"scopes"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func (bb BitbucketHost) describeRepos() (dRO describeReposOutput) {
	logger.Println("listing BitBucket repositories")

	var err error

	key := os.Getenv(bitbucketEnvVarKey)
	secret := os.Getenv(bitbucketEnvVarSecret)

	var token string

	token, err = bb.auth(key, secret)
	if err != nil {
		logger.Fatal(err)
	}

	var repos []repository

	rawRequestURL := bb.APIURL + "/repositories?role=member"

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	for {
		req, errNewReq := http.NewRequestWithContext(ctx, http.MethodGet, rawRequestURL, nil)
		if errNewReq != nil {
			logger.Fatal(errNewReq)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("Accept", "application/json; charset=utf-8")

		var resp *http.Response

		resp, err = httpClient.Do(req)
		if err != nil {
			logger.Fatal(err)
		}

		bodyB, _ := io.ReadAll(resp.Body)

		bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))
		_ = resp.Body.Close()

		var respObj bitbucketGetProjectsResponse
		if err = json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
			logger.Fatal(err)
		}

		for _, r := range respObj.Values {
			if r.Scm == "git" {
				repo := repository{
					Name:              r.Name,
					HTTPSUrl:          "https://bitbucket.org/" + r.FullName + ".git",
					PathWithNameSpace: r.FullName,
					Domain:            "bitbucket.com",
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
	}
}

func (bb BitbucketHost) getAPIURL() string {
	return bb.APIURL
}

func bitBucketWorker(user, token, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		parts := strings.Split(repo.HTTPSUrl, "//")
		repo.URLWithBasicAuth = parts[0] + "//" + user + ":" + token + "@" + parts[1]
		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (bb BitbucketHost) Backup() {
	if bb.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return
	}

	maxConcurrent := 5

	var err error

	var token string
	token, err = bb.auth(bb.Key, bb.Secret)
	if err != nil {
		logger.Fatal(err)
	}

	drO := bb.describeRepos()

	jobs := make(chan repository, len(drO.Repos))

	results := make(chan error, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go bitBucketWorker(bb.User, token, bb.BackupDir, bb.diffRemoteMethod(), bb.BackupsToRetain, jobs, results)
	}

	for x := range drO.Repos {
		repo := drO.Repos[x]
		jobs <- repo
	}

	close(jobs)

	for a := 1; a <= len(drO.Repos); a++ {
		res := <-results
		if res != nil {
			logger.Printf("backup failed: %+v\n", res)
		}
	}
}

type BitbucketHost struct {
	httpClient       *retryablehttp.Client
	Provider         string
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	BackupsToRetain  int
	User             string
	Key              string
	Secret           string
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

// return normalised method
func (bb BitbucketHost) diffRemoteMethod() string {
	switch strings.ToLower(bb.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	case "":
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", bb.DiffRemoteMethod)

		// default to bundle as safest
		return cloneMethod
	}
}
