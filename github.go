package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	gitHubCallSize       = 100
	githubEnvVarBackups  = "GITHUB_BACKUPS"
	githubEnvVarCallSize = "GITHUB_CALL_SIZE"
)

type NewGitHubHostInput struct {
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
	Orgs             []string
}

func (gh *GitHubHost) getAPIURL() string {
	return gh.APIURL
}

func NewGitHubHost(input NewGitHubHostInput) (host *GitHubHost, err error) {
	apiURL := githubAPIURL
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

	return &GitHubHost{
		httpClient:       getHTTPClient(),
		Provider:         "GitHub",
		APIURL:           apiURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToKeep:    getBackupsToKeep(githubEnvVarBackups),
		Token:            input.Token,
	}, nil
}

type GitHubHost struct {
	httpClient       *retryablehttp.Client
	Provider         string
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	BackupsToKeep    int
	Token            string
	Orgs             []string
}

type edge struct {
	Node struct {
		Name          string
		NameWithOwner string
		URL           string `json:"Url"`
		SSHURL        string `json:"sshUrl"`
	}
	Cursor string
}

type githubQueryNamesResponse struct {
	Data struct {
		Viewer struct {
			Repositories struct {
				Edges    []edge
				PageInfo struct {
					EndCursor   string
					HasNextPage bool
				}
			}
		}
	}
}

type githubQueryOrgResponse struct {
	Data struct {
		Organization struct {
			Repositories struct {
				Edges    []edge
				PageInfo struct {
					EndCursor   string
					HasNextPage bool
				}
			}
		}
	}
}

type graphQLRequest struct {
	Query     string `json:"query"`
	Variables string `json:"variables"`
}

func (gh *GitHubHost) makeGithubRequest(payload string) string {
	contentReader := bytes.NewReader([]byte(payload))

	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	req, newReqErr := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", contentReader)

	if newReqErr != nil {
		logger.Fatal(newReqErr)
	}

	req.Header.Set("Authorization", fmt.Sprintf("bearer %s", gh.Token))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	resp, reqErr := gh.httpClient.Do(req)
	if reqErr != nil {
		logger.Fatal(reqErr)
	}

	bodyB, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Fatal(err)
	}

	bodyStr := string(bytes.ReplaceAll(bodyB, []byte("\r"), []byte("\r\n")))
	_ = resp.Body.Close()

	// check response for errors
	switch resp.StatusCode {
	case 401:
		if strings.Contains(bodyStr, "Personal access tokens with fine grained access do not support the GraphQL API") {
			logger.Fatal("GitHub authorisation with fine grained PAT (Personal Access Token) failed as their GraphQL endpoint currently only supports classic PATs: https://github.blog/2022-10-18-introducing-fine-grained-personal-access-tokens-for-github/#coming-next")
		}

		logger.Fatalf("GitHub authorisation failed: %s", bodyStr)
	case 200:
		// authorisation successful
	default:
		logger.Fatalf("GitHub request failed: %s", bodyStr)
	}

	return bodyStr
}

func (gh *GitHubHost) describeGithubUserRepos() []repository {
	logger.Println("listing GitHub user's repositories")

	gcs := gitHubCallSize
	envCallSize := os.Getenv(githubEnvVarCallSize)
	if envCallSize != "" {
		if callSize, err := strconv.Atoi(envCallSize); err != nil {
			gcs = callSize
		}
	}

	var repos []repository

	reqBody := "{\"query\": \"query { viewer { repositories(first:" + strconv.Itoa(gcs) + ") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }} } }\""

	for {
		bodyStr := gh.makeGithubRequest(reqBody)

		var respObj githubQueryNamesResponse
		if err := json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
			logger.Fatal(err)
		}

		for _, repo := range respObj.Data.Viewer.Repositories.Edges {
			repos = append(repos, repository{
				Name:              repo.Node.Name,
				SSHUrl:            repo.Node.SSHURL,
				HTTPSUrl:          repo.Node.URL,
				PathWithNameSpace: repo.Node.NameWithOwner,
				Domain:            "github.com",
			})
		}

		if !respObj.Data.Viewer.Repositories.PageInfo.HasNextPage {
			break
		} else {
			reqBody = "{\"query\": \"query($first:Int $after:String){ viewer { repositories(first:$first after:$after) { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }} } }\", \"variables\":{\"first\":" + strconv.Itoa(gcs) + ",\"after\":\"" + respObj.Data.Viewer.Repositories.PageInfo.EndCursor + "\"} }"
		}
	}

	return repos
}

func createGithubRequestPayload(body string) string {
	gqlMarshalled, err := json.Marshal(graphQLRequest{Query: body})
	if err != nil {
		logger.Fatal(err)
	}

	return string(gqlMarshalled)
}

func (gh *GitHubHost) describeGithubOrgRepos(orgName string) []repository {
	logger.Printf("listing GitHub organisation %s's repositories", orgName)

	gcs := gitHubCallSize
	envCallSize := os.Getenv(githubEnvVarCallSize)
	if envCallSize != "" {
		if callSize, err := strconv.Atoi(envCallSize); err != nil {
			gcs = callSize
		}
	}

	var repos []repository

	reqBody := "query { organization(login: \"" + orgName + "\") { repositories(first:" + strconv.Itoa(gcs) + ") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }}}}"

	for {
		bodyStr := gh.makeGithubRequest(createGithubRequestPayload(reqBody))

		var respObj githubQueryOrgResponse
		if err := json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
			logger.Fatal(err)
		}

		for _, repo := range respObj.Data.Organization.Repositories.Edges {
			repos = append(repos, repository{
				Name:              repo.Node.Name,
				SSHUrl:            repo.Node.SSHURL,
				HTTPSUrl:          repo.Node.URL,
				PathWithNameSpace: repo.Node.NameWithOwner,
				Domain:            "github.com",
			})
		}

		if !respObj.Data.Organization.Repositories.PageInfo.HasNextPage {
			break
		} else {
			reqBody = "query { organization(login: \"" + orgName + "\") { repositories(first:" + strconv.Itoa(gcs) + " after: \"" + respObj.Data.Organization.Repositories.PageInfo.EndCursor + "\") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }}}}"
		}
	}

	return repos
}

func (gh *GitHubHost) describeRepos() describeReposOutput {
	repos := gh.describeGithubUserRepos()

	for _, org := range gh.Orgs {
		repos = append(repos, gh.describeGithubOrgRepos(org)...)
	}

	return describeReposOutput{
		Repos: repos,
	}
}

func gitHubWorker(token, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(token, "\n"), repo.HTTPSUrl[firstPos+2:])
		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (gh *GitHubHost) Backup() {
	if gh.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return
	}

	maxConcurrent := 5
	repoDesc := gh.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	backupsToKeep, err := strconv.Atoi(os.Getenv(githubEnvVarBackups))
	if err != nil {
		backupsToKeep = 0
	}

	for w := 1; w <= maxConcurrent; w++ {
		go gitHubWorker(gh.Token, gh.BackupDir, gh.DiffRemoteMethod, backupsToKeep, jobs, results)
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
func (gh *GitHubHost) diffRemoteMethod() string {
	switch strings.ToLower(gh.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", gh.DiffRemoteMethod)

		// default to bundle as safest
		return cloneMethod
	}
}
