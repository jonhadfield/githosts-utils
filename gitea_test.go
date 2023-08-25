package githosts

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func init() {
	if logger == nil {
		logger = log.New(os.Stdout, logEntryPrefix, log.Lshortfile|log.LstdFlags)
	}

	defer func() {
		log.SetOutput(os.Stderr)
	}()
}

func TestGiteaGetUsers(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN", giteaEnvVarAPIUrl})

	gHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
		Token:            giteaToken,
	})
	require.NoError(t, err)

	gHost.Token = giteaToken

	users := gHost.getAllUsers()
	require.True(t, userExists(userExistsInput{
		matchBy:  giteaMatchByIfDefined,
		users:    users,
		fullName: "soba test rod",
		login:    "soba-test-rod",
		email:    "soba-test-rod@example.com",
	}))
}

func TestGiteaGetOrganisations(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN", giteaEnvVarAPIUrl})

	gHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
		Token:            giteaToken,
	})
	require.NoError(t, err)

	// without org names we should get no orgs
	organizations := gHost.getOrganizations()
	require.Len(t, organizations, 0)

	// with single org name we should only get that org
	gHost.Orgs = []string{"soba-org-two"}
	organizations = gHost.getOrganizations()

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
}

// getOrganizationsRepos
func TestGetOrganizationsRepos(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN", giteaEnvVarAPIUrl})

	gHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
		Token:            giteaToken,
	})
	require.NoError(t, err)

	// without env vars, we shouldn't get any orgs
	repos := gHost.getOrganizationsRepos([]giteaOrganization{
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
		pathWithNamespace: "soba-org-two/soba-org-two-repo-two",
		httpsUrl:          "https://gitea.lessknown.co.uk/soba-org-two/soba-org-two-repo-two.git",
		// sshUrl:            "TODO",
	}))

	restoreEnvironmentVariables(envBackup)
}

func TestGetAllOrganizationRepos(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN", giteaEnvVarAPIUrl})

	gHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITEA_TOKEN"),
	})

	require.NoError(t, err)

	organizations := gHost.getOrganizations()
	require.GreaterOrEqual(t, len(organizations), 0)
	require.False(t, organisationExists(organisationExistsInput{
		matchBy:       giteaMatchByIfDefined,
		organisations: organizations,
		name:          "soba-org-one",
		fullName:      "soba org one",
	}))
	// gHost.Orgs = []string{"soba-org-two"}

	gHost.Orgs = []string{"soba-org-two"}
	organizations = gHost.getOrganizations()

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
	gHost.Orgs = []string{"*"}
	organizations = gHost.getOrganizations()

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
}

func TestGetAllUserRepos(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN", giteaEnvVarAPIUrl})

	gHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           os.Getenv(giteaEnvVarAPIUrl),
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITEA_TOKEN"),
	})
	require.NoError(t, err)

	users := gHost.getAllUsers()

	var repos []repository
	var userCount int
	for _, user := range users {
		userCount++
		repos = append(repos, gHost.getAllUserRepos(user.Login)...)
	}

	require.True(t, repoExists(repoExistsInput{
		matchBy:  giteaMatchByIfDefined,
		repos:    repos,
		name:     "soba-test-rod-repo-one",
		owner:    "soba-test-rod",
		httpsUrl: "https://gitea.lessknown.co.uk/soba-test-rod/soba-test-rod-repo-one.git",
	}))
}

func TestGetAPIURL(t *testing.T) {
	apiURL := "https://api.example.com/api/v1"

	gh, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           apiURL,
		DiffRemoteMethod: cloneMethod,
		Token:            os.Getenv("GITEA_TOKEN"),
	})
	require.NoError(t, err)
	require.Equal(t, apiURL, gh.getAPIURL())
}

func TestGiteaDiffRemoteMethod(t *testing.T) {
	apiURL := "https://api.example.com/api/v1"

	gh, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           apiURL,
		DiffRemoteMethod: refsMethod,
		Token:            os.Getenv("GITEA_TOKEN"),
	})
	require.NoError(t, err)
	require.Equal(t, refsMethod, gh.diffRemoteMethod())

	gh, err = NewGiteaHost(NewGiteaHostInput{
		APIURL:           apiURL,
		DiffRemoteMethod: cloneMethod,
		Token:            os.Getenv("GITEA_TOKEN"),
	})
	require.NoError(t, err)
	require.Equal(t, cloneMethod, gh.diffRemoteMethod())

	_, err = NewGiteaHost(NewGiteaHostInput{
		APIURL:           apiURL,
		DiffRemoteMethod: "invalid",
		Token:            os.Getenv("GITEA_TOKEN"),
	})
	require.Error(t, err)
}

func TestGiteaRepositoryBackup(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN"})

	backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           giteaAPIURL,
		DiffRemoteMethod: cloneMethod,
		BackupDir:        backupDIR,
		Token:            giteaToken,
	})
	require.NoError(t, err)

	ghHost.Backup()

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
}

