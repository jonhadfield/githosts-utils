package githosts

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMaskSecretsReplacesSecretsWithAsterisks(t *testing.T) {
	content := "Hello, my secret is secret123"
	secrets := []string{"secret123"}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, "Hello, my secret is *********", maskedContent)
}

func TestMaskSecretsHandlesMultipleSecrets(t *testing.T) {
	content := "Hello, my secrets are secret123 and secret456"
	secrets := []string{"secret123", "secret456"}

	maskedContent := maskSecrets(content, secrets)

	assert.Equal(t, "Hello, my secrets are ********* and *********", maskedContent)
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
