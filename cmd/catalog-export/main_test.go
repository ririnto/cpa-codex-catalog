package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportCreatesPrivateFileAndRefusesOverwrite verifies export permissions and the default no-overwrite rule.
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

// TestExportForceReplacesAndHidesInputErrors verifies forced replacement and redaction of input paths in errors.
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

// TestExportForceRejectsInputAliases ensures forced output cannot replace base or override inputs through path aliases.
func TestExportForceRejectsInputAliases(t *testing.T) {
	tests := []struct {
		name       string
		outputPath func(t *testing.T, directory, basePath, overridesPath string) string
	}{
		{
			name: "base path",
			outputPath: func(_ *testing.T, _, basePath, _ string) string {
				return basePath
			},
		},
		{
			name: "relative base path",
			outputPath: func(t *testing.T, _, basePath, _ string) string {
				workingDirectory, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				relativePath, err := filepath.Rel(workingDirectory, basePath)
				if err != nil {
					t.Fatal(err)
				}
				return relativePath
			},
		},
		{
			name: "base symlink",
			outputPath: func(t *testing.T, directory, basePath, _ string) string {
				aliasPath := filepath.Join(directory, "base-alias.json")
				if err := os.Symlink(basePath, aliasPath); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				return aliasPath
			},
		},
		{
			name: "base hard link",
			outputPath: func(t *testing.T, directory, basePath, _ string) string {
				aliasPath := filepath.Join(directory, "base-alias.json")
				if err := os.Link(basePath, aliasPath); err != nil {
					t.Skipf("hard links are unavailable: %v", err)
				}
				return aliasPath
			},
		},
		{
			name: "overrides path",
			outputPath: func(_ *testing.T, _, _, overridesPath string) string {
				return overridesPath
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			basePath := filepath.Join(directory, "base.json")
			overridesPath := filepath.Join(directory, "overrides.json")
			baseData := []byte(testCatalog)
			overridesData := []byte(`{"models":{"demo":{"display_name":"Updated"}}}`)
			if err := os.WriteFile(basePath, baseData, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(overridesPath, overridesData, 0o600); err != nil {
				t.Fatal(err)
			}
			outputPath := test.outputPath(t, directory, basePath, overridesPath)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := []string{"--base", basePath, "--overrides", overridesPath, "--out", outputPath, "--force"}
			if code := run(args, &stdout, &stderr); code == 0 {
				t.Fatal("forced export accepted an output path that aliases an input")
			}
			if !strings.Contains(stderr.String(), "catalog export failed") || strings.Contains(stderr.String(), basePath) || strings.Contains(stderr.String(), overridesPath) || strings.Contains(stderr.String(), outputPath) {
				t.Fatalf("error output exposed a path or omitted the generic error: %q", stderr.String())
			}
			actualBase, err := os.ReadFile(basePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actualBase, baseData) {
				t.Fatal("forced export changed the base catalog")
			}
			actualOverrides, err := os.ReadFile(overridesPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actualOverrides, overridesData) {
				t.Fatal("forced export changed the override file")
			}
		})
	}
}

// TestExportRequiresExplicitBaseAndOutput verifies that export requires both input and output paths.
func TestExportRequiresExplicitBaseAndOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "--base and --out are required") {
		t.Fatalf("missing-path result = %d, %q", code, stderr.String())
	}
}

const testCatalog = "{\"models\":[{\"slug\":\"demo\",\"display_name\":\"Demo\",\"model_messages\":{\"instructions_template\":\"Synthetic instructions\"},\"supported_reasoning_levels\":[{\"effort\":\"low\",\"description\":\"Light\"}],\"shell_type\":\"shell_command\",\"visibility\":\"list\",\"supported_in_api\":true,\"priority\":1,\"support_verbosity\":false,\"truncation_policy\":{\"mode\":\"bytes\",\"limit\":10000},\"experimental_supported_tools\":[]}]}"
