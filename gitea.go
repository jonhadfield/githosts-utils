package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/peterhellberg/link"
	"golang.org/x/exp/slices"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	giteaUsersPerPageDefault         = 20
	giteaUsersLimit                  = -1
	giteaOrganizationsPerPageDefault = 20
	giteaOrganizationsLimit          = -1
	giteaReposPerPageDefault         = 20
	giteaReposLimit                  = -1
	giteaEnvVarAPIUrl                = "GITEA_APIURL"
	// giteaEnvVarToken                 = "GITEA_TOKEN"
	giteaMatchByExact     = "exact"
	giteaMatchByIfDefined = "anyDefined"
	giteaProviderName     = "Gitea"
)

type NewGiteaHostInput struct {
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	Token            string
	Orgs             []string
	BackupsToRetain  int
	LogLevel         int
}

type HostsConfig struct {
	httpClient *retryablehttp.Client
	debug      bool
}

type GiteaHost struct {
	httpClient       *retryablehttp.Client
	APIURL           string
	DiffRemoteMethod string
	BackupDir        string
	BackupsToRetain  int
	Token            string
	Orgs             []string
	LogLevel         int
}

func NewGiteaHost(input NewGiteaHostInput) (host *GiteaHost, err error) {
	diffRemoteMethod := cloneMethod
	if input.DiffRemoteMethod != "" {
		if !validDiffRemoteMethod(input.DiffRemoteMethod) {
			return nil, fmt.Errorf("invalid diff remote method: %s", input.DiffRemoteMethod)
		}

		diffRemoteMethod = input.DiffRemoteMethod
	}

	return &GiteaHost{
		httpClient:       getHTTPClient(),
		APIURL:           input.APIURL,
		DiffRemoteMethod: diffRemoteMethod,
		BackupDir:        input.BackupDir,
		BackupsToRetain:  input.BackupsToRetain,
		Token:            input.Token,
		Orgs:             input.Orgs,
		LogLevel:         input.LogLevel,
	}, nil
}

type giteaUser struct {
	ID                int    `json:"id"`
	Login             string `json:"login"`
	LoginName         string `json:"login_name"`
	FullName          string `json:"full_name"`
	Email             string `json:"email"`
	AvatarURL         string `json:"avatar_url"`
	Language          string `json:"language"`
	IsAdmin           bool   `json:"is_admin"`
	LastLogin         string `json:"last_login"`
	Created           string `json:"created"`
	Restricted        bool   `json:"restricted"`
	Active            bool   `json:"active"`
	ProhibitLogin     bool   `json:"prohibit_login"`
	Location          string `json:"location"`
	Website           string `json:"website"`
	Description       string `json:"description"`
	Visibility        string `json:"visibility"`
	FollowersCount    int    `json:"followers_count"`
	FollowingCount    int    `json:"following_count"`
	StarredReposCount int    `json:"starred_repos_count"`
	Username          string `json:"username"`
}

type giteaOrganization struct {
	ID                       int    `json:"id"`
	Name                     string `json:"name"`
	FullName                 string `json:"full_name"`
	AvatarURL                string `json:"avatar_url"`
	Description              string `json:"description"`
	Website                  string `json:"website"`
	Location                 string `json:"location"`
	Visibility               string `json:"visibility"`
	RepoAdminChangeTeamAcces bool   `json:"repo_admin_change_team_access"`
	Username                 string `json:"username"`
}

type giteaGetUsersResponse []giteaUser
type giteaGetOrganizationsResponse []giteaOrganization

