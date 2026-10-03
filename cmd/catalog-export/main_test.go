package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCreatesPrivateFileAndRefusesOverwrite(t *testing.T) {
	directory := t.TempDir()
	basePath := filepath.Join(directory, "base.json")
	outputPath := filepath.Join(directory, "export.json")
	if err := os.WriteFile(basePath, []byte(testCatalog), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--base", basePath, "--out", outputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("first export returned %d: %s", code, stderr.String())
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %o, want 600", info.Mode().Perm())
	}
	if code := run([]string{"--base", basePath, "--out", outputPath}, &stdout, &stderr); code == 0 {
		t.Fatal("export replaced an existing file without --force")
	}
	if !strings.Contains(stderr.String(), "catalog export failed") || strings.Contains(stderr.String(), outputPath) {
		t.Fatalf("error output exposed a path or omitted the generic error: %q", stderr.String())
	}
}

func TestExportForceReplacesAndHidesInputErrors(t *testing.T) {
	directory := t.TempDir()
	basePath := filepath.Join(directory, "base.json")
	outputPath := filepath.Join(directory, "export.json")
	if err := os.WriteFile(basePath, []byte(testCatalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--base", basePath, "--out", outputPath, "--force"}, &stdout, &stderr); code != 0 {
		t.Fatalf("forced export returned %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Contains(data, []byte("demo")) {
		t.Fatalf("forced export content = %s, error = %v", data, err)
	}
	stderr.Reset()
	missingPath := filepath.Join(directory, "missing-secret-path.json")
	if code := run([]string{"--base", missingPath, "--out", filepath.Join(directory, "bad.json")}, &stdout, &stderr); code == 0 {
		t.Fatal("export with a missing input succeeded")
	}
	if strings.Contains(stderr.String(), missingPath) {
		t.Fatalf("failure output exposed an input path: %q", stderr.String())
	}
}

func TestExportRequiresExplicitBaseAndOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--base and --out are required") {
		t.Fatalf("missing-path result = %d, %q", code, stderr.String())
	}
}

const testCatalog = "{\"models\":[{\"slug\":\"demo\",\"display_name\":\"Demo\",\"model_messages\":{\"instructions_template\":\"Synthetic instructions\"},\"supported_reasoning_levels\":[{\"effort\":\"low\",\"description\":\"Light\"}],\"shell_type\":\"shell_command\",\"visibility\":\"list\",\"supported_in_api\":true,\"priority\":1,\"support_verbosity\":false,\"truncation_policy\":{\"mode\":\"bytes\",\"limit\":10000},\"experimental_supported_tools\":[]}]}"
