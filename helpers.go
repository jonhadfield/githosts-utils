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
	"gitlab.com/tozd/go/errors"
)

const (
	backupDirMode = 0o755
	lenSecretMask = 5
)

func createDirIfAbsent(path string) error {
	return os.MkdirAll(path, backupDirMode)
}

func getTimestamp() string {
	t := time.Now()

	return t.Format(timeStampFormat)
}

func timeStampToTime(s string) (time.Time, errors.E) {
	if len(s) != bundleTimestampChars {
		return time.Time{}, errors.New("invalid timestamp")
	}

	ptime, err := time.Parse(timeStampFormat, s)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "failed to parse timestamp")
	}

	return ptime, nil
}

func stripTrailing(input string, toStrip string) string {
	if strings.HasSuffix(input, toStrip) {
		return input[:len(input)-len(toStrip)]
	}

	return input
}

func urlWithToken(httpsURL, token string) string {
	pos := strings.Index(httpsURL, "//")
	if pos == -1 {
		return httpsURL
	}

	return fmt.Sprintf("%s%s@%s", httpsURL[:pos+2], stripTrailing(token, "\n"), httpsURL[pos+2:])
}

func urlWithBasicAuthURL(httpsURL, user, password string) string {
	parts := strings.SplitN(httpsURL, "//", 2)
	if len(parts) != 2 {
		return httpsURL
	}

	return fmt.Sprintf("%s//%s:%s@%s", parts[0], user, password, parts[1])
}

// parseGitError returns any lines from git output that contain error information.
// It looks for lines starting with "fatal:", "error:", or containing common error patterns.
// If none are found, it returns the full trimmed output.
func parseGitError(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var errs []string
	
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		
		// Check for common Git error prefixes and patterns
		if strings.HasPrefix(trimmed, "fatal:") || 
		   strings.HasPrefix(trimmed, "error:") ||
		   strings.HasPrefix(trimmed, "ERROR:") ||
		   strings.Contains(strings.ToLower(trimmed), "permission denied") ||
		   strings.Contains(strings.ToLower(trimmed), "authentication failed") ||
		   strings.Contains(strings.ToLower(trimmed), "repository not found") ||
		   strings.Contains(strings.ToLower(trimmed), "could not resolve host") ||
		   strings.Contains(strings.ToLower(trimmed), "connection refused") ||
		   strings.Contains(strings.ToLower(trimmed), "timeout") {
			errs = append(errs, trimmed)
		}
	}
	
	if len(errs) > 0 {
		return strings.Join(errs, "; ")
	}
	
	// If no specific errors found, return the full output (limit to first few lines to avoid huge messages)
	if len(lines) > 5 {
		return strings.Join(lines[:5], "; ") + "... (truncated)"
	}
	return strings.Join(lines, "; ")
}

func isEmpty(clonedRepoPath string) (bool, errors.E) {
	remoteHeadsCmd := exec.Command("git", "count-objects", "-v")
	remoteHeadsCmd.Dir = clonedRepoPath

	out, err := remoteHeadsCmd.CombinedOutput()
	if err != nil {
		gitErr := parseGitError(out)
		if gitErr != "" {
			return true, errors.Wrapf(err, "failed to count objects in %s: %s", clonedRepoPath, gitErr)
		}
		return true, errors.Wrapf(err, "failed to count objects in %s", clonedRepoPath)
	}

	loose, packed, parseErr := parseCountObjectsOutput(string(out))
	if parseErr != nil {
		return false, errors.Wrapf(parseErr, "failed to get object counts from %s", clonedRepoPath)
	}

	if !loose && !packed {
		return true, nil
	}

	return false, nil
}

func parseCountObjectsOutput(out string) (looseObjects, inPackObjects bool, err errors.E) {
	lines := strings.Split(out, "\n")
	var found int
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "count:":
				found++
				looseObjects = fields[1] != "0"
			case "in-pack:":
				found++
				inPackObjects = fields[1] != "0"
			}
		}
	}

	if found != 2 {
		return false, false, errors.New("failed to get object counts")
	}

	return looseObjects, inPackObjects, nil
}

func getResponseBody(resp *http.Response) ([]byte, error) {
	var output io.ReadCloser

	var err error

	if resp.Header.Get("Content-Encoding") == "gzip" {
		output, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to get response body: %w", err)
		}
	} else {
		output = resp.Body
	}

	buf := new(bytes.Buffer)
	if _, err = buf.ReadFrom(output); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return buf.Bytes(), nil
}

func maskSecrets(content string, secret []string) string {
	for _, s := range secret {
		content = strings.ReplaceAll(content, s, strings.Repeat("*", lenSecretMask))
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

func httpRequest(in httpRequestInput) ([]byte, http.Header, int, error) {
	if in.method == "" {
		return nil, nil, 0, fmt.Errorf("HTTP method not specified")
	}

	req, err := retryablehttp.NewRequest(in.method, in.url, in.reqBody)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to request %s: %w", maskSecrets(in.url, in.secrets), err)
	}

	req.Header = in.headers

	var resp *http.Response

	resp, err = in.client.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("request failed: %w", err)
	}

	defer func(Body io.ReadCloser) {
		if closeErr := Body.Close(); closeErr != nil {
			fmt.Printf("failed to close response body: %s\n", closeErr.Error())
		}
	}(resp.Body)

	body, err := getResponseBody(resp)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w", err)
	}

	return body, resp.Header, resp.StatusCode, err
}

func getDiffRemoteMethod(input string) (string, error) {
	if input == "" {
		return input, nil
	}

	if err := validDiffRemoteMethod(input); err != nil {
		return input, err
	}

	return input, nil
}

func remove(s []string, r string) []string {
	for i, v := range s {
		if v == r {
			return append(s[:i], s[i+1:]...)
		}
	}

	return s
}

func canonicalDiffRemoteMethod(method string) string {
	if strings.EqualFold(method, refsMethod) {
		return refsMethod
	}

	return cloneMethod
}
