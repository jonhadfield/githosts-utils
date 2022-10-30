package githosts

import (
	"log"
	"os"
	"strings"
)

const (
	workingDIRName               = ".working"
	bundleExtension              = ".bundle"
	maxIdleConns                 = 10
	idleConnTimeout              = 30
	maxRequestTime               = 10
	bundleTimestampChars         = 14
	minBundleFileNameTokens      = 3
	timeStampFormat              = "20060102150405"
	bitbucketAPIURL              = "https://api.bitbucket.org/2.0"
	githubAPIURL                 = "https://api.github.com/graphql"
	gitlabAPIURL                 = "https://gitlab.com/api/v4"
	gitlabProjectsPerPageDefault = 20
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
}

// Backup accepts a Git hosting provider and executes the backup task for it.
func Backup(providerName, backupDIR, APIURL string) (err error) {
	var provider gitProvider

	switch strings.ToLower(providerName) {
	case "bitbucket":
		u := bitbucketAPIURL
		if APIURL != "" {
			u = APIURL
		}

		input := newHostInput{
			ProviderName: "BitBucket",
			APIURL:       u,
		}

		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "github":
		u := githubAPIURL
		if APIURL != "" {
			u = APIURL
		}

		input := newHostInput{
			ProviderName: "Github",
			APIURL:       u,
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "gitlab":
		u := gitlabAPIURL
		if APIURL != "" {
			u = APIURL
		}

		input := newHostInput{
			ProviderName: "Gitlab",
			APIURL:       u,
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