func (g *GiteaHost) makeGiteaRequest(reqUrl string) (resp *http.Response, body []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	// var req *http.Request

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", os.Getenv("GITEA_TOKEN")))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	resp, err = g.httpClient.Do(req)
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

type repoExistsInput struct {
	matchBy           string // anyDefined, allDefined, exact
	repos             []repository
	name              string
	owner             string
	pathWithNamespace string
	domain            string
	httpsUrl          string
	sshUrl            string
	urlWithToken      string
	urlWithBasicAuth  string
	logLevel          int
}

type userExistsInput struct {
	matchBy   string // anyDefined, allDefined, exact
	users     []giteaUser
	id        int
	login     string
	loginName string
	email     string
	fullName  string
}
type organisationExistsInput struct {
	matchBy       string // anyDefined, allDefined, exact
	organisations []giteaOrganization
	name          string
	fullName      string
}

func allTrue(in ...bool) bool {
	for _, v := range in {
		if !v {
			return false
		}
	}

	return true
}

func repoExists(in repoExistsInput) bool {
	switch in.matchBy {
	case giteaMatchByExact:
		if in.logLevel > 0 {
			logger.Printf("matchBy %s", giteaMatchByExact)
		}
	case giteaMatchByIfDefined:
		if in.logLevel > 0 {
			logger.Printf("matchBy %s", giteaMatchByExact)
		}
	case "":
		if in.logLevel > 0 {
			logger.Printf("matchBy not defined")
		}
		return false
	default:
		logger.Printf("unexpected matchBy value %s", in.matchBy)

		return false
	}

	if in.matchBy == "" {
		if in.logLevel > 0 {
			logger.Printf("matchBy not defined, defaulting to %s", giteaMatchByExact)
		}
	}

	if len(in.repos) == 0 {

		return false
	}

	for _, r := range in.repos {
		nameMatch := in.name == r.Name
		ownerMatch := in.owner == r.Owner
		domainMatch := in.domain == r.Domain
		cloneUrlMatch := in.httpsUrl == r.HTTPSUrl
		sshUrlMatch := in.sshUrl == r.SSHUrl
		urlWithTokenMatch := in.urlWithToken == r.URLWithToken
		urlWithBasicAuthMatch := in.urlWithBasicAuth == r.URLWithBasicAuth
		pathWithNamespaceMatch := in.pathWithNamespace == r.PathWithNameSpace

		switch in.matchBy {
		case giteaMatchByExact:
			if allTrue(nameMatch, domainMatch, ownerMatch, cloneUrlMatch, sshUrlMatch, urlWithTokenMatch,
				urlWithBasicAuthMatch, pathWithNamespaceMatch) {

				return true
			}

			continue
		case giteaMatchByIfDefined:
			anyDefined := in.name != "" || in.domain != "" || in.owner != "" || in.httpsUrl != "" || in.sshUrl != ""
			switch {
			case in.name != "" && !nameMatch:
				continue
			case in.domain != "" && !domainMatch:
				continue
			case in.owner != "" && !ownerMatch:
				continue
			case in.httpsUrl != "" && !cloneUrlMatch:
				continue
			case in.sshUrl != "" && !sshUrlMatch:
				continue
			case in.urlWithToken != "" && !urlWithTokenMatch:
				continue
			case in.urlWithBasicAuth != "" && !urlWithBasicAuthMatch:
				continue
			case in.pathWithNamespace != "" && !pathWithNamespaceMatch:
				continue
			default:
				if anyDefined {
					return true
				}

				continue
			}
		}
	}

	return false
}

func userExists(in userExistsInput) bool {
	for _, u := range in.users {
		loginMatch := in.login == u.Login
		idMatch := in.id == u.ID
		loginNameMatch := in.loginName == u.LoginName
		emailMatch := in.email == u.Email
		fullNameMatch := in.fullName == u.FullName

		switch in.matchBy {
		case giteaMatchByExact:
			if allTrue(loginMatch, loginNameMatch, idMatch, emailMatch, fullNameMatch) {
				return true
			}

			continue
		case giteaMatchByIfDefined:
			anyDefined := in.login != "" || in.id != 0 || in.loginName != "" || in.email != "" || in.fullName != ""
			switch {
			case in.login != "" && !loginMatch:
				continue
			case in.id != 0 && !idMatch:
				continue
			case in.loginName != "" && !loginNameMatch:
				continue
			case in.email != "" && !emailMatch:
				continue
			case in.fullName != "" && !fullNameMatch:
				continue
			default:
				if anyDefined {
					return true
				}

				continue
			}
		}
	}

	return false
}

func organisationExists(in organisationExistsInput) bool {
	for _, o := range in.organisations {
		nameMatch := in.name == o.Name
		fullNameMatch := in.fullName == o.FullName

		switch in.matchBy {
		case giteaMatchByExact:
			if allTrue(nameMatch, fullNameMatch) {
				return true
			}

			continue
		case giteaMatchByIfDefined:
			switch {
			case in.name != "" && !nameMatch:
				continue
			case in.fullName != "" && !fullNameMatch:
				continue
			}

			return true
		}
	}

	return false
}

func (g *GiteaHost) describeRepos() describeReposOutput {
	logger.Println("listing repositories")

	userRepos := g.getAllUserRepositories()

	orgs := g.getOrganizations()

	var orgsRepos []repository
	if len(orgs) > 0 {
		orgsRepos = g.getOrganizationsRepos(orgs)
	}

	return describeReposOutput{
		Repos: append(userRepos, orgsRepos...),
	}
}

func extractDomainFromAPIUrl(apiUrl string) string {
	u, err := url.Parse(apiUrl)
	if err != nil {
		logger.Printf("failed to parse apiUrl %s: %v", apiUrl, err)
	}

	return u.Hostname()
}

func (g *GiteaHost) getOrganizationsRepos(organizations []giteaOrganization) (repos []repository) {
	domain := extractDomainFromAPIUrl(g.APIURL)

	for _, org := range organizations {
		if g.LogLevel > 0 {
			logger.Printf("getting repositories from gitea organization %s", org.Name)
		}

		orgRepos := g.getOrganizationRepos(org.Name)
		for _, orgRepo := range orgRepos {
			repos = append(repos, repository{
				Name:              orgRepo.Name,
				Owner:             orgRepo.Owner.Login,
				HTTPSUrl:          orgRepo.CloneUrl,
				SSHUrl:            orgRepo.SshUrl,
				PathWithNameSpace: orgRepo.FullName,
				Domain:            domain,
			})
		}
	}

	return repos
}

func (g *GiteaHost) getAllUsers() (users []giteaUser) {
	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	getUsersURL := g.APIURL + "/admin/users"
	if g.LogLevel > 0 {
		logger.Printf("get users url: %s", getUsersURL)
	}

	// Initial request
	u, err := url.Parse(getUsersURL)
	if err != nil {
		return
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(giteaUsersPerPageDefault))
	q.Set("limit", strconv.Itoa(giteaUsersLimit))
	u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	for {
		var resp *http.Response
		resp, body, err = g.makeGiteaRequest(reqUrl)
		if err != nil {
			return
		}

		if g.LogLevel > 0 {
			logger.Printf(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if g.LogLevel > 0 {
				logger.Println("users retrieved successfully")
			}
		case http.StatusForbidden:
			logger.Println("failed to get users due to invalid or missing credentials (HTTP 403)")

			return users
		default:
			logger.Printf("failed to get users with unexpected response: %d (%s)", resp.StatusCode, resp.Status)

			return users
		}

		var respObj giteaGetUsersResponse

		if err = json.Unmarshal(body, &respObj); err != nil {
			logger.Fatal(err)
		}

		users = append(users, respObj...)
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

	return users
}

func (g *GiteaHost) getOrganizations() (organizations []giteaOrganization) {
	if len(g.Orgs) == 0 {
		if g.LogLevel > 0 {
			logger.Print("no organizations specified")
		}

		return
	}

	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	if slices.Contains(g.Orgs, "*") {
		organizations = g.getAllOrganizations()
	} else {
		for _, orgName := range g.Orgs {
			organizations = append(organizations, g.getOrganization(orgName))
		}
	}

	return organizations
}

func (g *GiteaHost) getOrganization(orgName string) (organization giteaOrganization) {
	if g.LogLevel > 0 {
		logger.Printf("retrieving organization %s", orgName)
	}

	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	getOrganizationsURL := fmt.Sprintf("%s%s", g.APIURL+"/orgs/", orgName)
	if g.LogLevel > 0 {
		logger.Printf("get organization url: %s", getOrganizationsURL)
	}

	// Initial request
	u, err := url.Parse(getOrganizationsURL)
	if err != nil {
		return
	}

	// u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	var resp *http.Response
	resp, body, err = g.makeGiteaRequest(reqUrl)
	if err != nil {
		return
	}

	if g.LogLevel > 0 {
		logger.Print(string(body))
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if g.LogLevel > 0 {
			logger.Println("organisations retrieved successfully")
		}
	case http.StatusForbidden:
		logger.Println("failed to get organisations due to invalid or missing credentials (HTTP 403)")

		return organization
	default:
		logger.Printf("failed to get organisations with unexpected response: %d (%s)", resp.StatusCode, resp.Status)

		return organization
	}

	if err = json.Unmarshal(body, &organization); err != nil {
		logger.Fatal(err)
	}

	// if we got a link response then
	// reset request url
	// link: <https://gitea.lessknown.co.uk/api/v1/admin/organisations?limit=2&page=2>; rel="next",<https://gitea.lessknown.co.uk/api/v1/admin/organisations?limit=2&page=2>; rel="last"

	return organization
}

func (g *GiteaHost) getAllOrganizations() (organizations []giteaOrganization) {
	logger.Printf("retrieving organizations")

	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	getOrganizationsURL := g.APIURL + "/orgs"
	if g.LogLevel > 0 {
		logger.Printf("get organizations url: %s", getOrganizationsURL)
	}

	// Initial request
	u, err := url.Parse(getOrganizationsURL)
	if err != nil {
		return
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(giteaOrganizationsPerPageDefault))
	q.Set("limit", strconv.Itoa(giteaOrganizationsLimit))
	u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	for {
		var resp *http.Response
		resp, body, err = g.makeGiteaRequest(reqUrl)
		if err != nil {
			return
		}

		if g.LogLevel > 0 {
			logger.Print(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if g.LogLevel > 0 {
				logger.Println("organisations retrieved successfully")
			}
		case http.StatusForbidden:
			logger.Println("failed to get organisations due to invalid or missing credentials (HTTP 403)")

			return organizations
		default:
			logger.Printf("failed to get organisations with unexpected response: %d (%s)", resp.StatusCode, resp.Status)

			return organizations
		}

		var respObj giteaGetOrganizationsResponse

		if err = json.Unmarshal(body, &respObj); err != nil {
			logger.Fatal(err)
		}

		organizations = append(organizations, respObj...)

		// if we got a link response then
		// reset request url
		// link: <https://gitea.lessknown.co.uk/api/v1/admin/organisations?limit=2&page=2>; rel="next",<https://gitea.lessknown.co.uk/api/v1/admin/organisations?limit=2&page=2>; rel="last"
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

	return organizations
}

type giteaRepository struct {
	Id    int `json:"id"`
	Owner struct {
		Id                int       `json:"id"`
		Login             string    `json:"login"`
		LoginName         string    `json:"login_name"`
		FullName          string    `json:"full_name"`
		Email             string    `json:"email"`
		AvatarUrl         string    `json:"avatar_url"`
		Language          string    `json:"language"`
		IsAdmin           bool      `json:"is_admin"`
		LastLogin         time.Time `json:"last_login"`
		Created           time.Time `json:"created"`
		Restricted        bool      `json:"restricted"`
		Active            bool      `json:"active"`
		ProhibitLogin     bool      `json:"prohibit_login"`
		Location          string    `json:"location"`
		Website           string    `json:"website"`
		Description       string    `json:"description"`
		Visibility        string    `json:"visibility"`
		FollowersCount    int       `json:"followers_count"`
		FollowingCount    int       `json:"following_count"`
		StarredReposCount int       `json:"starred_repos_count"`
		Username          string    `json:"username"`
	} `json:"owner"`
	Name            string      `json:"name"`
	FullName        string      `json:"full_name"`
	Description     string      `json:"description"`
	Empty           bool        `json:"empty"`
	Private         bool        `json:"private"`
	Fork            bool        `json:"fork"`
	Template        bool        `json:"template"`
	Parent          interface{} `json:"parent"`
	Mirror          bool        `json:"mirror"`
	Size            int         `json:"size"`
	Language        string      `json:"language"`
	LanguagesUrl    string      `json:"languages_url"`
	HtmlUrl         string      `json:"html_url"`
	Link            string      `json:"link"`
	SshUrl          string      `json:"ssh_url"`
	CloneUrl        string      `json:"clone_url"`
	OriginalUrl     string      `json:"original_url"`
	Website         string      `json:"website"`
	StarsCount      int         `json:"stars_count"`
	ForksCount      int         `json:"forks_count"`
	WatchersCount   int         `json:"watchers_count"`
	OpenIssuesCount int         `json:"open_issues_count"`
	OpenPrCounter   int         `json:"open_pr_counter"`
	ReleaseCounter  int         `json:"release_counter"`
	DefaultBranch   string      `json:"default_branch"`
	Archived        bool        `json:"archived"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	ArchivedAt      time.Time   `json:"archived_at"`
	Permissions     struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
	HasIssues       bool `json:"has_issues"`
	InternalTracker struct {
		EnableTimeTracker                bool `json:"enable_time_tracker"`
		AllowOnlyContributorsToTrackTime bool `json:"allow_only_contributors_to_track_time"`
		EnableIssueDependencies          bool `json:"enable_issue_dependencies"`
	} `json:"internal_tracker"`
	HasWiki                       bool        `json:"has_wiki"`
	HasPullRequests               bool        `json:"has_pull_requests"`
	HasProjects                   bool        `json:"has_projects"`
	HasReleases                   bool        `json:"has_releases"`
	HasPackages                   bool        `json:"has_packages"`
	HasActions                    bool        `json:"has_actions"`
	IgnoreWhitespaceConflicts     bool        `json:"ignore_whitespace_conflicts"`
	AllowMergeCommits             bool        `json:"allow_merge_commits"`
	AllowRebase                   bool        `json:"allow_rebase"`
	AllowRebaseExplicit           bool        `json:"allow_rebase_explicit"`
	AllowSquashMerge              bool        `json:"allow_squash_merge"`
	AllowRebaseUpdate             bool        `json:"allow_rebase_update"`
	DefaultDeleteBranchAfterMerge bool        `json:"default_delete_branch_after_merge"`
	DefaultMergeStyle             string      `json:"default_merge_style"`
	DefaultAllowMaintainerEdit    bool        `json:"default_allow_maintainer_edit"`
	AvatarUrl                     string      `json:"avatar_url"`
	Internal                      bool        `json:"internal"`
	MirrorInterval                string      `json:"mirror_interval"`
	MirrorUpdated                 time.Time   `json:"mirror_updated"`
	RepoTransfer                  interface{} `json:"repo_transfer"`
}

func (g *GiteaHost) getOrganizationRepos(organizationName string) (repos []giteaRepository) {
	logger.Printf("retrieving repositories for organization %s", organizationName)

	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	getOrganizationReposURL := g.APIURL + fmt.Sprintf("/orgs/%s/repos", organizationName)
	if g.LogLevel > 0 {
		logger.Printf("get %s organization repos url: %s", organizationName, getOrganizationReposURL)
	}

	// Initial request
	u, err := url.Parse(getOrganizationReposURL)
	if err != nil {
		return
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(giteaReposPerPageDefault))
	q.Set("limit", strconv.Itoa(giteaReposLimit))
	u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	for {
		var resp *http.Response
		resp, body, err = g.makeGiteaRequest(reqUrl)
		if err != nil {
			return
		}

		if g.LogLevel > 0 {
			logger.Print(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if g.LogLevel > 0 {
				logger.Println("repos retrieved successfully")
			}
		case http.StatusForbidden:
			logger.Println("failed to get repos due to invalid or missing credentials (HTTP 403)")

			return repos
		default:
			logger.Printf("failed to get repos with unexpected response: %d (%s)", resp.StatusCode, resp.Status)

			return repos
		}

		var respObj []giteaRepository

		if err = json.Unmarshal(body, &respObj); err != nil {
			logger.Fatal(err)
		}

		repos = append(repos, respObj...)

		// if we got a link response then
		// reset request url
		// link: <https://gitea.lessknown.co.uk/api/v1/admin/repos?limit=2&page=2>; rel="next",<https://gitea.lessknown.co.uk/api/v1/admin/repos?limit=2&page=2>; rel="last"
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

func (g *GiteaHost) getAllUserRepos(userName string) (repos []repository) {
	logger.Printf("retrieving all repositories for user %s", userName)

	if strings.TrimSpace(g.APIURL) == "" {
		g.APIURL = gitlabAPIURL
	}

	getOrganizationReposURL := g.APIURL + fmt.Sprintf("/users/%s/repos", userName)
	if g.LogLevel > 0 {
		logger.Printf("get %s user repos url: %s", userName, getOrganizationReposURL)
	}

	// Initial request
	u, err := url.Parse(getOrganizationReposURL)
	if err != nil {
		return
	}

	q := u.Query()
	// set initial max per page
	q.Set("per_page", strconv.Itoa(giteaReposPerPageDefault))
	q.Set("limit", strconv.Itoa(giteaReposLimit))
	u.RawQuery = q.Encode()
	var body []byte

	reqUrl := u.String()
	for {
		var resp *http.Response
		resp, body, err = g.makeGiteaRequest(reqUrl)
		if err != nil {
			return
		}

		if g.LogLevel > 0 {
			logger.Print(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if g.LogLevel > 0 {
				logger.Println("repos retrieved successfully")
			}
		case http.StatusForbidden:
			logger.Println("failed to get repos due to invalid or missing credentials (HTTP 403)")

			return repos
		default:
			logger.Printf("failed to get repos with unexpected response: %d (%s)", resp.StatusCode, resp.Status)

			return repos
		}

		var respObj []giteaRepository

		if err = json.Unmarshal(body, &respObj); err != nil {
			logger.Fatal(err)
		}

		for _, r := range respObj {
			var ru *url.URL
			ru, err = url.Parse(r.CloneUrl)
			if err != nil {
				logger.Printf("failed to parse clone url for %s\n", r.Name)

				continue
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

func (g *GiteaHost) getAPIURL() string {
	return g.APIURL
}

// return normalised method
func (g *GiteaHost) diffRemoteMethod() string {
	switch strings.ToLower(g.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", g.DiffRemoteMethod)

		return "invalid remote comparison method"
	}
}

func giteaWorker(logLevel int, backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(os.Getenv("GITEA_TOKEN"), "\n"), repo.HTTPSUrl[firstPos+2:])
		results <- processBackup(logLevel, repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (g *GiteaHost) Backup() {
	if g.BackupDir == "" {
		logger.Printf("backup skipped as backup directory not specified")

		return
	}

	maxConcurrent := 5
	repoDesc := g.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	for w := 1; w <= maxConcurrent; w++ {
		go giteaWorker(g.LogLevel, g.BackupDir, g.diffRemoteMethod(), g.BackupsToRetain, jobs, results)
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

func (g *GiteaHost) getAllUserRepositories() []repository {
	users := g.getAllUsers()

	var repos []repository
	var userCount int
	for _, user := range users {
		userCount++
		repos = append(repos, g.getAllUserRepos(user.Login)...)
	}

	var repositories []repository
	for _, repo := range repos {
		repositories = append(repositories, repository{
			Name:              repo.Name,
			Owner:             repo.Owner,
			PathWithNameSpace: repo.PathWithNameSpace,
			Domain:            repo.Domain,
			HTTPSUrl:          repo.HTTPSUrl,
			SSHUrl:            repo.SSHUrl,
		})
	}

	return repositories

}
