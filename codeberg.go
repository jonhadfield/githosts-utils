//nolint:wsl_v5 // extensive whitespace linting would require significant refactoring
package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"gitlab.com/tozd/go/errors"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterhellberg/link"
)

const (
	codebergProviderName        = "Codeberg"
	codebergEnvVarWorkerDelay   = "CODEBERG_WORKER_DELAY"
	codebergEnvVarMaxConcurrent = "CODEBERG_MAX_CONCURRENT"
	// Codeberg is a donation funded public host, so default to a lower
	// concurrency and a longer inter-repository delay than self-hosted Gitea.
	codebergDefaultWorkerDelay = 1000
	codebergMaxConcurrency     = 5
	codebergReposPerPage       = 50
	// codebergOrgWildcard expands to the organisations the token belongs to.
	codebergOrgWildcard = "*"
)

// NewCodebergHostInput configures a Codeberg host.
//
// Codeberg runs Forgejo, which serves the same /api/v1 surface as Gitea, but as
// a shared public instance rather than one you administer. The Gitea host
// enumerates repositories via the instance-admin endpoints (/admin/users and
// /orgs), which a Codeberg personal access token cannot reach; this host uses
// the token-scoped endpoints (/user/repos and /user/orgs) instead.
type NewCodebergHostInput struct {
	Caller           string
	HTTPClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
	// Orgs limits organisation backups to the named organisations. An entry of
	// "*" expands to every organisation the token belongs to, and can be
	// combined with names of organisations it does not.
	Orgs []string
	// SkipUserRepos omits /user/repos, backing up only the repositories owned
	// by the organisations named in Orgs.
	SkipUserRepos bool
	// LimitUserOwned restricts /user/repos results to repositories owned by the
	// authenticated user, excluding those they merely have access to.
	LimitUserOwned       bool
	BackupsToRetain      int
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
}

type CodebergHost struct {
	Caller               string
	httpClient           *retryablehttp.Client
	Provider             string
	APIURL               string
	DiffRemoteMethod     string
	BackupDir            string
	Token                string
	Orgs                 []string
	SkipUserRepos        bool
	LimitUserOwned       bool
	BackupsToRetain      int
	LogLevel             int
	BackupLFS            bool
	EncryptionPassphrase string
}

func NewCodebergHost(input NewCodebergHostInput) (*CodebergHost, error) {
	setLoggerPrefix(input.Caller)

	apiURL := codebergAPIURL
	if input.APIURL != "" {
		apiURL = input.APIURL
	}

	diffRemoteMethod, err := getDiffRemoteMethod(input.DiffRemoteMethod)
	if err != nil {
		return nil, err
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

	return &CodebergHost{
		Caller:               input.Caller,
		httpClient:           httpClient,
		Provider:             codebergProviderName,
		APIURL:               strings.TrimSuffix(apiURL, "/"),
		DiffRemoteMethod:     diffRemoteMethod,
		BackupDir:            input.BackupDir,
		Token:                input.Token,
		Orgs:                 input.Orgs,
		SkipUserRepos:        input.SkipUserRepos,
		LimitUserOwned:       input.LimitUserOwned,
		BackupsToRetain:      input.BackupsToRetain,
		LogLevel:             input.LogLevel,
		BackupLFS:            input.BackupLFS,
		EncryptionPassphrase: input.EncryptionPassphrase,
	}, nil
}

func (cb *CodebergHost) getAPIURL() string {
	return cb.APIURL
}

// return normalised method.
func (cb *CodebergHost) diffRemoteMethod() string {
	return canonicalDiffRemoteMethod(cb.DiffRemoteMethod)
}

func (cb *CodebergHost) makeCodebergRequest(reqUrl string) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to request %s: %w", reqUrl, err)
	}

	req.Header.Set(HeaderAuthorization, AuthPrefixToken+cb.Token)
	req.Header.Set(HeaderContentType, contentTypeApplicationJSON)
	req.Header.Set(HeaderAccept, contentTypeApplicationJSON)

	resp, err := cb.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to request %s: %w", reqUrl, err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()

		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	body = bytes.ReplaceAll(body, []byte("\r"), []byte("\r\n"))

	_ = resp.Body.Close()

	return resp, body, nil
}

