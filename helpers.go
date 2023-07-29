package githosts

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
)

const (
	backupDirMode = 0o755
)

func createDirIfAbsent(path string) error {
	return os.MkdirAll(path, backupDirMode)
}

func getTimestamp() string {
	t := time.Now()

	return t.Format(timeStampFormat)
}

func timeStampToTime(s string) (t time.Time, err error) {
	if len(s) != bundleTimestampChars {
		return time.Time{}, errors.New("invalid timestamp")
	}

	return time.Parse(timeStampFormat, s)
}

func stripTrailing(input string, toStrip string) string {
	if strings.HasSuffix(input, toStrip) {
		return input[:len(input)-len(toStrip)]
	}

	return input
}

func isEmpty(clonedRepoPath string) (bool, error) {
	remoteHeadsCmd := exec.Command("git", "count-objects", "-v")
	remoteHeadsCmd.Dir = clonedRepoPath
	out, err := remoteHeadsCmd.CombinedOutput()
	if err != nil {
		return true, errors.Wrapf(err, "failed to count objects in %s", clonedRepoPath)
	}

	cmdOutput := strings.Split(string(out), "\n")
	var looseObjects bool
	var inPackObjects bool
	var matchingLinesFound int
	for _, line := range cmdOutput {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "count:":
				matchingLinesFound++
				looseObjects = fields[1] != "0"
			case "in-pack:":
				matchingLinesFound++
				inPackObjects = fields[1] != "0"
			}
		}
	}

	if matchingLinesFound != 2 {
		return false, fmt.Errorf("failed to get object counts from %s", clonedRepoPath)
	}

	if !looseObjects && !inPackObjects {
		return true, nil
	}

	return false, nil
}

func getResponseBody(resp *http.Response) (body []byte, err error) {
	var output io.ReadCloser

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		output, err = gzip.NewReader(resp.Body)

		if err != nil {
			return
		}
	default:
		output = resp.Body

		if err != nil {
			return
		}
	}

	buf := new(bytes.Buffer)

	_, err = buf.ReadFrom(output)
	if err != nil {
		return
	}

	body = buf.Bytes()

	return
}

func maskSecrets(content string, secret []string) string {
	for _, s := range secret {
		content = strings.ReplaceAll(content, s, strings.Repeat("*", len(s)))
	}

	return content
}

type httpRequestInput struct {
	client            *retryablehttp.Client
	url               string
	method            string
	headers           http.Header
	reqBody           []byte
	secrets           []string
	basicAuthUser     string
	basicAuthPassword string
	timeout           time.Duration
}

func httpRequest(in httpRequestInput) (body []byte, headers http.Header, status int, err error) {
	if in.method == "" {
		err = fmt.Errorf("HTTP method not specified")

		return
	}

	req, err := retryablehttp.NewRequest(in.method, in.url, in.reqBody)
	if err != nil {
		err = fmt.Errorf("failed to request %s: %w", maskSecrets(in.url, in.secrets), err)

		return
	}

	req.Header = in.headers

	var resp *http.Response

	resp, err = in.client.Do(req)
	if err != nil {
		return
	}

	headers = resp.Header

	body, err = getResponseBody(resp)
	if err != nil {
		err = fmt.Errorf("%w", err)

		return
	}

	defer resp.Body.Close()

	return body, resp.Header, resp.StatusCode, err
}
