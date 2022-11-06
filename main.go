package githosts

import (
	"log"
	"os"
	"strings"
)

const (
	workingDIRName               = ".working"
	maxIdleConns                 = 10
	idleConnTimeout              = 30
	maxRequestTime               = 10
	timeStampFormat              = "20060102150405"
	bitbucketAPIURL              = "https://api.bitbucket.org/2.0"
	githubAPIURL                 = "https://api.github.com/graphql"
	gitlabAPIURL                 = "https://gitlab.com/api/v4"
	gitlabProjectsPerPageDefault = 20
)

var logger *log.Logger

func init() {
	// allow for tests to override
	if logger == nil {
		logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
	}
}

// Backup accepts a Git hosting provider and executes the backup task for it.
func Backup(providerName, backupDIR, apiURL, compareMethod string) (err error) {
	var provider gitProvider

	switch strings.ToLower(providerName) {
	case "bitbucket":
		u := bitbucketAPIURL
		if apiURL != "" {
			u = apiURL
		}

		input := newHostInput{
			ProviderName:  "BitBucket",
			APIURL:        u,
			CompareMethod: compareMethod,
		}

		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "github":
		u := githubAPIURL
		if apiURL != "" {
			u = apiURL
		}

		input := newHostInput{
			ProviderName:  "Github",
			APIURL:        u,
			CompareMethod: compareMethod,
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "gitlab":
		u := gitlabAPIURL
		if apiURL != "" {
			u = apiURL
		}

		input := newHostInput{
			ProviderName:  "Gitlab",
			APIURL:        u,
			CompareMethod: compareMethod,
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	default:
		logger.Fatalf("unexpected provider '%s'", providerName)
	}

	provider.Backup(backupDIR)

	return err
}
