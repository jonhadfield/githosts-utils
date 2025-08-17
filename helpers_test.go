package githosts

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskSecretsReplacesSecretsWithAsterisks(t *testing.T) {
	content := "Hello, my secret is secret123"
	secrets := []string{"secret123"}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, "Hello, my secret is *****", maskedContent)
}

func TestMaskSecretsHandlesMultipleSecrets(t *testing.T) {
	content := "Hello, my secrets are secret123 and secret456"
	secrets := []string{"secret123", "secret456"}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, "Hello, my secrets are ***** and *****", maskedContent)
}

func TestMaskSecretsReturnsOriginalContentWhenNoSecrets(t *testing.T) {
	content := "Hello, I have no secrets"
	secrets := []string{}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, content, maskedContent)
}

func TestMaskSecretsDoesNotAlterContentWithoutSecrets(t *testing.T) {
	content := "Hello, my secret is not here"
	secrets := []string{"secret123"}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, content, maskedContent)
}

func TestCreateDirIfAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "sub")
	err := createDirIfAbsent(path)
	assert.NoError(t, err)

	info, statErr := os.Stat(path)
	assert.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestStripTrailing(t *testing.T) {
	assert.Equal(t, "hello", stripTrailing("hello\n", "\n"))
	assert.Equal(t, "world", stripTrailing("world", "\n"))
}

func TestURLHelpers(t *testing.T) {
	u := urlWithToken("https://example.com/repo.git", "tok")
	assert.Equal(t, "https://tok@example.com/repo.git", u)

	u = urlWithToken("noscheme", "tok")
	assert.Equal(t, "noscheme", u)

	u = urlWithBasicAuthURL("https://example.com/repo.git", "u", "p")
	assert.Equal(t, "https://u:p@example.com/repo.git", u)

	u = urlWithBasicAuthURL("noscheme", "u", "p")
	assert.Equal(t, "noscheme", u)
}

func TestTimeStampToTime(t *testing.T) {
	ts := getTimestamp()
	parsed, err := timeStampToTime(ts)
	assert.NoError(t, err)
	assert.NotZero(t, parsed)

	_, err = timeStampToTime("bad")
	assert.Error(t, err)
}

func TestParseCountObjectsOutput(t *testing.T) {
	loose, packed, err := parseCountObjectsOutput("count: 0\nin-pack: 0\n")
	assert.NoError(t, err)
	assert.False(t, loose)
	assert.False(t, packed)

	_, _, err = parseCountObjectsOutput("count: 1\n")
	assert.Error(t, err)
}

func TestIsEmpty(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", repo)
	assert.NoError(t, cmd.Run())

	empty, err := isEmpty(repo)
	assert.NoError(t, err)
	assert.True(t, empty)

	f := filepath.Join(repo, "file.txt")
	assert.NoError(t, os.WriteFile(f, []byte("content"), 0o600))
	assert.NoError(t, exec.Command("git", "-C", repo, "add", "file.txt").Run())
	assert.NoError(t, exec.Command("git", "-C", repo, "-c", "user.email=a@b", "-c", "user.name=n", "commit", "-m", "c").Run())

	empty, err = isEmpty(repo)
	assert.NoError(t, err)
	assert.False(t, empty)
}

func TestGetResponseBody(t *testing.T) {
	b := bytes.NewBufferString("hello")
	resp := &http.Response{Body: io.NopCloser(b), Header: http.Header{}}
	out, err := getResponseBody(resp)
	assert.NoError(t, err)
	assert.Equal(t, "hello", string(out))

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	_, _ = gz.Write([]byte("hello"))
	gz.Close()

	resp = &http.Response{Body: io.NopCloser(&gzBuf), Header: http.Header{"Content-Encoding": []string{"gzip"}}}
	out, err = getResponseBody(resp)
	assert.NoError(t, err)
	assert.Equal(t, "hello", string(out))
}

func TestGetBundleRefs(t *testing.T) {
	refs, err := getBundleRefs("testfiles/example-bundles/example.20221102202522.bundle")
	assert.NoError(t, err)
	assert.Equal(t, "2c84a508078d81eae0246ae3f3097befd0bcb7dc", refs["refs/heads/master"])
}

func TestRemoveNotFound(t *testing.T) {
	s := []string{"a", "b", "c"}
	out := remove(s, "z")
	require.Equal(t, s, out)
}
