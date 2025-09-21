package githosts

import (
	"testing"
)

func TestMaskURLCredentials(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with username and password",
			input:    "https://example-user:example-token@dev.azure.com/example-org/example-project/_git/example-project",
			expected: "https://********@dev.azure.com/example-org/example-project/_git/example-project",
		},
		{
			name:     "URL with just token",
			input:    "https://ghp_1234567890abcdef@github.com/user/repo.git",
			expected: "https://********@github.com/user/repo.git",
		},
		{
			name:     "URL without credentials",
			input:    "https://github.com/user/repo.git",
			expected: "https://github.com/user/repo.git",
		},
		{
			name:     "HTTP URL with credentials",
			input:    "http://user:pass@example.com/repo",
			expected: "http://********@example.com/repo",
		},
		{
			name:     "Non-URL string",
			input:    "/path/to/local/repo",
			expected: "/path/to/local/repo",
		},
		{
			name:     "Git command argument",
			input:    "--mirror",
			expected: "--mirror",
		},
		{
			name:     "URL with port and credentials",
			input:    "https://user:password@example.com:8080/path/to/repo",
			expected: "https://********@example.com:8080/path/to/repo",
		},
		{
			name:     "URL with encoded characters in password",
			input:    "https://user:p%40ssw0rd%21@example.com/repo",
			expected: "https://********@example.com/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskURLCredentials(tt.input)
			if result != tt.expected {
				t.Errorf("maskURLCredentials() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskGitCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name: "git clone with credentials in URL",
			args: []string{
				"git",
				"clone",
				"-v",
				"--mirror",
				"https://user:password@github.com/example-org/example-repo",
				"/repos/.working/github.com/example-org/example-repo",
			},
			expected: "git clone -v --mirror https://********@github.com/example-org/example-repo /repos/.working/github.com/example-org/example-repo",
		},
		{
			name: "git clone with Azure DevOps credentials",
			args: []string{
				"git",
				"clone",
				"-v",
				"--mirror",
				"https://example-user:example-token@dev.azure.com/example-org/example-project/_git/example-project",
				"/repos/.working/dev.azure.com/example-org/example-project/example-project",
			},
			expected: "git clone -v --mirror https://********@dev.azure.com/example-org/example-project/_git/example-project /repos/.working/dev.azure.com/example-org/example-project/example-project",
		},
		{
			name: "git clone without credentials",
			args: []string{
				"git",
				"clone",
				"-v",
				"--mirror",
				"https://github.com/public/repo.git",
				"/repos/.working/github.com/public/repo",
			},
			expected: "git clone -v --mirror https://github.com/public/repo.git /repos/.working/github.com/public/repo",
		},
		{
			name: "git with special config options",
			args: []string{
				"git",
				"-c",
				"http.followRedirects=false",
				"-c",
				"http.postBuffer=524288000",
				"clone",
				"-v",
				"--mirror",
				"https://token123@git.sr.ht/~user/repo",
				"/path/to/repo",
			},
			expected: "git -c http.followRedirects=false -c http.postBuffer=524288000 clone -v --mirror https://********@git.sr.ht/~user/repo /path/to/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskGitCommand(tt.args)
			if result != tt.expected {
				t.Errorf("maskGitCommand() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskSecretsIntegration(t *testing.T) {
	// Test that the original maskSecrets function still works
	tests := []struct {
		name     string
		content  string
		secrets  []string
		expected string
	}{
		{
			name:     "Single secret replacement",
			content:  "The password is secret123",
			secrets:  []string{"secret123"},
			expected: "The password is *****",
		},
		{
			name:     "Multiple secrets",
			content:  "User: admin, Pass: secret123, Token: token456",
			secrets:  []string{"secret123", "token456"},
			expected: "User: admin, Pass: *****, Token: *****",
		},
		{
			name:     "No secrets to mask",
			content:  "This is public information",
			secrets:  []string{},
			expected: "This is public information",
		},
		{
			name:     "Secret not in content",
			content:  "This text has no secrets",
			secrets:  []string{"password123"},
			expected: "This text has no secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskSecrets(tt.content, tt.secrets)
			if result != tt.expected {
				t.Errorf("maskSecrets() = %v, want %v", result, tt.expected)
			}
		})
	}
}