func (cb *CodebergHost) handleCodebergAPIResponse(resp *http.Response, resource string) errors.E {
	switch resp.StatusCode {
	case http.StatusOK:
		if cb.LogLevel > 0 {
			logger.Printf("%s retrieved successfully", resource)
		}

		return nil
	case http.StatusUnauthorized:
		logger.Printf("failed to get %s due to invalid or missing credentials (HTTP 401)", resource)

		return errors.Errorf("unauthorized response to Codeberg request for %s", resource)
	case http.StatusForbidden:
		logger.Printf("failed to get %s due to insufficient token scope (HTTP 403)", resource)

		return errors.Errorf("forbidden response to Codeberg request for %s", resource)
	default:
		logger.Printf("failed to get %s with unexpected response: %d (%s)", resource, resp.StatusCode, resp.Status)

		return errors.Errorf("unexpected response making Codeberg request for %s: %d (%s)", resource, resp.StatusCode, resp.Status)
	}
}

// paginateCodebergAPI walks a Forgejo collection endpoint, following the
// rel="next" link header until exhausted, handing each page's body to process.
func (cb *CodebergHost) paginateCodebergAPI(baseURL, resource string, process func([]byte) error) errors.E {
	if strings.TrimSpace(cb.APIURL) == "" {
		return errors.Errorf("%s API URL missing", codebergProviderName)
	}

	if cb.LogLevel > 0 {
		logger.Printf("get %s url: %s", resource, baseURL)
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return errors.Errorf("failed to parse get %s URL %s: %s", resource, baseURL, err)
	}

	q := u.Query()
	q.Set("limit", strconv.Itoa(codebergReposPerPage))
	u.RawQuery = q.Encode()

	reqUrl := u.String()

	for reqUrl != "" {
		resp, body, reqErr := cb.makeCodebergRequest(reqUrl) //nolint:bodyclose // response body is closed in makeCodebergRequest
		if reqErr != nil {
			return errors.Errorf("failed to make Codeberg request for %s: %s", resource, reqErr)
		}

		if cb.LogLevel > 0 {
			logger.Print(string(body))
		}

		if respErr := cb.handleCodebergAPIResponse(resp, resource); respErr != nil {
			return respErr
		}

		if procErr := process(body); procErr != nil {
			return errors.Errorf("failed to process %s response: %s", resource, procErr)
		}

		reqUrl = ""

		for _, l := range link.ParseResponse(resp) {
			if l.Rel == txtNext {
				reqUrl = l.URI
			}
		}
	}

	return nil
}

// codebergRepositoriesToRepos converts Forgejo repository responses into the
// internal repository type, deriving the domain from each clone URL so bundles
// are written beneath the host the repository actually came from.
func codebergRepositoriesToRepos(in []giteaRepository) ([]repository, errors.E) {
	var repos []repository

	for _, r := range in {
		ru, err := url.Parse(r.CloneUrl)
		if err != nil {
			return nil, errors.Errorf("failed to parse clone url for %s: %s", r.Name, err)
		}

		repos = append(repos, repository{
			Name:              r.Name,
			Owner:             r.Owner.Login,
			HTTPSUrl:          r.CloneUrl,
			SSHUrl:            r.SshUrl,
			Domain:            ru.Host,
			PathWithNameSpace: r.FullName,
		})
	}

	return repos, nil
}