func TestGiteaRepositoryBackupWithoutBackupDir(t *testing.T) {
	giteaToken := os.Getenv("GITEA_TOKEN")
	giteaAPIURL := os.Getenv(giteaEnvVarAPIUrl)

	if giteaToken == "" {
		t.Skipf("Skipping Gitea test as %s is missing", "GITEA_TOKEN")
	}

	if giteaAPIURL == "" {
		t.Skipf("Skipping Gitea test as %s are missing", giteaEnvVarAPIUrl)
	}

	resetBackups()

	resetGlobals()
	envBackup := backupEnvironmentVariables()
	defer restoreEnvironmentVariables(envBackup)

	unsetEnvVars([]string{envVarGitBackupDir, "GITEA_TOKEN"})

	// backupDIR := os.Getenv(envVarGitBackupDir)

	ghHost, err := NewGiteaHost(NewGiteaHostInput{
		APIURL:           giteaAPIURL,
		DiffRemoteMethod: cloneMethod,
		Token:            giteaToken,
	})
	// no error expected as backup dir not required for all usage
	require.NoError(t, err)

	ghHost.Backup()
	// check we don't backup to current path if no backup dir is set
	path := filepath.Join("gitea.lessknown.co.uk", "gitea_admin", "soba-repo-one")
	require.NoDirExists(t, path)
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

func TestRepoExists1(t *testing.T) {
	require.True(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByExact,
		repos: []repository{
			{
				Name:              "repo1",
				Owner:             "owner1",
				PathWithNameSpace: "pwns1",
				Domain:            "domain1",
				HTTPSUrl:          "httpsUrl1",
				SSHUrl:            "sshUrl1",
				URLWithToken:      "urlWithToken1",
				URLWithBasicAuth:  "urlWithBasicAuth1",
			},
		},
		name:              "repo1",
		owner:             "owner1",
		pathWithNamespace: "pwns1",
		domain:            "domain1",
		httpsUrl:          "httpsUrl1",
		sshUrl:            "sshUrl1",
		urlWithToken:      "urlWithToken1",
		urlWithBasicAuth:  "urlWithBasicAuth1",
	}))
}

func TestRepoExists2(t *testing.T) {
	require.False(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByExact,
		repos: []repository{
			{
				Name:              "repo2",
				Owner:             "owner1",
				PathWithNameSpace: "pwns1",
				Domain:            "domain1",
				HTTPSUrl:          "httpsUrl1",
				SSHUrl:            "sshUrl1",
				URLWithToken:      "urlWithToken1",
				URLWithBasicAuth:  "urlWithBasicAuth1",
			},
		},
		name:              "repo1",
		owner:             "owner1",
		pathWithNamespace: "pwns1",
		domain:            "domain1",
		httpsUrl:          "httpsUrl1",
		sshUrl:            "sshUrl1",
		urlWithToken:      "urlWithToken1",
		urlWithBasicAuth:  "urlWithBasicAuth1",
	}))
}

func TestRepoExists3(t *testing.T) {
	require.True(t, repoExists(repoExistsInput{
		matchBy: giteaMatchByIfDefined,
		repos: []repository{
			{
				Name:              "repo1",
				Owner:             "owner1",
				PathWithNameSpace: "pwns1",
				HTTPSUrl:          "httpsUrl1",
				Domain:            "domain1",
				SSHUrl:            "sshUrl1",
				URLWithToken:      "urlWithToken1",
				URLWithBasicAuth:  "urlWithBasicAuth1",
			},
		},
		name:              "repo1",
		owner:             "owner1",
		pathWithNamespace: "pwns1",
		domain:            "domain1",
		sshUrl:            "sshUrl1",
		urlWithToken:      "urlWithToken1",
		urlWithBasicAuth:  "urlWithBasicAuth1",
	}))
}

func TestUserExistsExactMatch(t *testing.T) {
	require.True(t, userExists(userExistsInput{
		matchBy: giteaMatchByExact,
		users: []giteaUser{
			{
				ID:        10,
				Login:     "userlogin1",
				LoginName: "userloginname1",
				FullName:  "fullname1",
				Email:     "email1@example.com",
				Username:  "username1",
			},
		},
		id:        10,
		login:     "userlogin1",
		loginName: "userloginname1",
		email:     "email1@example.com",
		fullName:  "fullname1",
	}))
	require.False(t, userExists(userExistsInput{
		matchBy: giteaMatchByExact,
		users: []giteaUser{
			{
				ID:        10,
				Login:     "userlogin2",
				LoginName: "userloginname1",
				FullName:  "fullname1",
				Email:     "email1@example.com",
				Username:  "username1",
			},
		},
		id:        10,
		login:     "userlogin1",
		loginName: "userloginname1",
		email:     "email1@example.com",
		fullName:  "fullname1",
	}))
}

func TestUserExistsIfDefinedMatch(t *testing.T) {
	require.True(t, userExists(userExistsInput{
		matchBy: giteaMatchByIfDefined,
		users: []giteaUser{
			{
				ID:        10,
				Login:     "userlogin1",
				LoginName: "userloginname1",
				FullName:  "fullname1",
				Email:     "email1@example.com",
				Username:  "username1",
			},
		},
		id:        10,
		login:     "userlogin1",
		loginName: "userloginname1",
		email:     "email1@example.com",
		fullName:  "fullname1",
	}))
	require.False(t, userExists(userExistsInput{
		matchBy: giteaMatchByIfDefined,
		users: []giteaUser{
			{
				ID:        10,
				LoginName: "userloginname1",
				FullName:  "fullname1",
				Email:     "email2@example.com",
				Username:  "username1",
			},
		},
		id:        10,
		login:     "userlogin1",
		loginName: "userloginname1",
		email:     "email1@example.com",
		fullName:  "fullname1",
	}))
}
