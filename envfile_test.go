package githosts

import (
	"os"
	"testing"
)

func TestGetEnvOrFileReturnsEnvValue(t *testing.T) {
	t.Setenv("TESTVAR", "value")
	t.Setenv("TESTVAR_FILE", "/tmp/should-not-read")
	if v := getEnvOrFile("TESTVAR"); v != "value" {
		t.Fatalf("expected env value, got %q", v)
	}
}

func TestGetEnvOrFileReturnsFileValue(t *testing.T) {
	file := t.TempDir() + "/f"
	os.WriteFile(file, []byte("fileval"), 0o644)
	t.Setenv("TESTVAR", "")
	t.Setenv("TESTVAR_FILE", file)
	if v := getEnvOrFile("TESTVAR"); v != "fileval" {
		t.Fatalf("expected file value, got %q", v)
	}
}

func TestGetEnvOrFileEmptyWhenUnset(t *testing.T) {
	t.Setenv("TESTVAR", "")
	t.Setenv("TESTVAR_FILE", "")
	if v := getEnvOrFile("TESTVAR"); v != "" {
		t.Fatalf("expected empty string, got %q", v)
	}
}
