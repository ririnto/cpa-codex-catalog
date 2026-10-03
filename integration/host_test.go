package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const catalogFixture = `{
  "identity": "private-cache-identity-fixture",
  "fetched_at": "2026-10-01T12:00:00Z",
  "etag": "private-etag-fixture",
  "client_version": "0.152.0",
  "models": [
    {
      "slug": "codex-fixture-model",
      "display_name": "Codex <Fixture> & Co",
      "description": "Synthetic base description <plain> & literal",
      "default_reasoning_level": "medium",
      "supported_reasoning_levels": [
        {"effort": "low", "description": "Light synthetic reasoning"},
        {"effort": "medium", "description": "Balanced synthetic reasoning"},
        {"effort": "high", "description": "Deep synthetic reasoning"}
      ],
      "shell_type": "unified_exec",
      "visibility": "list",
      "supported_in_api": true,
      "priority": 12,
      "additional_speed_tiers": ["fast"],
      "service_tiers": [
        {"id": "priority", "name": "Fixture priority", "description": "Synthetic service tier"}
      ],
      "default_service_tier": "priority",
      "available_access_programs": null,
      "availability_nux": {"message": "Synthetic availability notice"},
      "upgrade": null,
      "model_messages": {
        "content_filter_guidance": "Synthetic filter guidance <preserved> & literal",
        "persistent_instructions": "Synthetic persistent instruction",
        "tools": null,
        "instructions_template": "Base instruction stays <unchanged> & literal.",
        "instructions_variables": null,
        "approvals": null,
        "collaboration_modes": null,
        "auto_review": null,
        "permissions": null,
        "multi_agent": null,
        "token_budget": null,
        "guardian_v2": null,
        "confirmation_policies": null
      },
      "include_skills_usage_instructions": true,
      "include_plugin_usage_instructions": true,
      "include_apps_usage_instructions": true,
      "supports_reasoning_summary_parameter": true,
      "default_reasoning_summary": "concise",
      "support_verbosity": true,
      "default_verbosity": "low",
      "apply_patch_tool_type": "freeform",
      "web_search_tool_type": "text",
      "truncation_policy": {"mode": "tokens", "limit": 8192},
      "supports_image_detail_original": true,
      "context_window": 48000,
      "max_context_window": 64000,
      "auto_compact_token_limit": 42000,
      "comp_hash": "fixture-compaction-hash",
      "effective_context_window_percent": 85,
      "experimental_supported_tools": ["fixture_tool"],
      "input_modalities": ["text", "image"],
      "supports_search_tool": false,
      "supports_experimental_context": true,
      "use_responses_lite": false,
      "supports_reasoning_effort_updates": true,
      "node_repl_auto_review_required": false,
      "node_repl_disabled": false,
      "auto_review_model_override": null,
      "model_specialty": "synthetic",
      "tool_mode": "direct",
      "multi_agent_version": "v2",
      "multi_agent_reasoning_effort": "high",
      "prefer_websockets": true,
      "minimal_client_version": "0.150.0",
      "supports_parallel_tool_calls": true,
      "requires_sandboxed_review": false
    }
  ]
}`

const overridesFixture = `{
  "defaults": {"supports_search_tool": true},
  "models": {
    "codex-fixture-model": {
      "display_name": "Overridden <Codex> & metadata",
      "priority": 77
    }
  }
}`

const resourceBearer = "fixture-resource-bearer-token"
const resourceBearerEnv = "CPA_CODEX_CATALOG_TEST_TOKEN"
const resourcePath = "/v0/resource/plugins/cpa-codex-catalog/models"

