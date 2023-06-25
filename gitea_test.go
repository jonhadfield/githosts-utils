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

func init() {
	logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
}

func TestGiteaGetUsers(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken, giteaEnvVarAPIUrl})

	gHost := giteaHost{
		Provider:         giteaProviderName,
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
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
		matchBy:  giteaMatchByIfDefined,
		users:    users,
		fullName: "soba test rod",
		login:    "soba-test-rod",
		email:    "soba-test-rod@example.com",
	}))

	restoreEnvironmentVariables(envBackup)
}

func TestGiteaGetOrganisations(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken, giteaEnvVarAPIUrl, giteaEnvVarOrgs})
	gHost := giteaHost{
		Provider:         giteaProviderName,
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
	}
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	// without org names we should get no orgs
	organizations := gHost.getOrganizations(client)
	require.Len(t, organizations, 0)

	// with single org name we should only get that org
	_ = os.Setenv(giteaEnvVarOrgs, "soba-org-two")
	organizations = gHost.getOrganizations(client)

	require.False(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-one",
		fullName:      "soba org one",
	}))

	require.True(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-two",
		fullName:      "soba org two",
	}))

	restoreEnvironmentVariables(envBackup)
}

// getOrganizationsRepos
func TestGetOrganizationsRepos(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken, giteaEnvVarAPIUrl, giteaEnvVarOrgs})

	gHost := giteaHost{
		Provider:         giteaProviderName,
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
	}

	client := &http.Client{Transport: &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}}

	// without env vars, we shouldn't get any orgs
	repos := gHost.getOrganizationsRepos(client, []giteaOrganization{
		{Name: "soba-org-one", FullName: "soba org one"},
	})

	require.True(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             repos,
		name:              "soba-org-one-repo-one",
		pathWithNamespace: "soba-org-one/soba-org-one-repo-one",
		httpsUrl:          "https://gitea.lessknown.co.uk/soba-org-one/soba-org-one-repo-one.git",
		// sshUrl:            "TODO",
	}))

	require.False(t, repoExists(repoExistsInput{
		matchBy:           giteaMatchByIfDefined,
		repos:             repos,
		name:              "soba-org-two-repo-two",
		pathWithNamespace: "soba-org-teo/soba-org-two-repo-two",
		httpsUrl:          "https://gitea.lessknown.co.uk/soba-org-two/soba-org-two-repo-two.git",
		// sshUrl:            "TODO",
	}))

	restoreEnvironmentVariables(envBackup)
}

func TestGetAllOrganizationRepos(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken, giteaEnvVarAPIUrl, giteaEnvVarOrgs})

	gHost := giteaHost{
		Provider:         giteaProviderName,
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
	}
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	organizations := gHost.getOrganizations(client)
	require.GreaterOrEqual(t, len(organizations), 0)

	_ = os.Setenv(giteaEnvVarOrgs, "soba-org-two")
	organizations = gHost.getOrganizations(client)

	require.GreaterOrEqual(t, len(organizations), 1)
	require.False(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-one",
		fullName:      "soba org one",
	}))

	require.True(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-two",
		fullName:      "soba org two",
	}))

	// * should return all orgs
	_ = os.Setenv(giteaEnvVarOrgs, "*")
	organizations = gHost.getOrganizations(client)

	require.GreaterOrEqual(t, len(organizations), 2)
	require.True(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-one",
		fullName:      "soba org one",
	}))

	require.True(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-two",
		fullName:      "soba org two",
	}))

	restoreEnvironmentVariables(envBackup)
}

