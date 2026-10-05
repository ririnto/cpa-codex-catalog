package integration

import (
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

const (
	clientKey       = "fixture-client-key"
	fixtureModelID  = "codex-fixture-model"
	pluginDirectory = "cpa-codex-catalog"
)

func TestNativeHostPatchesGeneratedCodexCatalog(t *testing.T) {
	binary := os.Getenv("CPA_BINARY")
	if binary == "" {
		t.Skip("set CPA_BINARY to a built CLIProxyAPI v8 server for native host integration")
	}
	base := startProxy(t, binary)
	codex := getJSON(t, base+"/v1/models?client_version=0.153.1")
	models, ok := codex["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("generated Codex models = %#v, want the one configured host model", codex["models"])
	}
	model := models[0].(map[string]any)
	if model["slug"] != fixtureModelID || model["display_name"] != "Codex fixture override" || model["supports_search_tool"] != true {
		t.Fatalf("generated Codex model lost its host entry or inline overrides: %#v", model)
	}
	futureMetadata, ok := model["future_metadata"].(map[string]any)
	if !ok || futureMetadata["source"] != "fixture" {
		t.Fatalf("unknown model metadata was not retained: %#v", model["future_metadata"])
	}
	if _, exists := model["model_messages"].(map[string]any); !exists {
		t.Fatalf("host Codex metadata was lost: %#v", model)
	}
	if containsModel(models, "dormant-model") {
		t.Fatal("a dormant slug override added a host model")
	}

	openAI := getJSON(t, base+"/v1/models")
	if _, exists := openAI["models"]; exists {
		t.Fatalf("generic OpenAI inventory changed shape: %#v", openAI)
	}
	if rows, ok := openAI["data"].([]any); !ok || len(rows) != 1 {
		t.Fatalf("generic OpenAI inventory = %#v, want one data row", openAI["data"])
	}

	claude := getJSONWithHeaders(t, base+"/v1/models", http.Header{"Anthropic-Version": []string{"2023-06-01"}})
	if _, exists := claude["data"].([]any); !exists {
		t.Fatalf("Claude model list changed shape: %#v", claude)
	}
}

func startProxy(t *testing.T, binary string) string {
	t.Helper()
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "plugins", runtime.GOOS, runtime.GOARCH)
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ext := ".so"
	if runtime.GOOS == "darwin" {
		ext = ".dylib"
	}
	if runtime.GOOS == "windows" {
		ext = ".dll"
	}
	artifact, err := os.ReadFile(filepath.Join("..", "build", "plugins", runtime.GOOS, runtime.GOARCH, pluginDirectory+ext))
	if err != nil {
		t.Fatalf("run go tool task build before host integration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, pluginDirectory+ext), artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	authDir := filepath.Join(root, "auths")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`config-version: 8
server:
  host: 127.0.0.1
  port: %d
management:
  disable-control-panel: true
access:
  api-keys: [%s]
oauth:
  auth-dir: %q
api-keys:
  openai-compatibility:
    - name: fixture
      base-url: http://127.0.0.1:1/v1
      keys:
        - api-key: fixture-upstream-key
      models:
        - name: %s
          alias: %s
plugins:
  enabled: true
  dir: %q
  configs:
    cpa-codex-catalog:
      enabled: true
      defaults:
        supports_search_tool: true
      models:
        %s:
          display_name: Codex fixture override
          future_metadata:
            source: fixture
        dormant-model:
          display_name: Dormant model
`, port, clientKey, authDir, fixtureModelID, fixtureModelID, filepath.Join(root, "plugins"), fixtureModelID)
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "host.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--config", configPath, "--local-model")
	command.Dir = root
	command.Env = isolatedEnvironment(root)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = logFile.Close()
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			log, _ := os.ReadFile(logPath)
			t.Fatalf("native host failed to load inline catalog overrides: %s", log)
		case <-ticker.C:
			request, err := http.NewRequest(http.MethodGet, base+"/v1/models?client_version=0.153.1", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+clientKey)
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

func isolatedEnvironment(root string) []string {
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		return []string{
			"SystemRoot=" + systemRoot,
			"PATH=" + filepath.Join(systemRoot, "System32"),
			"HOME=" + root,
			"TEMP=" + root,
			"TMP=" + root,
		}
	}
	return []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root}
}

func getJSON(t *testing.T, rawURL string) map[string]any {
	return getJSONWithHeaders(t, rawURL, nil)
}

func getJSONWithHeaders(t *testing.T, rawURL string, headers http.Header) map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+clientKey)
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", rawURL, response.StatusCode, body)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode GET %s: %v: %s", rawURL, err, body)
	}
	return decoded
}

func containsModel(models []any, id string) bool {
	for _, value := range models {
		model, ok := value.(map[string]any)
		if ok && model["slug"] == id {
			return true
		}
	}
	return false
}
