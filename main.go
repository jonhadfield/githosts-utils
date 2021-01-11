package githosts

import (
	"log"
	"os"
)

const (
	workingDIRName          = ".working"
	bundleExtension         = ".bundle"
	maxIdleConns            = 10
	idleConnTimeout         = 30
	maxRequestTime          = 10
	bundleTimestampChars    = 14
	minBundleFileNameTokens = 3
	timeStampFormat         = "20060102150405"
)

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "soba: ", log.Lshortfile|log.LstdFlags)
}

// Backup accepts a Git hosting provider and executes the backup task for it.
func Backup(providerName, backupDIR string) (err error) {
	var provider gitProvider

	switch providerName {
	case "bitbucket":
		input := newHostInput{
			ProviderName: "BitBucket",
			APIURL:       "https://api.bitbucket.org/2.0",
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "github":
		input := newHostInput{
			ProviderName: "Github",
			APIURL:       "https://api.github.com/graphql",
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	case "gitlab":
		input := newHostInput{
			ProviderName: "Gitlab",
			APIURL:       "https://gitlab.com/api/v4",
		}
		provider, err = createHost(input)

		if err != nil {
			return
		}
	}

	provider.Backup(backupDIR)

	return err
}
