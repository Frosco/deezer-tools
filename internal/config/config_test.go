package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `arl = "abc123"`+"\n", 0600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ARL != "abc123" {
		t.Errorf("ARL = %q, want %q", cfg.ARL, "abc123")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.toml")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) && !errorContains(err, "not found") {
		t.Errorf("error should indicate missing file, got: %v", err)
	}
}

func TestLoad_WorldReadablePermsRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm checks unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `arl = "abc123"`+"\n", 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for 0644 perms, got nil")
	}
	if !errorContains(err, "permissions") {
		t.Errorf("error should mention permissions, got: %v", err)
	}
}

func TestLoad_MissingARL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `# empty`+"\n", 0600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing arl, got nil")
	}
	if !errorContains(err, "arl") {
		t.Errorf("error should mention arl, got: %v", err)
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `not = valid = toml`, 0600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func errorContains(err error, sub string) bool {
	return err != nil && containsFold(err.Error(), sub)
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a := s[i+j]
			b := sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
