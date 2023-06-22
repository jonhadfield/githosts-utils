package githosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/peterhellberg/link"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// giteaCallSize      = 100
	// giteaGetUsersLimit = 100
	giteaUsersPerPageDefault         = 20
	giteaUsersLimit                  = -1
	giteaOrganizationsPerPageDefault = 20
	giteaOrganizationsLimit          = -1
	giteaReposPerPageDefault         = 20
	giteaReposLimit                  = -1
)

// const (
// 	workingDIRName               = ".working"
// 	maxIdleConns                 = 10
// 	idleConnTimeout              = 30 * time.Second
// 	defaultHttpRequestTimeout    = 30 * time.Second
// 	defaultHttpClientTimeout     = 10 * time.Second
// 	timeStampFormat              = "20060102150405"
// 	bitbucketAPIURL              = "https://api.bitbucket.org/2.0"
// 	githubAPIURL                 = "https://api.github.com/graphql"
// 	gitlabAPIURL                 = "https://gitlab.com/api/v4"
// 	gitlabProjectsPerPageDefault = 20
// )

type giteaHost struct {
	Provider         string
	APIURL           string
	DiffRemoteMethod string
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

func makeGiteaRequest(c *http.Client, reqUrl string) (resp *http.Response, body []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHttpRequestTimeout)
	defer cancel()

	var req *http.Request

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", os.Getenv("GITEA_TOKEN")))
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

const (
	matchByExact     = "exact"
	matchByIfDefined = "anyDefined"
)

func repoExists(in repoExistsInput) bool {
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
		case matchByExact:
			if allTrue(nameMatch, domainMatch, ownerMatch, cloneUrlMatch, sshUrlMatch, urlWithTokenMatch,
				urlWithBasicAuthMatch, pathWithNamespaceMatch) {
				return true
			}

			continue
		case matchByIfDefined:
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
			}

			return true
		}
	}

	return true
}

func userExists(in userExistsInput) bool {
	for _, u := range in.users {
		loginMatch := in.login == u.Login
		idMatch := in.id == u.ID
		loginNameMatch := in.loginName == u.LoginName
		emailMatch := in.email == u.Email
		fullNameMatch := in.fullName == u.FullName

		switch in.matchBy {
		case matchByExact:
			if allTrue(loginMatch, loginNameMatch, idMatch, emailMatch, fullNameMatch) {
				return true
			}

			continue
		case matchByIfDefined:
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
			}

			return true
		}
	}

	return false
}

