package githosts

import (
	"log"
	"net/http"
	"os"
	"time"
)

const (
	workingDIRName               = ".working"
	maxIdleConns                 = 10
	idleConnTimeout              = 30 * time.Second
	defaultHttpRequestTimeout    = 30 * time.Second
	defaultHttpClientTimeout     = 10 * time.Second
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
		logger = log.New(os.Stdout, logEntryPrefix, log.Lshortfile|log.LstdFlags)
	}
}

var httpTransport = &http.Transport{
	MaxIdleConns:       maxIdleConns,
	IdleConnTimeout:    idleConnTimeout,
	DisableCompression: true,
}

var httpClient = &http.Client{
	Transport: httpTransport,
	Timeout:   defaultHttpClientTimeout,
}
