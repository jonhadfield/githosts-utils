package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	gitlabMinAccessLevel = 10
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

func (provider gitlabHost) getProjectsByUserID(client http.Client) (repos []repository) {
	getUserIDURL := provider.APIURL + "/users/" + strconv.Itoa(provider.User.ID) + "/projects"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getUserIDURL, nil)
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

type gitLabGroup struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}
type gitLabGetGroupsResponse []gitLabGroup

func (provider gitlabHost) getProjectsByGroupID(client http.Client, groupID int) (repos []repository) {
	getProjectsByGroupIDURL := provider.APIURL + "/groups/" + strconv.Itoa(groupID) + "/projects"

	u, err := url.Parse(getProjectsByGroupIDURL)
	if err != nil {
		logger.Fatal(err)
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(gitlabProjectsPerPageDefault))
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	var nextPage string

	for {
		var req *http.Request

		if nextPage != "" {
			q.Set("page", nextPage)
			u.RawQuery = q.Encode()
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

		nextPage = resp.Header.Get("x-next-page")
		// if we don't have a next page, then break
		if nextPage == "" {
			break
		}
	}

	return repos
}

func (provider gitlabHost) getGroups(client http.Client) (groups []gitLabGroup) {
	minAccessLevel, err := strconv.Atoi(os.Getenv("GITLAB_GROUP_ACCESS_LEVEL_FILTER"))
	if err != nil {
		logger.Println("using default group access level filter")

		minAccessLevel = gitlabMinAccessLevel
	}

	getGroupsByAccessLevelURL := fmt.Sprintf("%s/groups?min_access_level=%d", provider.APIURL, minAccessLevel)

	u, err := url.Parse(getGroupsByAccessLevelURL)
	if err != nil {
		logger.Fatal(err)
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(gitlabGroupsPerPageDefault))
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	var nextPage string

	for {
		var req *http.Request

		if nextPage != "" {
			q.Set("page", nextPage)
			u.RawQuery = q.Encode()
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

		var bodyB []byte

		bodyB, err = io.ReadAll(resp.Body)
		if err != nil {
			return
		}

		bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))

		_ = resp.Body.Close()

		var respObj gitLabGetGroupsResponse
		if err = json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
			logger.Fatal(err)
		}

		groups = append(groups, respObj...)

		nextPage = resp.Header.Get("x-next-page")

		// if we don't have a next page, then break
		if nextPage == "" {
			break
		}
	}

	return groups
}

func (provider gitlabHost) describeRepos() describeReposOutput {
	logger.Println("listing GitLab repositories")

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout * time.Second,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}

	userRepos := provider.getProjectsByUserID(*client)

	groups := provider.getGroups(*client)

	var groupRepos []repository

	for _, g := range groups {
		groupRepos = append(groupRepos, provider.getProjectsByGroupID(*client, g.Id)...)
	}

	return describeReposOutput{
		Repos: append(userRepos, groupRepos...),
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
