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
	"strconv"
	"strings"
)

func (provider bitbucketHost) auth(c *http.Client, key, secret string) (token string, err error) {
	rc := retryablehttp.NewClient()
	rc.Logger = nil
	rc.HTTPClient = c
	rc.RetryMax = 1

	b, _, _, err := httpRequest(httpRequestInput{
		client: rc,
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

func (provider bitbucketHost) describeRepos() (dRO describeReposOutput) {
	logger.Println("listing BitBucket repositories")

	var err error

	key := os.Getenv("BITBUCKET_KEY")
	secret := os.Getenv("BITBUCKET_SECRET")

	var token string

	token, err = provider.auth(httpClient, key, secret)
	if err != nil {
		logger.Fatal(err)
	}

	var repos []repository

	rawRequestURL := provider.APIURL + "/repositories?role=member"

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
		if err := json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
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

func (provider bitbucketHost) getAPIURL() string {
	return provider.APIURL
}

func bitBucketWorker(user, token, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		parts := strings.Split(repo.HTTPSUrl, "//")
		repo.URLWithBasicAuth = parts[0] + "//" + user + ":" + token + "@" + parts[1]
		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (provider bitbucketHost) Backup(backupDIR string) {
	maxConcurrent := 5

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}

	var err error

	user := os.Getenv("BITBUCKET_USER")
	key := os.Getenv("BITBUCKET_KEY")
	secret := os.Getenv("BITBUCKET_SECRET")

	backupsToKeep, err := strconv.Atoi(os.Getenv("BITBUCKET_BACKUPS"))
	if err != nil {
		backupsToKeep = 0
	}

	var token string
	token, err = provider.auth(client, key, secret)

	if err != nil {
		logger.Fatal(err)
	}

	drO := provider.describeRepos()

	jobs := make(chan repository, len(drO.Repos))

	results := make(chan error, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go bitBucketWorker(user, token, backupDIR, provider.diffRemoteMethod(), backupsToKeep, jobs, results)
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

type bitbucketHost struct {
	Provider         string
	APIURL           string
	DiffRemoteMethod string
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
func (provider bitbucketHost) diffRemoteMethod() string {
	switch strings.ToLower(provider.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	case "":
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", provider.DiffRemoteMethod)

		// default to bundle as safest
		return cloneMethod
	}
}
