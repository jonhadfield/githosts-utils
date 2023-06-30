package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterhellberg/link"
	"golang.org/x/exp/slices"

	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	// GitlabDefaultMinimumProjectAccessLevel https://docs.gitlab.com/ee/user/permissions.html#roles
	GitlabDefaultMinimumProjectAccessLevel = 20
	gitlabEnvVarToken                      = "GITLAB_TOKEN"
	gitlabEnvVarBackups                    = "GITLAB_BACKUPS"
	gitlabEnvVarProjectMinAccessLevel      = "GITLAB_PROJECT_MIN_ACCESS_LEVEL"
	gitlabEnvVarAPIUrl                     = "GITLAB_APIURL"
)

type gitlabUser struct {
	ID       int    `json:"id"`
	UserName string `json:"username"`
}

type GitlabHost struct {
	httpClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	BackupsToKeep    int
	Token            string
	User             gitlabUser
}

func NewGitLabHost(input NewGitlabHostInput) (host *GitlabHost, err error) {
	apiURL := gitlabAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod := cloneMethod
	if input.DiffRemoteMethod != "" {
		if !validDiffRemoteMethod(input.DiffRemoteMethod) {
			return nil, fmt.Errorf("invalid diff remote method: %s", input.DiffRemoteMethod)
		}

		diffRemoteMethod = input.DiffRemoteMethod
	}

	return &GitlabHost{
		httpClient:       getHTTPClient(),
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToKeep:    getBackupsToKeep(gitlabEnvVarBackups),
	}, nil
}

func (gl *GitlabHost) getAuthenticatedGitlabUser() (user gitlabUser) {
	gitlabToken := strings.TrimSpace(os.Getenv(gitlabEnvVarToken))
	if gitlabToken == "" {
		logger.Print("env var GITLAB_TOKEN not set")

		return
	}

	var err error

	// use default if not passed
	if gl.APIURL == "" {
		gl.APIURL = gitlabAPIURL
	}

	getUserIDURL := gl.APIURL + "/user"

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	var req *retryablehttp.Request

	req, err = retryablehttp.NewRequestWithContext(ctx, http.MethodGet, getUserIDURL, nil)
	if err != nil {
		logger.Fatal(err)
	}

	req.Header.Set("Private-Token", os.Getenv(gitlabEnvVarToken))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	var resp *http.Response

	resp, err = gl.httpClient.Do(req)
	if err != nil {
		logger.Fatal(err)
	}

	bodyB, _ := io.ReadAll(resp.Body)
	bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))

	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if strings.ToLower(os.Getenv(envVarGitHostsLog)) == "trace" {
			logger.Println("authentication successful")
		}
	case http.StatusForbidden:
		logger.Fatal("failed to authenticate (HTTP 403)")
	case http.StatusUnauthorized:
		logger.Fatal("failed to authenticate due to invalid credentials (HTTP 401)")
	default:
		logger.Printf("failed to authenticate due to unexpected response: %d (%s)", resp.StatusCode, resp.Status)

		return
	}

	if err = json.Unmarshal([]byte(bodyStr), &user); err != nil {
		logger.Fatal(err)
	}

	return user
}

type gitLabOwner struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type gitLabProject struct {
	Path              string      `json:"path"`
	PathWithNameSpace string      `json:"path_with_namespace"`
	HTTPSURL          string      `json:"http_url_to_repo"`
	SSHURL            string      `json:"ssh_url_to_repo"`
	Owner             gitLabOwner `json:"owner"`
}
type gitLabGetProjectsResponse []gitLabProject

var validAccessLevels = map[int]string{
	20: "Reporter",
	30: "Developer",
	40: "Maintainer",
	50: "Owner",
}

