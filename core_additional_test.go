package githosts

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCutBySpaceAndTrimOutput(t *testing.T) {
	cases := []struct {
		in     string
		before string
		after  string
		found  bool
	}{
		{"abcd\trefs/heads/main", "abcd", "refs/heads/main", true},
		{"abcd  refs/heads/main", "abcd", "refs/heads/main", true},
		{"  abcd  refs/heads/main  ", "abcd", "refs/heads/main", true},
		{"abcd", "", "", false},
	}

	for _, c := range cases {
		b, a, f := cutBySpaceAndTrimOutput(c.in)
		if b != c.before || a != c.after || f != c.found {
			t.Errorf("unexpected result for %q: %q %q %v", c.in, b, a, f)
		}
	}
}

func TestGenerateBasicAuth(t *testing.T) {
	out := generateBasicAuth("bob", "batteryhorsestaple")
	expected := base64.StdEncoding.EncodeToString([]byte("bob:batteryhorsestaple"))
	if out != expected {
		t.Errorf("expected %s got %s", expected, out)
	}
}

func TestSetLoggerPrefix(t *testing.T) {
	prev := logger.Prefix()
	setLoggerPrefix("prefix")
	if logger.Prefix() != "prefix: " {
		t.Errorf("unexpected prefix %q", logger.Prefix())
	}
	setLoggerPrefix("")
	if logger.Prefix() != "prefix: " {
		t.Errorf("empty prefix should not change")
	}
	logger.SetPrefix(prev)
}

func TestValidDiffRemoteMethod(t *testing.T) {
	if err := validDiffRemoteMethod("clone"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validDiffRemoteMethod("refs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validDiffRemoteMethod("bad"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetDiffRemoteMethod(t *testing.T) {
	m, err := getDiffRemoteMethod("clone")
	if err != nil || m != "clone" {
		t.Fatalf("unexpected result: %s %v", m, err)
	}
	m, err = getDiffRemoteMethod("bad")
	if err == nil {
		t.Fatalf("expected error")
	}
	if m != "bad" {
		t.Fatalf("method should be returned even on error")
	}
}

func TestGetHTTPClient(t *testing.T) {
	c := getHTTPClient()
	if c == nil || c.HTTPClient == nil {
		t.Fatal("nil client")
	}
	if c.RetryMax != 2 {
		t.Errorf("expected retry max 2")
	}
	if c.HTTPClient.Timeout != 120*time.Second {
		t.Errorf("unexpected timeout")
	}
}

func TestToPtr(t *testing.T) {
	v := 10
	p := ToPtr(v)
	if *p != v {
		t.Errorf("unexpected value")
	}
}

func TestExtractDomainFromAPIUrl(t *testing.T) {
	if d := extractDomainFromAPIUrl("https://gitea.example.com/api/v1"); d != "gitea.example.com" {
		t.Errorf("unexpected domain %s", d)
	}
}

func TestGetRemoteRefs(t *testing.T) {
	tmp := t.TempDir()
	remoteDir := filepath.Join(tmp, "remote")
	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "clone", "testfiles/example-bundles/example.20221102202522.bundle", remoteDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}

	refs, err := getRemoteRefs(remoteDir)
	if err != nil {
		t.Fatalf("getRemoteRefs failed: %v", err)
	}
	if refs["refs/heads/master"] == "" || refs["refs/remotes/origin/my-branch"] == "" {
		t.Fatalf("unexpected refs %+v", refs)
	}
}
