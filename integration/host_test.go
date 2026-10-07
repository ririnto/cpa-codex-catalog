package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	binary, err := filepath.Abs(filepath.Join("..", "build", "native", "cliproxyapi-v8.0.15"))
	if err != nil {
		t.Fatalf("resolve prepared host path: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("native integration requires prepared host artifacts; run `go tool task prepare-native` and `go tool task integration`: %v", err)
	}
	var requests map[string]modelListRequest
	loadSeed(t, "model-list-requests.json", &requests)
	var expected map[string]modelListExpectation
	loadSeed(t, "model-list-responses.expected.json", &expected)
	pluginConfig := loadSeedBytes(t, "plugin-config.json")
	base := startProxy(t, binary, requests["codex"], pluginConfig)
	codex := getJSONFromSeed(t, base, requests["codex"])
	models := assertModelCollection(t, codex, expected["codex"])
	if len(models) == 0 {
		t.Fatal("generated Codex catalog has no host models")
	}
	model := models[0].(map[string]any)
	if model["slug"] != expected["codex"].Slug || model["display_name"] != expected["codex"].DisplayName || model["supports_search_tool"] != expected["codex"].SupportsSearchTool {
		t.Fatalf("generated Codex model lost its host entry or inline overrides: %#v", model)
	}
	futureMetadata, ok := model["future_metadata"].(map[string]any)
	if !ok || !reflect.DeepEqual(futureMetadata, expected["codex"].FutureMetadata) {
		t.Fatalf("inline unknown metadata overrides = %#v, want %#v", model["future_metadata"], expected["codex"].FutureMetadata)
	}
	modelMessages, exists := model["model_messages"].(map[string]any)
	if !exists || modelMessages["instructions_template"] == "" {
		t.Fatalf("host Codex metadata was lost: %#v", model)
	}
	if _, exists := model["supported_reasoning_levels"].([]any); !exists {
		t.Fatalf("host reasoning metadata was lost: %#v", model)
	}
	if containsModel(models, "dormant-model") {
		t.Fatal("a dormant slug override added a host model")
	}
	baselineConfig := setPluginEnabled(t, pluginConfig, false)
	baseline := startProxy(t, binary, requests["codex"], baselineConfig)
	baselineCodex := getJSONFromSeed(t, baseline, requests["codex"])
	baselineModels := assertModelCollection(t, baselineCodex, expected["codex"])
	assertHostMetadataPreserved(t, baselineModels[0].(map[string]any), model)
	openAI := getJSONFromSeed(t, base, requests["openai"])
	if _, exists := openAI["models"]; exists {
		t.Fatalf("generic OpenAI inventory changed shape: %#v", openAI)
	}
	assertModelCollection(t, openAI, expected["openai"])
	claude := getJSONFromSeed(t, base, requests["claude"])
	assertModelCollection(t, claude, expected["claude"])
}

func startProxy(t *testing.T, binary string, codexRequest modelListRequest, pluginConfig []byte) string {
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
	var compactConfig bytes.Buffer
	if err := json.Compact(&compactConfig, pluginConfig); err != nil {
		t.Fatalf("compact synthetic plugin configuration: %v", err)
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
    cpa-codex-catalog: %s
`, port, clientKey, authDir, fixtureModelID, fixtureModelID, filepath.Join(root, "plugins"), compactConfig.String())
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
			request, err := http.NewRequest(codexRequest.Method, base+codexRequest.Path, nil)
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

type modelListRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

type modelListExpectation struct {
	Collection         string         `json:"collection"`
	Count              int            `json:"count"`
	Slug               string         `json:"slug"`
	DisplayName        string         `json:"display_name"`
	SupportsSearchTool bool           `json:"supports_search_tool"`
	FutureMetadata     map[string]any `json:"future_metadata"`
}

func getJSONFromSeed(t *testing.T, base string, seed modelListRequest) map[string]any {
	t.Helper()
	if seed.Method != http.MethodGet {
		t.Fatalf("seed request method = %q, want GET", seed.Method)
	}
	headers := make(http.Header)
	for name, value := range seed.Headers {
		headers.Set(name, value)
	}
	return getJSONWithHeaders(t, base+seed.Path, headers)
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

func loadSeed(t *testing.T, name string, value any) {
	t.Helper()
	if err := json.Unmarshal(loadSeedBytes(t, name), value); err != nil {
		t.Fatalf("decode synthetic seed %q: %v", name, err)
	}
}

func loadSeedBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1-synthetic", name))
	if err != nil {
		t.Fatalf("read synthetic seed %q: %v", name, err)
	}
	return data
}

func setPluginEnabled(t *testing.T, config []byte, enabled bool) []byte {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatalf("decode plugin config for baseline: %v", err)
	}
	decoded["enabled"] = enabled
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode plugin config for baseline: %v", err)
	}
	return encoded
}

func assertHostMetadataPreserved(t *testing.T, baseline, patched map[string]any) {
	t.Helper()
	pluginManagedFields := map[string]struct{}{
		"description":          {},
		"display_name":         {},
		"future_metadata":      {},
		"input_modalities":     {},
		"supports_search_tool": {},
	}
	for name, value := range baseline {
		if _, changedByConfig := pluginManagedFields[name]; changedByConfig {
			continue
		}
		if name == "model_messages" {
			baselineMessages, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("baseline model_messages is %T, want object", value)
			}
			patchedMessages, ok := patched[name].(map[string]any)
			if !ok {
				t.Fatalf("patched model_messages is %T, want object", patched[name])
			}
			for messageName, messageValue := range baselineMessages {
				if messageName == "tools" {
					continue
				}
				if got, exists := patchedMessages[messageName]; !exists || !reflect.DeepEqual(got, messageValue) {
					t.Fatalf("host model_messages.%s changed: got %#v, want %#v", messageName, got, messageValue)
				}
			}
			continue
		}
		if got, exists := patched[name]; !exists || !reflect.DeepEqual(got, value) {
			t.Fatalf("host model field %q changed: got %#v, want %#v", name, got, value)
		}
	}
}

func assertModelCollection(t *testing.T, response map[string]any, expected modelListExpectation) []any {
	t.Helper()
	models, ok := response[expected.Collection].([]any)
	if !ok || len(models) != expected.Count {
		t.Fatalf("response collection %q = %#v, want %d rows", expected.Collection, response[expected.Collection], expected.Count)
	}
	return models
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
