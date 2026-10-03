package githosts

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetLogOutput(t *testing.T) {
	defer SetLogOutput(os.Stdout)

	var buf bytes.Buffer

	SetLogOutput(&buf)

	_, err := NewGitHubHost(NewGitHubHostInput{Token: "x"})
	require.NoError(t, err)
	require.Contains(t, buf.String(), "diff remote method", "log messages go to the writer given")

	buf.Reset()
	SetLogOutput(io.Discard)

	_, err = NewGitHubHost(NewGitHubHostInput{Token: "x"})
	require.NoError(t, err)
	require.Empty(t, buf.String())
}
