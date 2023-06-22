package githosts

import (
	"github.com/stretchr/testify/require"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// var buf bytes.Buffer

func init() {
	logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
}

func TestGiteaGetUsers(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv("GITEA_APIURL")

	if giteaToken == "" {
		t.Skip("Skipping Gitea test as GITEA_TOKEN is missing")
	}

	if giteaAPIURL == "" {
		t.Skip("Skipping Gitea test as GITEA_APIURL are missing")
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITEA_APIURL"})

	gHost := giteaHost{
		Provider:         "Gitea",
		APIURL:           os.Getenv("GITEA_APIURL"),
		DiffRemoteMethod: refsMethod,
	}
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	users := gHost.getAllUsers(client)
	require.True(t, userExists(userExistsInput{
		matchBy:  matchByIfDefined,
		users:    users,
		fullName: "soba test rod",
		login:    "soba-test-rod",
		email:    "soba-test-rod@example.com",
	}))
	// require.Len(t, users, 4)

	// backupDIR := os.Getenv("GIT_BACKUP_DIR")
	//
	// // extract domain from API URL to use for unique backup directory
	// gUrl, err := url.Parse(giteaAPIURL)
	// require.NoError(t, err)
	// giteaDomain := gUrl.Host
	// backupDIR = filepath.Join(backupDIR, giteaDomain)
	//
	// ghHost := githubHost{
	// 	Provider:         "Gitea",
	// 	APIURL:           giteaAPIURL,
	// 	DiffRemoteMethod: cloneMethod,
	// }
	//
	// ghHost.Backup(backupDIR)
	//
	// expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
	// require.DirExists(t, expectedPathOne)
	// dirOneEntries, err := dirContents(expectedPathOne)
	// require.NoError(t, err)
	// require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())
	//
	// expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
	// require.DirExists(t, expectedPathTwo)
	// dirTwoEntries, err := dirContents(expectedPathTwo)
	// require.NoError(t, err)
	// require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())
	//
	// expectedPathThree := filepath.Join(backupDIR, "github.com", "go-soba", "repo2")
	// require.NoDirExists(t, expectedPathThree)

	restoreEnvironmentVariables(envBackup)
}

//
// func TestGiteaGetOrganisations(t *testing.T) {
// 	giteaToken := os.Getenv("GITEA_TOKEN")
// 	giteaAPIURL := os.Getenv("GITEA_APIURL")
//
// 	if giteaToken == "" {
// 		t.Skip("Skipping Gitea test as GITEA_TOKEN is missing")
// 	}
//
// 	if giteaAPIURL == "" {
// 		t.Skip("Skipping Gitea test as GITEA_APIURL are missing")
// 	}
//
// 	resetBackups()
//
// 	resetGlobals()
// 	envBackup := backupEnvironmentVariables()
//
// 	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITEA_APIURL"})
//
// 	gHost := giteaHost{
// 		Provider:         "Gitea",
// 		APIURL:           os.Getenv("GITEA_APIURL"),
// 		DiffRemoteMethod: refsMethod,
// 	}
// 	tr := &http.Transport{
// 		MaxIdleConns:       maxIdleConns,
// 		IdleConnTimeout:    idleConnTimeout,
// 		DisableCompression: true,
// 	}
// 	client := &http.Client{Transport: tr}
//
// 	organizations := gHost.getAllOrganizations(client)
// 	// require.Len(t, organizations, 1)
//
// 	// backupDIR := os.Getenv("GIT_BACKUP_DIR")
// 	//
// 	// // extract domain from API URL to use for unique backup directory
// 	// gUrl, err := url.Parse(giteaAPIURL)
// 	// require.NoError(t, err)
// 	// giteaDomain := gUrl.Host
// 	// backupDIR = filepath.Join(backupDIR, giteaDomain)
// 	//
// 	// ghHost := githubHost{
// 	// 	Provider:         "Gitea",
// 	// 	APIURL:           giteaAPIURL,
// 	// 	DiffRemoteMethod: cloneMethod,
// 	// }
// 	//
// 	// ghHost.Backup(backupDIR)
// 	//
// 	// expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
// 	// require.DirExists(t, expectedPathOne)
// 	// dirOneEntries, err := dirContents(expectedPathOne)
// 	// require.NoError(t, err)
// 	// require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())
// 	//
// 	// expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
// 	// require.DirExists(t, expectedPathTwo)
// 	// dirTwoEntries, err := dirContents(expectedPathTwo)
// 	// require.NoError(t, err)
// 	// require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())
// 	//
// 	// expectedPathThree := filepath.Join(backupDIR, "github.com", "go-soba", "repo2")
// 	// require.NoDirExists(t, expectedPathThree)
//
// 	restoreEnvironmentVariables(envBackup)
// }

// func TestGetAllOrganizationRepos(t *testing.T) {
// 	giteaToken := os.Getenv("GITEA_TOKEN")
// 	giteaAPIURL := os.Getenv("GITEA_APIURL")
//
// 	if giteaToken == "" {
// 		t.Skip("Skipping Gitea test as GITEA_TOKEN is missing")
// 	}
//
// 	if giteaAPIURL == "" {
// 		t.Skip("Skipping Gitea test as GITEA_APIURL are missing")
// 	}
//
// 	resetBackups()
//
// 	resetGlobals()
// 	envBackup := backupEnvironmentVariables()
//
// 	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITEA_APIURL"})
//
// 	gHost := giteaHost{
// 		Provider:         "Gitea",
// 		APIURL:           os.Getenv("GITEA_APIURL"),
// 		DiffRemoteMethod: refsMethod,
// 	}
// 	tr := &http.Transport{
// 		MaxIdleConns:       maxIdleConns,
// 		IdleConnTimeout:    idleConnTimeout,
// 		DisableCompression: true,
// 	}
// 	client := &http.Client{Transport: tr}
//
// 	organizations := gHost.getAllOrganizations(client)
// 	// require.Len(t, organizations, 1)
// 	require.True(t, organisationExists(organisationExistsInput{
// 		matchBy:       matchByIfDefined,
// 		organisations: organizations,
// 		name:          "soba-test-org",
// 		fullName:      "soba test org",
// 	}))
//
// 	// var repos []giteaRepository
// 	// var orgCount int
// 	// for _, org := range organizations {
// 	// 	orgCount++
// 	// 	repos = append(repos, gHost.getAllOrganizationRepos(client, org.Name)...)
// 	// }
// 	//
// 	// require.Equal(t, orgCount, 1)
// 	// require.Len(t, repos, 1)
//
// 	restoreEnvironmentVariables(envBackup)
// }

func TestGetAllUserRepos(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv("GITEA_APIURL")

	if giteaToken == "" {
		t.Skip("Skipping Gitea test as GITEA_TOKEN is missing")
	}

	if giteaAPIURL == "" {
		t.Skip("Skipping Gitea test as GITEA_APIURL are missing")
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITEA_APIURL"})

	gHost := giteaHost{
		Provider:         "Gitea",
		APIURL:           os.Getenv("GITEA_APIURL"),
		DiffRemoteMethod: refsMethod,
	}
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	users := gHost.getAllUsers(client)
	// require.Len(t, users, 4)

	var repos []repository
	var userCount int
	for _, user := range users {
		userCount++
		repos = append(repos, gHost.getAllUserRepos(client, user.Login)...)
	}

	require.True(t, repoExists(repoExistsInput{
		matchBy:          matchByIfDefined,
		repos:            repos,
		name:             "soba-test-rod-repo-one",
		owner:            "soba-test-rod",
		httpsUrl:         "https://gitea.lessknown.co.uk/soba-test-rod/soba-test-rod-repo-one.git",
		sshUrl:           "",
		urlWithToken:     "",
		urlWithBasicAuth: "",
	}))

	restoreEnvironmentVariables(envBackup)

}

func TestGiteaRepositoryBackup(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv("GITEA_APIURL")

	if giteaToken == "" {
		t.Skip("Skipping Gitea test as GITEA_TOKEN is missing")
	}

	if giteaAPIURL == "" {
		t.Skip("Skipping Gitea test as GITEA_APIURL are missing")
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN"})

	backupDIR := os.Getenv("GIT_BACKUP_DIR")

	// extract domain from API URL to use for unique backup directory

	ghHost := giteaHost{
		Provider:         "Gitea",
		APIURL:           giteaAPIURL,
		DiffRemoteMethod: cloneMethod,
	}

	ghHost.Backup(backupDIR)

	expectedPathOne := filepath.Join(backupDIR, "gitea.lessknown.co.uk", "gitea_admin", "soba-repo-one")
	require.DirExists(t, expectedPathOne)
	dirOneEntries, err := dirContents(expectedPathOne)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^soba-repo-one\.\d{14}\.bundle$`), dirOneEntries[0].Name())

	expectedPathTwo := filepath.Join(backupDIR, "gitea.lessknown.co.uk", "soba-test-rod", "soba-test-rod-repo-one")
	require.DirExists(t, expectedPathTwo)
	dirTwoEntries, err := dirContents(expectedPathTwo)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^soba-test-rod-repo-one\.\d{14}\.bundle$`), dirTwoEntries[0].Name())

	expectedPathThree := filepath.Join(backupDIR, "gitea.lessknown.co.uk", "gitea_admin", "soba-repo-two")
	require.NoDirExists(t, expectedPathThree)

	restoreEnvironmentVariables(envBackup)
}

//
// func TestDescribeGithubOrgRepos(t *testing.T) {
// 	if os.Getenv("GITEA_TOKEN") == "" {
// 		t.Skip("Skipping GitHub test as GITEA_TOKEN is missing")
// 	}
//
// 	// need to set output to buffer in order to test output
// 	logger.SetOutput(&buf)
// 	defer logger.SetOutput(os.Stdout)
//
// 	resetBackups()
//
// 	resetGlobals()
// 	envBackup := backupEnvironmentVariables()
//
// 	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITHUB_ORGS"})
//
// 	repos := describeGithubOrgRepos(http.DefaultClient, "Nudelmesse")
// 	require.Len(t, repos, 2)
//
// 	restoreEnvironmentVariables(envBackup)
// }
//
// func TestPublicGitHubOrgRepoBackups(t *testing.T) {
// 	if os.Getenv("GITEA_TOKEN") == "" {
// 		t.Skip("Skipping GitHub test as GITEA_TOKEN is missing")
// 	}
//
// 	// need to set output to buffer in order to test output
// 	logger.SetOutput(&buf)
// 	defer logger.SetOutput(os.Stdout)
//
// 	resetBackups()
//
// 	resetGlobals()
// 	envBackup := backupEnvironmentVariables()
//
// 	unsetEnvVars([]string{"GIT_BACKUP_DIR", "GITEA_TOKEN", "GITHUB_ORGS"})
//
// 	backupDIR := os.Getenv("GIT_BACKUP_DIR")
//
// 	ghHost := githubHost{
// 		Provider:         "GitHub",
// 		APIURL:           githubAPIURL,
// 		DiffRemoteMethod: refsMethod,
// 	}
//
// 	ghHost.Backup(backupDIR)
//
// 	expectedPathOne := filepath.Join(backupDIR, "github.com", "go-soba", "repo0")
// 	require.DirExists(t, expectedPathOne)
// 	dirOneEntries, err := dirContents(expectedPathOne)
// 	require.NoError(t, err)
// 	require.Regexp(t, regexp.MustCompile(`^repo0\.\d{14}\.bundle$`), dirOneEntries[0].Name())
//
// 	expectedPathTwo := filepath.Join(backupDIR, "github.com", "go-soba", "repo1")
// 	require.DirExists(t, expectedPathTwo)
// 	dirTwoEntries, err := dirContents(expectedPathTwo)
// 	require.NoError(t, err)
// 	require.Regexp(t, regexp.MustCompile(`^repo1\.\d{14}\.bundle$`), dirTwoEntries[0].Name())
//
// 	// backup once more so we have bundles to compare and skip
// 	ghHost.Backup(backupDIR)
// 	logLines := strings.Split(strings.ReplaceAll(buf.String(), "\r\n", "\n"), "\n")
//
// 	var reRepo0 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo0'`)
// 	var reRepo1 = regexp.MustCompile(`skipping clone of github\.com repo 'go-soba/repo1'`)
// 	var matches int
//
// 	logger.SetOutput(os.Stdout)
//
// 	for x := range logLines {
// 		logger.Print(logLines[x])
// 		if reRepo0.MatchString(logLines[x]) {
// 			matches++
// 		}
// 		if reRepo1.MatchString(logLines[x]) {
// 			matches++
// 		}
// 	}
//
// 	require.Equal(t, 2, matches)
//
// 	restoreEnvironmentVariables(envBackup)
// }
