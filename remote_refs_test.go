package githosts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRemoteRefsMatchLocalRefsTrue(t *testing.T) {
	tmpDir := t.TempDir()
	remoteDir := filepath.Join(tmpDir, "remote")
	backupDir := filepath.Join(tmpDir, "backup")

	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// clone from existing bundle to remote repo
	cmd := exec.Command("git", "clone", "testfiles/example-bundles/example.20221102202522.bundle", remoteDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	// create bundle
	cmd = exec.Command("git", "-C", remoteDir, "bundle", "create", filepath.Join(backupDir, "remote.20240101010101.bundle"), "--all")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bundle: %v %s", err, out)
	}

	if !remoteRefsMatchLocalRefs(context.Background(), remoteDir, backupDir, "") {
		t.Errorf("expected refs to match")
	}
}

func TestRemoteRefsMatchLocalRefsFalse(t *testing.T) {
	tmpDir := t.TempDir()
	remoteDir := filepath.Join(tmpDir, "remote")
	backupDir := filepath.Join(tmpDir, "backup")

	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "clone", "testfiles/example-bundles/example.20221102202522.bundle", remoteDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}

	cmd = exec.Command("git", "-C", remoteDir, "bundle", "create", filepath.Join(backupDir, "remote.20240101010101.bundle"), "--all")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bundle: %v %s", err, out)
	}

	// make a new commit in remote repo so refs differ
	if err := os.WriteFile(filepath.Join(remoteDir, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", remoteDir, "add", "file.txt").CombinedOutput(); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", remoteDir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "update").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}

	if remoteRefsMatchLocalRefs(context.Background(), remoteDir, backupDir, "") {
		t.Errorf("expected refs to differ")
	}
}