// getAuthenticatedUser returns the login of the token's owner.
func (cb *CodebergHost) getAuthenticatedUser() (string, errors.E) {
	resp, body, err := cb.makeCodebergRequest(cb.APIURL + "/user") //nolint:bodyclose // response body is closed in makeCodebergRequest
	if err != nil {
		return "", errors.Errorf("failed to make Codeberg request for authenticated user: %s", err)
	}

	if respErr := cb.handleCodebergAPIResponse(resp, "authenticated user"); respErr != nil {
		return "", respErr
	}

	var user giteaUser
	if uErr := json.Unmarshal(body, &user); uErr != nil {
		return "", errors.Errorf("failed to unmarshal authenticated user json response: %s", uErr)
	}

	if user.Login == "" {
		return "", errors.New("Codeberg returned an empty login for the authenticated user")
	}

	return user.Login, nil
}

// getUserRepos returns the repositories accessible to the token via
// /user/repos, which covers repositories the user owns plus any they are a
// collaborator on, including those owned by their organisations.
func (cb *CodebergHost) getUserRepos() ([]repository, errors.E) {
	logger.Println("retrieving repositories for authenticated user")

	var owner string

	if cb.LimitUserOwned {
		var err errors.E

		owner, err = cb.getAuthenticatedUser()
		if err != nil {
			return nil, err
		}
	}

	var giteaRepos []giteaRepository

	err := cb.paginateCodebergAPI(cb.APIURL+"/user/repos", "user repositories", func(body []byte) error {
		var respObj []giteaRepository
		if uErr := json.Unmarshal(body, &respObj); uErr != nil {
			return uErr //nolint:wrapcheck // error context is added by the caller
		}

		for _, r := range respObj {
			if cb.LimitUserOwned && !strings.EqualFold(r.Owner.Login, owner) {
				if cb.LogLevel > 0 {
					logger.Printf("skipping %s as it is not owned by %s", r.FullName, owner)
				}

				continue
			}

			giteaRepos = append(giteaRepos, r)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return codebergRepositoriesToRepos(giteaRepos)
}

// getOrganizations resolves the configured organisation names. "*" expands to
// the organisations the token belongs to, via /user/orgs, and is combined with
// any organisations named alongside it, so the wildcard can be used to top up
// an explicit list rather than replace it. The Gitea host's /orgs endpoint is
// deliberately not used here: on a public instance it lists every organisation
// on the host, not the caller's.
func (cb *CodebergHost) getOrganizations() ([]string, errors.E) {
	if len(cb.Orgs) == 0 {
		if cb.LogLevel > 0 {
			logger.Print("no organizations specified")
		}

		return nil, nil
	}

	var names []string

	seen := make(map[string]struct{}, len(cb.Orgs))

	// add records an organisation once, so an organisation both named and
	// returned by the wildcard expansion isn't listed twice.
	add := func(name string) {
		if name == "" {
			return
		}

		if _, ok := seen[name]; ok {
			return
		}

		seen[name] = struct{}{}

		names = append(names, name)
	}

	for _, o := range cb.Orgs {
		if o != codebergOrgWildcard {
			add(o)
		}
	}

	if !slices.Contains(cb.Orgs, codebergOrgWildcard) {
		return names, nil
	}

	logger.Println("retrieving organizations for authenticated user")

	err := cb.paginateCodebergAPI(cb.APIURL+"/user/orgs", "user organizations", func(body []byte) error {
		var respObj []giteaOrganization
		if uErr := json.Unmarshal(body, &respObj); uErr != nil {
			return uErr //nolint:wrapcheck // error context is added by the caller
		}

		for _, o := range respObj {
			add(o.Username)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

func (cb *CodebergHost) getOrganizationsRepos(organizations []string) ([]repository, errors.E) {
	var repos []repository

	for _, org := range organizations {
		if cb.LogLevel > 0 {
			logger.Printf("getting repositories from Codeberg organization %s", org)
		}

		var giteaRepos []giteaRepository

		err := cb.paginateCodebergAPI(cb.APIURL+"/orgs/"+org+"/repos", "organization repositories", func(body []byte) error {
			var respObj []giteaRepository
			if uErr := json.Unmarshal(body, &respObj); uErr != nil {
				return uErr //nolint:wrapcheck // error context is added by the caller
			}

			giteaRepos = append(giteaRepos, respObj...)

			return nil
		})
		if err != nil {
			return nil, errors.Errorf("failed to get organization %s repos: %s", org, err)
		}

		orgRepos, err := codebergRepositoriesToRepos(giteaRepos)
		if err != nil {
			return nil, err
		}

		repos = append(repos, orgRepos...)
	}

	return repos, nil
}

// dedupeRepos removes repositories that appear more than once. /user/repos
// already includes repositories owned by the caller's organisations, so listing
// those organisations explicitly would otherwise queue the same repository
// twice and have two workers clone into the same working directory.
func dedupeRepos(in []repository) []repository {
	seen := make(map[string]struct{}, len(in))

	var out []repository

	for _, r := range in {
		key := r.Domain + "/" + r.PathWithNameSpace
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}

		out = append(out, r)
	}

	return out
}

func (cb *CodebergHost) describeRepos() (describeReposOutput, errors.E) {
	logger.Println("listing repositories")

	var repos []repository

	if !cb.SkipUserRepos {
		userRepos, err := cb.getUserRepos()
		if err != nil {
			return describeReposOutput{}, errors.Errorf("failed to get user repositories: %s", err)
		}

		repos = append(repos, userRepos...)
	}

	orgs, err := cb.getOrganizations()
	if err != nil {
		return describeReposOutput{}, errors.Errorf("failed to get organizations: %s", err)
	}

	if len(orgs) > 0 {
		orgsRepos, orgErr := cb.getOrganizationsRepos(orgs)
		if orgErr != nil {
			return describeReposOutput{}, errors.Errorf("failed to get organizations repos: %s", orgErr)
		}

		repos = append(repos, orgsRepos...)
	}

	return describeReposOutput{
		Repos: dedupeRepos(repos),
	}, nil
}

func (cb *CodebergHost) Backup() ProviderBackupResult {
	if cb.BackupDir == "" {
		logger.Print(msgBackupSkippedNoDir)

		return ProviderBackupResult{
			BackupResults: nil,
			Error:         errors.New(msgBackupDirNotSpecified),
		}
	}

	repoDesc, err := cb.describeRepos()
	if err != nil {
		return ProviderBackupResult{
			BackupResults: nil,
			Error:         err,
		}
	}

	maxConcurrent := maxConcurrentFromEnv(codebergEnvVarMaxConcurrent, codebergMaxConcurrency)

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan RepoBackupResults, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go genericWorker(WorkerConfig{
			LogLevel:         cb.LogLevel,
			BackupDir:        cb.BackupDir,
			DiffRemoteMethod: cb.diffRemoteMethod(),
			BackupsToKeep:    cb.BackupsToRetain,
			BackupLFS:        cb.BackupLFS,
			DefaultDelay:     codebergDefaultWorkerDelay,
			DelayEnvVar:      codebergEnvVarWorkerDelay,
			Secrets:          []string{cb.Token},
			SetupRepo: func(repo *repository) {
				repo.URLWithToken = urlWithToken(repo.HTTPSUrl, cb.Token)
			},
			EncryptionPassphrase: cb.EncryptionPassphrase,
		}, jobs, results)
	}

	for x := range repoDesc.Repos {
		repo := repoDesc.Repos[x]
		jobs <- repo
	}

	close(jobs)

	var providerBackupResults ProviderBackupResult

	for a := 1; a <= len(repoDesc.Repos); a++ {
		res := <-results
		if res.Error != nil {
			logger.Printf("backup failed: %+v\n", res.Error)
		}

		providerBackupResults.BackupResults = append(providerBackupResults.BackupResults, res)
	}

	return providerBackupResults
}