func TestNativeHostCodexCatalogResource(t *testing.T) {
	binary := os.Getenv("CPA_BINARY")
	if binary == "" {
		t.Skip("set CPA_BINARY to a built CLIProxyAPI v8 server for native host integration")
	}
	fixtureDir, err := os.MkdirTemp("", "cpa-codex-catalog-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureDir) })
	catalogPath := filepath.Join(fixtureDir, "catalog.json")
	if err := os.WriteFile(catalogPath, []byte(catalogFixture), 0600); err != nil {
		t.Fatal(err)
	}
	overridesPath := filepath.Join(fixtureDir, "overrides.json")
	if err := os.WriteFile(overridesPath, []byte(overridesFixture), 0600); err != nil {
		t.Fatal(err)
	}
	base := startProxy(t, binary, catalogPath, overridesPath)
	t.Run("BearerProtection", func(t *testing.T) {
		for _, token := range []string{"", "wrong-token"} {
			status, body, headers := getResource(t, base, token)
			if status != http.StatusUnauthorized {
				t.Fatalf("resource status for token %q = %d, want 401: %s", token, status, body)
			}
			if headers.Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", headers.Get("WWW-Authenticate"))
			}
			if bytes.Contains(body, []byte("private-cache-identity-fixture")) {
				t.Fatalf("unauthorized response exposed cache identity: %s", body)
			}
		}
	})
	t.Run("CatalogMetadataAndOverrides", func(t *testing.T) {
		status, body, _ := getResource(t, base, resourceBearer)
		if status != http.StatusOK {
			t.Fatalf("resource status = %d, want 200: %s", status, body)
		}
		var root map[string]any
		if err := json.Unmarshal(body, &root); err != nil {
			t.Fatalf("decode catalog JSON: %v: %s", err, body)
		}
		for _, key := range []string{"identity", "fetched_at", "etag", "client_version"} {
			if _, exists := root[key]; exists {
				t.Fatalf("catalog response retained cache wrapper field %q: %s", key, body)
			}
		}
		models, ok := root["models"].([]any)
		if !ok || len(models) != 1 {
			t.Fatalf("models = %#v, want one model", root["models"])
		}
		model, ok := models[0].(map[string]any)
		if !ok {
			t.Fatalf("model = %#v, want object", models[0])
		}
		if model["slug"] != "codex-fixture-model" || model["display_name"] != "Overridden <Codex> & metadata" || model["priority"] != float64(77) {
			t.Fatalf("model override fields missing: %#v", model)
		}
		if model["supports_search_tool"] != true {
			t.Fatalf("default override missing: %#v", model)
		}
		if model["context_window"] != float64(48000) || model["max_context_window"] != float64(64000) {
			t.Fatalf("unspecified base context was not preserved: %#v", model)
		}
		messages, ok := model["model_messages"].(map[string]any)
		if !ok || messages["instructions_template"] != "Base instruction stays <unchanged> & literal." || messages["persistent_instructions"] != "Synthetic persistent instruction" {
			t.Fatalf("unspecified base instructions were not preserved: %#v", model["model_messages"])
		}
		if model["comp_hash"] != "fixture-compaction-hash" || model["minimal_client_version"] != "0.150.0" {
			t.Fatalf("rich Codex metadata was lost: %#v", model)
		}
	})
}

func startProxy(t *testing.T, binary, catalogPath, overridesPath string) string {
	t.Helper()
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	pluginDir := filepath.Join(root, "plugins", runtime.GOOS, runtime.GOARCH)
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	ext := ".so"
	if runtime.GOOS == "darwin" {
		ext = ".dylib"
	}
	if runtime.GOOS == "windows" {
		ext = ".dll"
	}
	artifact, err := os.ReadFile(filepath.Join("..", "build", "plugins", runtime.GOOS, runtime.GOARCH, "cpa-codex-catalog"+ext))
	if err != nil {
		t.Fatalf("run go tool task build before host integration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "cpa-codex-catalog"+ext), artifact, 0600); err != nil {
		t.Fatal(err)
	}
	authDir := filepath.Join(root, "auths")
	if err := os.MkdirAll(authDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("config-version: 8\nserver:\n  host: 127.0.0.1\n  port: %d\nmanagement:\n  disable-control-panel: true\naccess:\n  api-keys: [fixture-client-key]\noauth:\n  auth-dir: %q\nplugins:\n  enabled: true\n  dir: %q\n  configs:\n    cpa-codex-catalog:\n      enabled: true\n      catalog_path: %q\n      overrides_path: %q\n      bearer_token_env: %q\n", port, authDir, filepath.Join(root, "plugins"), catalogPath, overridesPath, resourceBearerEnv)
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "host.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "--config", configPath, "--local-model")
	command.Dir = root
	command.Env = append(os.Environ(), resourceBearerEnv+"="+resourceBearer)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait(); _ = logFile.Close() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			log, _ := os.ReadFile(logPath)
			t.Fatalf("native host failed to load catalog resource: %s", log)
		case <-ticker.C:
			request, _ := http.NewRequest(http.MethodGet, base+resourcePath+"?client_version=0.153.1", nil)
			request.Header.Set("Authorization", "Bearer "+resourceBearer)
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return base
				}
			}
		}
	}
}

func getResource(t *testing.T, base, token string) (int, []byte, http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+resourcePath+"?client_version=0.153.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body, response.Header.Clone()
}