func TestGetAllUserRepos(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken, giteaEnvVarAPIUrl})

	gHost := giteaHost{
		Provider:         giteaProviderName,
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
	}
	tr := &http.Transport{
		MaxIdleConns:       maxIdleConns,
		IdleConnTimeout:    idleConnTimeout,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	users := gHost.getAllUsers(client)

	var repos []repository
	var userCount int
	for _, user := range users {
		userCount++
		repos = append(repos, gHost.getAllUserRepos(client, user.Login)...)
	}

	require.True(t, repoExists(repoExistsInput{
		matchBy:          giteaMatchByIfDefined,
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

func TestGetAPIURL(t *testing.T) {
	apiURL := "https://api.example.com/api/v1"

	require.Equal(t, apiURL, giteaHost{
		Provider:         giteaProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: cloneMethod,
	}.getAPIURL())
}

func TestGiteaDiffRemoteMethod(t *testing.T) {
	apiURL := "https://api.example.com/api/v1"

	require.Equal(t, refsMethod, giteaHost{
		Provider:         giteaProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: refsMethod,
	}.diffRemoteMethod())

	require.Equal(t, cloneMethod, giteaHost{
		Provider:         giteaProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: cloneMethod,
	}.diffRemoteMethod())

	require.Contains(t, giteaHost{
		Provider:         giteaProviderName,
		APIURL:           apiURL,
		DiffRemoteMethod: "invalid",
	}.diffRemoteMethod(), "invalid")

}

func TestGiteaRepositoryBackup(t *testing.T) {
	giteaToken := os.Getenv(giteaEnvVarToken)
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", giteaEnvVarToken)
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, giteaEnvVarToken})

	backupDIR := os.Getenv(envVarGitBackupDir)

	// extract domain from API URL to use for unique backup directory

	ghHost := giteaHost{
		Provider:         giteaProviderName,
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

func TestGiteaRepositoryExistsWithoutRepos(t *testing.T) {
	// without repos presented - should return false
	require.False(t, repoExists(repoExistsInput{
		repos:        []repository{},
		name:         "repo1",
		owner:        "go-soba",
		domain:       "gitea.example.com",
		httpsUrl:     "",
		sshUrl:       "",
		urlWithToken: "",
	}))

}

func TestGiteaRepositoryExistsWithMatch(t *testing.T) {
	// positive 'if defined' match
	require.True(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByIfDefined,
		repos: []repository{
			{
				Name:             "repo0",
				Owner:            "go-soba",
				Domain:           "gitea.example.com",
				HTTPSUrl:         "",
				SSHUrl:           "",
				URLWithToken:     "",
				URLWithBasicAuth: "",
			},
		},
		name:         "repo0",
		owner:        "go-soba",
		domain:       "gitea.example.com",
		httpsUrl:     "",
		sshUrl:       "",
		urlWithToken: "",
	}))
}

func TestGiteaRepositoryExistsWithoutMatch(t *testing.T) {
	// positive 'if defined' match with "if defined check"
	require.False(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByIfDefined,
		repos: []repository{
			{
				Name:             "repo0",
				Owner:            "go-soba",
				Domain:           "gitea.example.com",
				HTTPSUrl:         "",
				SSHUrl:           "",
				URLWithToken:     "",
				URLWithBasicAuth: "",
			},
		},
		name:         "repo1",
		owner:        "go-soba",
		domain:       "gitea.example.com",
		httpsUrl:     "",
		sshUrl:       "",
		urlWithToken: "",
	}))

	// negative 'if defined' match with "if defined check"
	require.False(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByIfDefined,
		repos: []repository{
			{
				Name:             "repo0",
				Owner:            "go-soba",
				Domain:           "gitea.example.com",
				HTTPSUrl:         "https://gitea.example.com/go-soba/repo0.git",
				SSHUrl:           "",
				URLWithToken:     "",
				URLWithBasicAuth: "",
			},
		},
		name:         "repo0",
		owner:        "go-soba",
		domain:       "https://gitea.example.com/go-soba/repo1.git",
		httpsUrl:     "",
		sshUrl:       "",
		urlWithToken: "",
	}))

	// negative 'if defined' match with "match all check"
	require.False(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByExact,
		repos: []repository{
			{
				Name:             "repo0",
				Owner:            "go-soba",
				Domain:           "gitea.example.com",
				HTTPSUrl:         "https://gitea.example.com/go-soba/repo0.git",
				SSHUrl:           "git@ssh://gitea.example.com/go-soba/repo0.git",
				URLWithToken:     "",
				URLWithBasicAuth: "",
			},
		},
		name:         "repo0",
		owner:        "go-soba",
		domain:       "gitea.example.com",
		httpsUrl:     "https://gitea.example.com/go-soba/repo0.git",
		sshUrl:       "",
		urlWithToken: "",
	}))
}
