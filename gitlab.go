package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/exp/slices"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMinimumProjectAccessLevel = 20
)

type gitlabHost struct {
	User     gitlabUser
	Provider string
	APIURL   string
}

type gitlabUser struct {
	ID       int    `json:"id"`
	UserName string `json:"username"`
}

func (provider gitlabHost) getAuthenticatedGitlabUser(client http.Client) (user gitlabUser) {
	var err error

	getUserIDURL := provider.APIURL + "/user"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	var req *http.Request

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, getUserIDURL, nil)
	if err != nil {
		logger.Fatal(err)
	}

	req.Header.Set("Private-Token", os.Getenv("GITLAB_TOKEN"))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	var resp *http.Response

	resp, err = client.Do(req)
	if err != nil {
		logger.Fatal(err)
	}

	bodyB, _ := io.ReadAll(resp.Body)
	bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))

	_ = resp.Body.Close()

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

func (provider gitlabHost) getAllProjectRepositories(client http.Client) (repos []repository) {
	var sortedLevels []int
	for k := range validAccessLevels {
		sortedLevels = append(sortedLevels, k)
	}

	sort.Ints(sortedLevels)

	var validMinimumProjectAccessLevels []string

	for _, level := range sortedLevels {
		validMinimumProjectAccessLevels = append(validMinimumProjectAccessLevels, fmt.Sprintf("%s (%d)", validAccessLevels[level], level))
	}

	logger.Println("retrieving all GitLab projects for user:", provider.User.UserName)

	getProjectsURL := provider.APIURL + "/projects"

	var minAccessLevel int
	var err error

	minAccessLevelEnvVar := os.Getenv("GITLAB_PROJECT_MIN_ACCESS_LEVEL")
	if minAccessLevelEnvVar != "" {
		minAccessLevel, err = strconv.Atoi(minAccessLevelEnvVar)
		if err != nil {
			logger.Printf("GITLAB_PROJECT_MIN_ACCESS_LEVEL '%s' is not a number so using default",
				minAccessLevelEnvVar)

			minAccessLevel = defaultMinimumProjectAccessLevel
		}
	}

	if !slices.Contains(sortedLevels, minAccessLevel) {
		if minAccessLevelEnvVar != "" {
			logger.Printf("GitLab project minimum access level must be one of %s so using default",
				strings.Join(validMinimumProjectAccessLevels, ", "))
		}

		minAccessLevel = defaultMinimumProjectAccessLevel
	}

	logger.Printf("GitLab project minimum access level set to %s (%d)",
		validAccessLevels[minAccessLevel],
		minAccessLevel)

	u, err := url.Parse(getProjectsURL)
	if err != nil {
		logger.Fatal(err)
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(gitlabProjectsPerPageDefault))
	q.Set("min_access_level", strconv.Itoa(minAccessLevel))
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		logger.Fatal(err)
	}

	req.Header.Set("Private-Token", os.Getenv("GITLAB_TOKEN"))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	var resp *http.Response

	resp, err = client.Do(req)
	if err != nil {
		logger.Fatal(err)
	}

	bodyB, _ := io.ReadAll(resp.Body)
	bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))
	if strings.ToLower(os.Getenv("SOBA_LOG")) == "trace" {
		logger.Println(bodyStr)
	}

	_ = resp.Body.Close()

	var respObj gitLabGetProjectsResponse

	if err = json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
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

	return repos
}

func (provider gitlabHost) describeRepos() describeReposOutput {
	logger.Println("listing GitLab repositories")

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout * time.Second,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}

	userRepos := provider.getAllProjectRepositories(*client)

	return describeReposOutput{
		Repos: userRepos,
	}

}

func (provider gitlabHost) getAPIURL() string {
	return provider.APIURL
}

func gitlabWorker(userName string, backupDIR string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = repo.HTTPSUrl[:firstPos+2] + userName + ":" + stripTrailing(os.Getenv("GITLAB_TOKEN"), "\n") + "@" + repo.HTTPSUrl[firstPos+2:]
		results <- processBackup(repo, backupDIR, backupsToKeep)
	}
}

func (provider gitlabHost) Backup(backupDIR string) {
	maxConcurrent := 5

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout * time.Second,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}
	provider.User = provider.getAuthenticatedGitlabUser(*client)
	repoDesc := provider.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	backupsToKeep, err := strconv.Atoi(os.Getenv("GITLAB_BACKUPS"))
	if err != nil {
		backupsToKeep = 0
	}

	for w := 1; w <= maxConcurrent; w++ {
		go gitlabWorker(provider.User.UserName, backupDIR, backupsToKeep, jobs, results)
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
