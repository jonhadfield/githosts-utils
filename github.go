package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/exp/slices"
)

const (
	gitHubCallSize       = 100
	githubEnvVarCallSize = "GITHUB_CALL_SIZE"
	gitHubDomain         = "github.com"
	gitHubProviderName   = "GitHub"
)

type NewGitHubHostInput struct {
	Caller           string
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
	SkipUserRepos    bool
	Orgs             []string
	BackupsToRetain  int
	LogLevel         int
}

func (gh *GitHubHost) getAPIURL() string {
	return gh.APIURL
}

func NewGitHubHost(input NewGitHubHostInput) (host *GitHubHost, err error) {
	setLoggerPrefix(input.Caller)

	apiURL := githubAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	return &GitHubHost{
		Caller:           input.Caller,
		httpClient:       getHTTPClient(),
		Provider:         gitHubProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: getDiffRemoteMethod(input.DiffRemoteMethod),
		BackupDir:        input.BackupDir,
		SkipUserRepos:    input.SkipUserRepos,
		BackupsToRetain:  input.BackupsToRetain,
		Token:            input.Token,
		Orgs:             input.Orgs,
		LogLevel:         input.LogLevel,
	}, nil
}

type GitHubHost struct {
	Caller           string
	httpClient       *retryablehttp.Client
	Provider         string
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	SkipUserRepos    bool
	BackupsToRetain  int
	Token            string
	Orgs             []string
	LogLevel         int
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

type githubQueryOrgsResponse struct {
	Data struct {
		Viewer struct {
			Organizations struct {
				Edges    []orgsEdge
				PageInfo struct {
					EndCursor   string
					HasNextPage bool
				}
			}
		}
	}
	Errors []struct {
		Type    string
		Path    []string
		Message string
	}
}
type orgsEdge struct {
	Node struct {
		Name string
	}
	Cursor string
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
	Errors []struct {
		Type    string
		Path    []string
		Message string
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

// describeGithubUserRepos returns a list of repositories owned by authenticated user
func (gh *GitHubHost) describeGithubUserRepos() []repository {
	logger.Println("listing GitHub user's owned repositories")

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
				Domain:            gitHubDomain,
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

func (gh *GitHubHost) describeGithubUserOrganizations() []githubOrganization {
	logger.Println("listing GitHub user's related Organizations")

	var orgs []githubOrganization

	reqBody := "{\"query\": \"{ viewer { organizations(first:100) { edges { node { name } } } } }\""

	bodyStr := gh.makeGithubRequest(reqBody)
	var respObj githubQueryOrgsResponse
	if err := json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
		logger.Fatal(err)
	}

	if len(respObj.Errors) > 0 {
		for _, queryError := range respObj.Errors {
			logger.Printf("failed to retrieve organizations user's a member of: %s", queryError.Message)
		}

		return nil
	}

	for _, org := range respObj.Data.Viewer.Organizations.Edges {
		orgs = append(orgs, githubOrganization{
			Name: org.Node.Name,
		})
	}

	return orgs
}

type githubOrganization struct {
	Name string `json:"name"`
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

		if respObj.Errors != nil {
			for _, gqlErr := range respObj.Errors {
				if gqlErr.Type == "NOT_FOUND" {
					logger.Printf("organization %s not found", orgName)
				} else {
					logger.Printf("unexpected error: type: %s message: %s", gqlErr.Type, gqlErr.Message)
				}
			}
		}

		for _, repo := range respObj.Data.Organization.Repositories.Edges {
			repos = append(repos, repository{
				Name:              repo.Node.Name,
				SSHUrl:            repo.Node.SSHURL,
				HTTPSUrl:          repo.Node.URL,
				PathWithNameSpace: repo.Node.NameWithOwner,
				Domain:            gitHubDomain,
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

func remove(s []string, r string) []string {
	for i, v := range s {
		if v == r {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func (gh *GitHubHost) describeRepos() describeReposOutput {
	var repos []repository
	if !gh.SkipUserRepos {
		// get authenticated user's owned repos
		repos = gh.describeGithubUserRepos()
	}

	// set orgs repos to retrieve to those specified when client constructed
	orgs := gh.Orgs

	// if we get a wildcard, get all orgs user belongs to
	if slices.Contains(gh.Orgs, "*") {
		// delete the wildcard, leaving any existing specified orgs that may have been passed in
		orgs = remove(orgs, "*")
		// get a list of orgs the authenticated user belongs to
		githubOrgs := gh.describeGithubUserOrganizations()

		for _, gho := range githubOrgs {
			orgs = append(orgs, gho.Name)
		}
	}

	// append repos belonging to any orgs specified
	for _, org := range orgs {
		repos = append(repos, gh.describeGithubOrgRepos(org)...)
	}

	return describeReposOutput{
		Repos: repos,
	}
}

func gitHubWorker(logLevel int, token, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(token, "\n"), repo.HTTPSUrl[firstPos+2:])
		results <- processBackup(logLevel, repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (gh *GitHubHost) Backup() {
	if gh.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return
	}

	maxConcurrent := 10
	repoDesc := gh.describeRepos()
	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go gitHubWorker(gh.LogLevel, gh.Token, gh.BackupDir, gh.DiffRemoteMethod, gh.BackupsToRetain, jobs, results)
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
