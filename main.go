package githosts

import (
	"log"
	"os"
	"strconv"
	"time"
)

const (
	workingDIRName               = ".working"
	maxIdleConns                 = 10
	idleConnTimeout              = 30 * time.Second
	defaultHttpRequestTimeout    = 30 * time.Second
	timeStampFormat              = "20060102150405"
	bitbucketAPIURL              = "https://api.bitbucket.org/2.0"
	githubAPIURL                 = "https://api.github.com/graphql"
	gitlabAPIURL                 = "https://gitlab.com/api/v4"
	gitlabProjectsPerPageDefault = 20
	sourcehutAPIURL              = "https://git.sr.ht/query"
	contentTypeApplicationJSON   = "application/json; charset=utf-8"

	// Concurrency limits
	defaultMaxConcurrentGitHub = 10
	defaultMaxConcurrentGitLab = 5
	defaultMaxConcurrentOther  = 10

	// Timeout values
	backupTimeout = 120 * time.Second

	// HTTP Headers
	HeaderContentType   = "Content-Type"
	HeaderAuthorization = "Authorization"
	HeaderAccept        = "Accept"

	// Authentication prefixes
	AuthPrefixBearer = "Bearer "
	AuthPrefixToken  = "token "
	AuthPrefixBasic  = "Basic "

	// Content types
	ContentTypeJSON        = "application/json"
	ContentTypeFormEncoded = "application/x-www-form-urlencoded"
	ContentTypeAny         = "*/*"
)

var logger *log.Logger

type WorkerConfig struct {
	LogLevel             int
	BackupDir            string
	DiffRemoteMethod     string
	BackupsToKeep        int
	BackupLFS            bool
	DefaultDelay         int
	DelayEnvVar          string
	Secrets              []string
	SetupRepo            func(*repository) // Function to set up authentication on the repo
	EncryptionPassphrase string
}

func genericWorker(config WorkerConfig, jobs <-chan repository, results chan<- RepoBackupResults) {
	for repo := range jobs {
		// Set up authentication for the repo
		if config.SetupRepo != nil {
			config.SetupRepo(&repo)
		}

		err := processBackup(processBackupInput{
			LogLevel:             config.LogLevel,
			Repo:                 repo,
			BackupDIR:            config.BackupDir,
			BackupsToKeep:        config.BackupsToKeep,
			DiffRemoteMethod:     config.DiffRemoteMethod,
			BackupLFS:            config.BackupLFS,
			Secrets:              config.Secrets,
			EncryptionPassphrase: config.EncryptionPassphrase,
		})
		results <- repoBackupResult(repo, err)

		// Add delay between repository backups to prevent rate limiting
		delay := config.DefaultDelay

		if config.DelayEnvVar != "" {
			if envDelay, sErr := strconv.Atoi(os.Getenv(config.DelayEnvVar)); sErr == nil {
				delay = envDelay
			}
		}

		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

func init() {
	// allow for tests to override
	if logger == nil {
		logger = log.New(os.Stdout, logEntryPrefix, log.Lshortfile|log.LstdFlags)
	}
}
