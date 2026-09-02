package githosts

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub returns its quota accounting on every GraphQL response. GraphQL uses a
// points budget (default 5,000/hour) rather than a request count, so
// x-ratelimit-used is the number that matters when attributing consumption.
const (
	headerGitHubRateLimitLimit     = "X-Ratelimit-Limit"
	headerGitHubRateLimitRemaining = "X-Ratelimit-Remaining"
	headerGitHubRateLimitUsed      = "X-Ratelimit-Used"
	headerGitHubRateLimitReset     = "X-Ratelimit-Reset"
)

// githubAPIUsageLoggingEnabled reports whether GraphQL usage diagnostics should
// be emitted. GITHUB_LOG_API_USAGE takes precedence; otherwise the framework's
// existing GITHOSTS_LOG=debug switch enables it too.
func githubAPIUsageLoggingEnabled() bool {
	if v := strings.TrimSpace(os.Getenv(githubEnvVarLogAPIUsage)); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			return enabled
		}
	}

	return os.Getenv(envVarGitHostsLog) == "debug"
}

// logGitHubRateLimitHeaders logs the quota headers from a single GraphQL
// response. Absent headers are reported as "-" rather than omitted, so a
// response that carries no quota accounting at all is still visible.
func logGitHubRateLimitHeaders(resp *http.Response) {
	if resp == nil || !githubAPIUsageLoggingEnabled() {
		return
	}

	logger.Printf("GitHub GraphQL rate limit: limit=%s remaining=%s used=%s reset=%s",
		headerOrDash(resp, headerGitHubRateLimitLimit),
		headerOrDash(resp, headerGitHubRateLimitRemaining),
		headerOrDash(resp, headerGitHubRateLimitUsed),
		formatGitHubRateLimitReset(resp.Header.Get(headerGitHubRateLimitReset)))
}

func headerOrDash(resp *http.Response, name string) string {
	if v := resp.Header.Get(name); v != "" {
		return v
	}

	return "-"
}

// formatGitHubRateLimitReset renders the X-RateLimit-Reset unix timestamp as an
// absolute time plus the interval until it, which is what a reader actually
// needs. Unparsable values are passed through verbatim.
func formatGitHubRateLimitReset(v string) string {
	if v == "" {
		return "-"
	}

	secs, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return v
	}

	resetAt := time.Unix(secs, 0).UTC()

	return fmt.Sprintf("%s (in %s)", resetAt.Format(time.RFC3339), time.Until(resetAt).Round(time.Second))
}

// githubPassStats records the GraphQL requests issued, and items returned, by a
// single discovery pass (the user-repos pass, the organizations-list pass, or
// one per-organization repos pass).
type githubPassStats struct {
	Name      string
	ItemLabel string
	Requests  int
	Items     int
}

// githubAPIStats accumulates per-pass GraphQL usage across one discovery run.
// Discovery is sequential today, but the mutex keeps the counters correct if a
// caller ever shares a GitHubHost across goroutines.
type githubAPIStats struct {
	mu      sync.Mutex
	passes  []*githubPassStats
	current *githubPassStats
}

func newGitHubAPIStats() *githubAPIStats {
	return &githubAPIStats{}
}

// startPass opens a new pass and makes it the target of subsequent
// recordRequest/recordItems calls.
func (s *githubAPIStats) startPass(name, itemLabel string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pass := &githubPassStats{Name: name, ItemLabel: itemLabel}
	s.passes = append(s.passes, pass)
	s.current = pass
}

// recordRequest counts one HTTP request against the current pass. It is called
// per attempt, so rate-limit retries are counted individually.
func (s *githubAPIStats) recordRequest() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current != nil {
		s.current.Requests++
	}
}

// recordItems adds the items returned by one page to the current pass.
func (s *githubAPIStats) recordItems(n int) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current != nil {
		s.current.Items += n
	}
}

// totalRequests returns the number of GraphQL requests issued across all passes.
func (s *githubAPIStats) totalRequests() int {
	if s == nil {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var total int
	for _, pass := range s.passes {
		total += pass.Requests
	}

	return total
}

// logSummary reports the per-pass breakdown for a completed discovery run,
// along with how many repositories the dedupe step removed - the figure that
// says whether the passes overlap.
func (s *githubAPIStats) logSummary(beforeDedupe, afterDedupe int) {
	if s == nil || !githubAPIUsageLoggingEnabled() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var totalRequests int
	for _, pass := range s.passes {
		totalRequests += pass.Requests
	}

	logger.Printf("GitHub GraphQL discovery summary: %d request(s) across %d pass(es)",
		totalRequests, len(s.passes))

	for _, pass := range s.passes {
		logger.Printf("  pass %q: %d request(s), %d %s",
			pass.Name, pass.Requests, pass.Items, pass.ItemLabel)
	}

	logger.Printf("GitHub GraphQL discovery totals: %d request(s), %d repos before dedupe, %d after (%d duplicate(s) removed)",
		totalRequests, beforeDedupe, afterDedupe, beforeDedupe-afterDedupe)
}
