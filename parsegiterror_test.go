package githosts

import "testing"

func TestParseGitErrorFiltersLines(t *testing.T) {
	t.Parallel()

	in := []byte("fatal: bad\nerror: nope\n info")
	out := parseGitError(in)
	if out != "fatal: bad, error: nope" {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestParseGitErrorNoPrefixes(t *testing.T) {
	t.Parallel()

	in := []byte("warning: x\nhello")
	out := parseGitError(in)
	if out != "warning: x, hello" {
		t.Fatalf("unexpected output %q", out)
	}
}
