package githosts

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/require"
)

func TestHTTPRequestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}

		if r.Header.Get("X-Test") != "true" {
			t.Errorf("missing header")
		}

		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte("resp:" + string(b)))
	}))
	defer srv.Close()

	client := retryablehttp.NewClient()
	body, hdr, code, err := httpRequest(httpRequestInput{
		client:  client,
		url:     srv.URL,
		method:  http.MethodPost,
		headers: http.Header{"X-Test": {"true"}},
		reqBody: []byte("input"),
		timeout: time.Second,
	})

	require.NoError(t, err)
	require.Equal(t, 200, code)
	require.Equal(t, "resp:input", string(body))
	require.Equal(t, "text/plain; charset=utf-8", hdr.Get("Content-Type"))
}

func TestHTTPRequestNoMethod(t *testing.T) {
	client := retryablehttp.NewClient()
	_, _, _, err := httpRequest(httpRequestInput{ //nolint:dogsled // ignoring response values is intentional in test
		client: client,
		url:    "http://example.com",
	})
	require.Error(t, err)
}
