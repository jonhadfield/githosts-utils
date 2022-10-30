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
	"time"
)

const (
	gitHubCallSize = 100
)

type githubHost struct {
	Provider string
	APIURL   string
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

func makeGithubRequest(c *http.Client, payload string) string {
	contentReader := bytes.NewReader([]byte(payload))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*maxRequestTime)
	defer cancel()

	req, newReqErr := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", contentReader)

	if newReqErr != nil {
		logger.Fatal(newReqErr)
	}

	req.Header.Set("Authorization", fmt.Sprintf("bearer %s",
		stripTrailing(os.Getenv("GITHUB_TOKEN"), "\n")))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	resp, reqErr := c.Do(req)
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

func describeGithubUserRepos(c *http.Client) []repository {
	logger.Println("listing GitHub user's repositories")

	var repos []repository

	reqBody := "{\"query\": \"query { viewer { repositories(first:" + strconv.Itoa(gitHubCallSize) + ") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }} } }\""

	for {
		bodyStr := makeGithubRequest(c, reqBody)

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
			reqBody = "{\"query\": \"query($first:Int $after:String){ viewer { repositories(first:$first after:$after) { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }} } }\", \"variables\":{\"first\":" + strconv.Itoa(gitHubCallSize) + ",\"after\":\"" + respObj.Data.Viewer.Repositories.PageInfo.EndCursor + "\"} }"
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

func describeGithubOrgRepos(c *http.Client, orgName string) []repository {
	logger.Printf("listing GitHub organisation %s's repositories", orgName)

	var repos []repository

	reqBody := "query { organization(login: \"" + orgName + "\") { repositories(first:" + strconv.Itoa(gitHubCallSize) + ") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }}}}"

	for {
		bodyStr := makeGithubRequest(c, createGithubRequestPayload(reqBody))

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
			reqBody = "{\"query\": \"query($first:Int $after:String){ viewer { repositories(first:$first after:$after) { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }} } }\", \"variables\":{\"first\":" + strconv.Itoa(gitHubCallSize) + ",\"after\":\"" + respObj.Data.Organization.Repositories.PageInfo.EndCursor + "\"} }"
		}
	}

	return repos
}

func (provider githubHost) describeRepos() describeReposOutput {
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout * time.Second,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	repos := describeGithubUserRepos(client)

	if len(strings.TrimSpace(os.Getenv("GITHUB_ORGS"))) > 0 {
		orgs := strings.Split(os.Getenv("GITHUB_ORGS"), ",")
		for _, org := range orgs {
			repos = append(repos, describeGithubOrgRepos(client, org)...)
		}
	}

	return describeReposOutput{
		Repos: repos,
	}
}

func (provider githubHost) getAPIURL() string {
	return provider.APIURL
}

func gitHubWorker(backupDIR string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(os.Getenv("GITHUB_TOKEN"), "\n"), repo.HTTPSUrl[firstPos+2:])
		results <- processBackup(repo, backupDIR, backupsToKeep)
	}
}

func (provider githubHost) Backup(backupDIR string) {
	maxConcurrent := 5
	repoDesc := provider.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	backupsToKeep, err := strconv.Atoi(os.Getenv("GITHUB_BACKUPS"))
	if err != nil {
		backupsToKeep = 0
	}

	for w := 1; w <= maxConcurrent; w++ {
		go gitHubWorker(backupDIR, backupsToKeep, jobs, results)
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