func organisationExists(in organisationExistsInput) bool {
	for _, o := range in.organisations {
		nameMatch := in.name == o.Name
		fullNameMatch := in.fullName == o.FullName

		switch in.matchBy {
		case matchByExact:
			if allTrue(nameMatch, fullNameMatch) {
				return true
			}

			continue
		case matchByIfDefined:
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

func (provider giteaHost) describeRepos() describeReposOutput {
	logger.Println("listing repositories")

	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}

	client := &http.Client{Transport: tr}

	userRepos := provider.getAllUserRepositories(client)

	return describeReposOutput{
		Repos: userRepos,
	}
}

func (provider giteaHost) getAllUsers(client *http.Client) (users []giteaUser) {
	logger.Printf("retrieving all users")

	if strings.TrimSpace(provider.APIURL) == "" {
		provider.APIURL = gitlabAPIURL
	}

	getUsersURL := provider.APIURL + "/admin/users"
	if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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
		resp, body, err = makeGiteaRequest(client, reqUrl)
		if err != nil {
			return
		}

		if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
			logger.Printf(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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

		// for _, userResp := range respObj {
		// 	// gitlab replaces hyphens with spaces in owner names, so fix
		// 	// owner := strings.ReplaceAll(project.Owner.Name, " ", "-")
		// 	// repo := repository{
		// 	// 	Name:              project.Path,
		// 	// 	Owner:             owner,
		// 	// 	PathWithNameSpace: project.PathWithNameSpace,
		// 	// 	HTTPSUrl:          project.HTTPSURL,
		// 	// 	SSHUrl:            project.SSHURL,
		// 	// 	Domain:            "gitlab.com",
		// 	// }
		//
		// 	user :=
		//
		// 	users = append(users, repo)
		// }

		// if we got a link response then
		// reset request url
		// link: <https://gitea.lessknown.co.uk/api/v1/admin/users?limit=2&page=2>; rel="next",<https://gitea.lessknown.co.uk/api/v1/admin/users?limit=2&page=2>; rel="last"
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

func (provider giteaHost) getAllOrganizations(client *http.Client) (organizations []giteaOrganization) {
	logger.Printf("retrieving all organizations")

	if strings.TrimSpace(provider.APIURL) == "" {
		provider.APIURL = gitlabAPIURL
	}

	getOrganizationsURL := provider.APIURL + "/orgs"
	if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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
		resp, body, err = makeGiteaRequest(client, reqUrl)
		if err != nil {
			return
		}

		if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
			logger.Println(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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

		// for _, userResp := range respObj {
		// 	// gitlab replaces hyphens with spaces in owner names, so fix
		// 	// owner := strings.ReplaceAll(project.Owner.Name, " ", "-")
		// 	// repo := repository{
		// 	// 	Name:              project.Path,
		// 	// 	Owner:             owner,
		// 	// 	PathWithNameSpace: project.PathWithNameSpace,
		// 	// 	HTTPSUrl:          project.HTTPSURL,
		// 	// 	SSHUrl:            project.SSHURL,
		// 	// 	Domain:            "gitlab.com",
		// 	// }
		//
		// 	user :=
		//
		// 	organisations = append(organisations, repo)
		// }

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

type getRepositoriesResponse []giteaRepository

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

func (provider giteaHost) getAllOrganizationRepos(client *http.Client, organizationName string) (repos []giteaRepository) {
	logger.Printf("retrieving all repositories for organization %s", organizationName)

	if strings.TrimSpace(provider.APIURL) == "" {
		provider.APIURL = gitlabAPIURL
	}

	getOrganizationReposURL := provider.APIURL + fmt.Sprintf("/orgs/%s/repos", organizationName)
	if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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
		resp, body, err = makeGiteaRequest(client, reqUrl)
		if err != nil {
			return
		}

		if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
			logger.Println(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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

		// for _, userResp := range respObj {
		// 	// gitlab replaces hyphens with spaces in owner names, so fix
		// 	// owner := strings.ReplaceAll(project.Owner.Name, " ", "-")
		// 	// repo := repository{
		// 	// 	Name:              project.Path,
		// 	// 	Owner:             owner,
		// 	// 	PathWithNameSpace: project.PathWithNameSpace,
		// 	// 	HTTPSUrl:          project.HTTPSURL,
		// 	// 	SSHUrl:            project.SSHURL,
		// 	// 	Domain:            "gitlab.com",
		// 	// }
		//
		// 	user :=
		//
		// 	repos = append(repos, repo)
		// }

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

func (provider giteaHost) getAllUserRepos(client *http.Client, userName string) (repos []repository) {
	logger.Printf("retrieving all repositories for user %s", userName)

	if strings.TrimSpace(provider.APIURL) == "" {
		provider.APIURL = gitlabAPIURL
	}

	getOrganizationReposURL := provider.APIURL + fmt.Sprintf("/users/%s/repos", userName)
	if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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
		resp, body, err = makeGiteaRequest(client, reqUrl)
		if err != nil {
			return
		}

		if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
			logger.Print(string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
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

//
// func (provider giteaHost) getAllUserRepositories(client http.Client) (repos []repository) {
// 	logger.Printf("retrieving all users")
//
// 	if strings.TrimSpace(provider.APIURL) == "" {
// 		provider.APIURL = gitlabAPIURL
// 	}
//
// 	getUsersURL := provider.APIURL + "/admin/users"
//
// 	var minAccessLevel int
//
// 	var err error
//
// 	minAccessLevelEnvVar := os.Getenv("GITLAB_PROJECT_MIN_ACCESS_LEVEL")
// 	if minAccessLevelEnvVar != "" {
// 		minAccessLevel, err = strconv.Atoi(minAccessLevelEnvVar)
// 		if err != nil {
// 			logger.Printf("GITLAB_PROJECT_MIN_ACCESS_LEVEL '%s' is not a number so using default",
// 				minAccessLevelEnvVar)
//
// 			minAccessLevel = DefaultMinimumProjectAccessLevel
// 		}
// 	}
//
// 	if !slices.Contains(sortedLevels, minAccessLevel) {
// 		if minAccessLevelEnvVar != "" {
// 			logger.Printf("project minimum access level must be one of %s so using default",
// 				strings.Join(validMinimumProjectAccessLevels, ", "))
// 		}
//
// 		minAccessLevel = DefaultMinimumProjectAccessLevel
// 	}
//
// 	logger.Printf("project minimum access level set to %s (%d)",
// 		validAccessLevels[minAccessLevel],
// 		minAccessLevel)
//
// 	// Initial request
// 	u, err := url.Parse(getUsersURL)
// 	if err != nil {
// 		return
// 	}
//
// 	q := u.Query()
// 	// set initial max per page
// 	q.Set("per_page", strconv.Itoa(gitlabProjectsPerPageDefault))
// 	q.Set("min_access_level", strconv.Itoa(minAccessLevel))
// 	u.RawQuery = q.Encode()
// 	var body []byte
//
// 	reqUrl := u.String()
// 	for {
// 		var resp *http.Response
// 		resp, body, err = makeGitLabRequest(&client, reqUrl)
// 		if err != nil {
// 			return
// 		}
//
// 		if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
// 			logger.Println(string(body))
// 		}
//
// 		switch resp.StatusCode {
// 		case http.StatusOK:
// 			if strings.ToLower(os.Getenv("GITHOSTS_LOG")) == "trace" {
// 				logger.Println("projects retrieved successfully")
// 			}
// 		case http.StatusForbidden:
// 			logger.Println("failed to get projects due to invalid missing permissions (HTTP 403)")
//
// 			return repos
// 		default:
// 			logger.Printf("failed to get projects due to unexpected response: %d (%s)", resp.StatusCode, resp.Status)
//
// 			return repos
// 		}
//
// 		var respObj gitLabGetProjectsResponse
//
// 		if err = json.Unmarshal(body, &respObj); err != nil {
// 			logger.Fatal(err)
// 		}
//
// 		for _, project := range respObj {
// 			// gitlab replaces hyphens with spaces in owner names, so fix
// 			owner := strings.ReplaceAll(project.Owner.Name, " ", "-")
// 			repo := repository{
// 				Name:              project.Path,
// 				Owner:             owner,
// 				PathWithNameSpace: project.PathWithNameSpace,
// 				HTTPSUrl:          project.HTTPSURL,
// 				SSHUrl:            project.SSHURL,
// 				Domain:            "gitlab.com",
// 			}
//
// 			repos = append(repos, repo)
// 		}
//
// 		// if we got a link response then
// 		// reset request url
// 		reqUrl = ""
// 		for _, l := range link.ParseResponse(resp) {
// 			if l.Rel == "next" {
// 				reqUrl = l.URI
// 			}
// 		}
//
// 		if reqUrl == "" {
// 			break
// 		}
// 	}
//
// 	return repos
// }

// func describeGiteaOrgRepos(c *http.Client, orgName string) []repository {
// 	logger.Printf("listing Gitea organisation %s's repositories", orgName)
//
// 	gcs := giteaCallSize
// 	envCallSize := os.Getenv("GITEA_CALL_SIZE")
// 	if envCallSize != "" {
// 		if callSize, err := strconv.Atoi(envCallSize); err != nil {
// 			gcs = callSize
// 		}
// 	}
//
// 	var repos []repository
//
// 	reqBody := "query { organization(login: \"" + orgName + "\") { repositories(first:" + strconv.Itoa(gcs) + ") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }}}}"
//
// 	for {
// 		bodyStr := makeGiteaRequest(c, createGiteaRequestPayload(reqBody))
//
// 		var respObj giteaQueryOrgResponse
// 		if err := json.Unmarshal([]byte(bodyStr), &respObj); err != nil {
// 			logger.Fatal(err)
// 		}
//
// 		for _, repo := range respObj.Data.Organization.Repositories.Edges {
// 			repos = append(repos, repository{
// 				Name:              repo.Node.Name,
// 				SSHUrl:            repo.Node.SSHURL,
// 				HTTPSUrl:          repo.Node.URL,
// 				PathWithNameSpace: repo.Node.NameWithOwner,
// 				Domain:            "gitea.com",
// 			})
// 		}
//
// 		if !respObj.Data.Organization.Repositories.PageInfo.HasNextPage {
// 			break
// 		} else {
// 			reqBody = "query { organization(login: \"" + orgName + "\") { repositories(first:" + strconv.Itoa(gcs) + " after: \"" + respObj.Data.Organization.Repositories.PageInfo.EndCursor + "\") { edges { node { name nameWithOwner url sshUrl } cursor } pageInfo { endCursor hasNextPage }}}}"
// 		}
// 	}
//
// 	return repos
// }

// func (provider giteaHost) describeRepos() describeReposOutput {
// 	tr := &http.Transport{
// 		MaxIdleConns:       maxIdleConns,
// 		IdleConnTimeout:    idleConnTimeout,
// 		DisableCompression: true,
// 	}
// 	client := &http.Client{Transport: tr}
//
// 	repos := describeGiteaUserRepos(client)
//
// 	if len(strings.TrimSpace(os.Getenv("GITEA_ORGS"))) > 0 {
// 		orgs := strings.Split(os.Getenv("GITEA_ORGS"), ",")
// 		for _, org := range orgs {
// 			repos = append(repos, describeGiteaOrgRepos(client, org)...)
// 		}
// 	}
//
// 	return describeReposOutput{
// 		Repos: repos,
// 	}
// }

func (provider giteaHost) getAPIURL() string {
	return provider.APIURL
}

// func giteaWorker(backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
// 	for repo := range jobs {
// 		firstPos := strings.Index(repo.HTTPSUrl, "//")
// 		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(os.Getenv("GITEA_TOKEN"), "\n"), repo.HTTPSUrl[firstPos+2:])
// 		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
// 	}
// }

// func (provider giteaHost) Backup(backupDIR string) {
// 	maxConcurrent := 5
// 	repoDesc := provider.describeRepos()
//
// 	jobs := make(chan repository, len(repoDesc.Repos))
// 	results := make(chan error, maxConcurrent)
//
// 	backupsToKeep, err := strconv.Atoi(os.Getenv("GITEA_BACKUPS"))
// 	if err != nil {
// 		backupsToKeep = 0
// 	}
//
// 	for w := 1; w <= maxConcurrent; w++ {
// 		go giteaWorker(backupDIR, provider.diffRemoteMethod(), backupsToKeep, jobs, results)
// 	}
//
// 	for x := range repoDesc.Repos {
// 		repo := repoDesc.Repos[x]
// 		jobs <- repo
// 	}
//
// 	close(jobs)
//
// 	for a := 1; a <= len(repoDesc.Repos); a++ {
// 		res := <-results
// 		if res != nil {
// 			logger.Printf("backup failed: %+v\n", res)
// 		}
// 	}
// }

// return normalised method
func (provider giteaHost) diffRemoteMethod() string {
	switch strings.ToLower(provider.DiffRemoteMethod) {
	case refsMethod:
		return refsMethod
	case cloneMethod:
		return cloneMethod
	default:
		logger.Printf("unexpected diff remote method: %s", provider.DiffRemoteMethod)

		// default to bundle as safest
		return cloneMethod
	}
}

func giteaWorker(backupDIR, diffRemoteMethod string, backupsToKeep int, jobs <-chan repository, results chan<- error) {
	for repo := range jobs {
		firstPos := strings.Index(repo.HTTPSUrl, "//")
		repo.URLWithToken = fmt.Sprintf("%s%s@%s", repo.HTTPSUrl[:firstPos+2], stripTrailing(os.Getenv("GITEA_TOKEN"), "\n"), repo.HTTPSUrl[firstPos+2:])
		results <- processBackup(repo, backupDIR, backupsToKeep, diffRemoteMethod)
	}
}

func (provider giteaHost) Backup(backupDIR string) {
	maxConcurrent := 5
	repoDesc := provider.describeRepos()

	jobs := make(chan repository, len(repoDesc.Repos))
	results := make(chan error, maxConcurrent)

	backupsToKeep, err := strconv.Atoi(os.Getenv("GITEA_BACKUPS"))
	if err != nil {
		backupsToKeep = 0
	}

	for w := 1; w <= maxConcurrent; w++ {
		go giteaWorker(backupDIR, provider.diffRemoteMethod(), backupsToKeep, jobs, results)
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

func (g giteaHost) getAllUserRepositories(client *http.Client) []repository {
	gHost := giteaHost{
		Provider:         "Gitea",
		APIURL:           os.Getenv("GITEA_APIURL"),
		DiffRemoteMethod: refsMethod,
	}

	users := gHost.getAllUsers(client)

	var repos []repository
	var userCount int
	for _, user := range users {
		userCount++
		repos = append(repos, gHost.getAllUserRepos(client, user.Login)...)
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