func (gl *GitlabHost) getAllProjectRepositories(client http.Client) (repos []repository) {
	var sortedLevels []int
	for k := range validAccessLevels {
		sortedLevels = append(sortedLevels, k)
	}

	sort.Ints(sortedLevels)

	var validMinimumProjectAccessLevels []string

	for _, level := range sortedLevels {
		validMinimumProjectAccessLevels = append(validMinimumProjectAccessLevels, fmt.Sprintf("%s (%d)", validAccessLevels[level], level))
	}

	logger.Printf("retrieving all projects for user %s (%d):", gl.User.UserName, gl.User.ID)

	if strings.TrimSpace(gl.APIURL) == "" {
		gl.APIURL = gitlabAPIURL
	}

	getProjectsURL := gl.APIURL + "/projects"

	var minAccessLevel int

	var err error

	minAccessLevelEnvVar := os.Getenv(gitlabEnvVarProjectMinAccessLevel)
	if minAccessLevelEnvVar != "" {
		minAccessLevel, err = strconv.Atoi(minAccessLevelEnvVar)
		if err != nil {
			logger.Printf("%s '%s' is not a number so using default",
				gitlabEnvVarProjectMinAccessLevel, minAccessLevelEnvVar)

			minAccessLevel = GitlabDefaultMinimumProjectAccessLevel
		}
	}

	if !slices.Contains(sortedLevels, minAccessLevel) {
		if minAccessLevelEnvVar != "" {
			logger.Printf("project minimum access level must be one of %s so using default",
				strings.Join(validMinimumProjectAccessLevels, ", "))
		}

		minAccessLevel = GitlabDefaultMinimumProjectAccessLevel
	}

	logger.Printf("project minimum access level set to %s (%d)",
		validAccessLevels[minAccessLevel],
		minAccessLevel)

	// Initial request
	u, err := url.Parse(getProjectsURL)
	if err != nil {
		return
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(gitlabProjectsPerPageDefault))
	q.Set("min_access_level", strconv.Itoa(minAccessLevel))
	u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	for {
		var resp *http.Response
		resp, body, err = makeGitLabRequest(&client, reqUrl)
		if err != nil {
			return
		}

		if strings.ToLower(os.Getenv(envVarGitHostsLog)) == "trace" {
			logger.Println(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if strings.ToLower(os.Getenv(envVarGitHostsLog)) == "trace" {
				logger.Println("projects retrieved successfully")
			}
		case http.StatusForbidden:
			logger.Println("failed to get projects due to invalid missing permissions (HTTP 403)")

			return repos
		default:
			logger.Printf("failed to get projects due to unexpected response: %d (%s)", resp.StatusCode, resp.Status)

			return repos
		}

		var respObj gitLabGetProjectsResponse

		if err = json.Unmarshal(body, &respObj); err != nil {
			logger.Fatal(err)
		}

		for _, project := range respObj {
			// gitlab replaces hyphens with spaces in owner names, so fix
			owner := strings.ReplaceAll(project.Owner.Name, " ", "-")
			repo := repository{
				Name:              project.Path,
				Owner:             owner,
				PathWithNameSpace: project.PathWithNameSpace,
				HTTPSUrl:          project.HTTPSURL,
				SSHUrl:            project.SSHURL,
				Domain:            "gitlab.com",
			}

			repos = append(repos, repo)
		}

		// if we got a link response then
		// reset request url
		reqUrl = ""
		for _, l := range link.ParseResponse(resp) {
			if l.Rel == "next" {
				reqUrl = l.URI
			}
		}

		if reqUrl == "" {
			break
		}
	}

	return repos
}

func makeGitLabRequest(c *http.Client, reqUrl string) (resp *http.Response, body []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	var req *http.Request

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return
	}

	req.Header.Set("Private-Token", os.Getenv(gitlabEnvVarToken))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	resp, err = c.Do(req)
	if err != nil {
		return
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	body = bytes.ReplaceAll(body, []byte("\r"), []byte("\r\n"))
	_ = resp.Body.Close()

	return resp, body, err
}

type NewGitlabHostInput struct {
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
}

func NewGitlabHost(input NewGitlabHostInput) (host *GitlabHost, err error) {
	apiURL := gitlabAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod := cloneMethod
	if input.DiffRemoteMethod != "" {
		if !validDiffRemoteMethod(input.DiffRemoteMethod) {
			return nil, fmt.Errorf("invalid diff remote method: %s", input.DiffRemoteMethod)
		}

		diffRemoteMethod = input.DiffRemoteMethod
	}

	return &GitlabHost{
		httpClient:       getHTTPClient(),
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToKeep:    getBackupsToKeep(gitlabEnvVarBackups),
	}, nil
}

func (gl *GitlabHost) auth(key, secret string) (token string, err error) {
	b, _, _, err := httpRequest(httpRequestInput{
		client: gl.httpClient,
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
		return "", errors.New("failed to unmarshall bitbucket json response")
	}

	return respObj.AccessToken, err
}

func (gl *GitlabHost) describeRepos() describeReposOutput {
	logger.Println("listing repositories")

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}

	userRepos := gl.getAllProjectRepositories(*client)

	return describeReposOutput{
		Repos: userRepos,
	}
}

func (gl *GitlabHost) getAPIURL() string {
	return gl.APIURL
}

func gitlabWorker(userName, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = repo.HTTPSUrl[:firstPos+2] + userName + ":" + stripTrailing(os.Getenv(gitlabEnvVarToken), "\n") + "@" + repo.HTTPSUrl[firstPos+2:]
		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (gl *GitlabHost) Backup() {
	if gl.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return
	}

	maxConcurrent := 5

	gl.httpClient = retryablehttp.NewClient()
	gl.User = gl.getAuthenticatedGitlabUser()
	if gl.User.ID == 0 {
		// skip backup if user is not authenticated
		return
	}

	repoDesc := gl.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	backupsToKeep, err := strconv.Atoi(os.Getenv(gitlabEnvVarBackups))
	if err != nil {
		backupsToKeep = 0
	}

	for w := 1; w <= maxConcurrent; w++ {
		go gitlabWorker(gl.User.UserName, gl.BackupDir, gl.diffRemoteMethod(), backupsToKeep, jobs, results)
	}

	for x := range repoDesc.Repos {
		repo := repoDesc.Repos[x]
		jobs <- repo
	}

	close(jobs)

	for a := 1; a <= len(repoDesc.Repos); a++ {
		res := <-results
		if res != nil {
			logger.Printf("backup failed: %+v\n", res)
		}
	}
}

// return normalised method
func (gl *GitlabHost) diffRemoteMethod() string {
	switch strings.ToLower(gl.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", gl.DiffRemoteMethod)

		// default to bundle as safest
		return cloneMethod
	}
}
