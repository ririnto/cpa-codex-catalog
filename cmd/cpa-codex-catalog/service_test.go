package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

func TestCatalogResourceAndRegistration(t *testing.T) {
	service := newPluginService(nil)
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}
	response, err := service.handleManagement(request)
	if err != nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured response = (%+v, %v)", response, err)
	}
	catalogPath := writeTestCatalog(t, "first")
	if err := service.configure(configYAML(catalogPath, nil, "")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	registration := service.registerManagement()
	if len(registration.Resources) != 1 || registration.Resources[0].Path != "/models" {
		t.Fatalf("registered resources = %+v", registration.Resources)
	}
	response, err = service.handleManagement(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("resource response = (%+v, %v)", response, err)
	}
	if got := response.Headers.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Headers.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body, &body); err != nil || len(body["models"].([]any)) != 1 {
		t.Fatalf("invalid catalog body: %s (%v)", response.Body, err)
	}
	request.Method = http.MethodPost
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusMethodNotAllowed || response.Headers.Get("Allow") != http.MethodGet {
		t.Fatalf("unsupported method response = %+v", response)
	}
	request.Method = http.MethodGet
	request.Path = "/wrong"
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d", response.StatusCode)
	}
}

func TestAvailabilityHTTPClientHasNoActiveRequestDeadline(t *testing.T) {
	if defaultAvailabilityHTTPClient.Timeout != 0 {
		t.Fatalf("availability client timeout = %s, want none", defaultAvailabilityHTTPClient.Timeout)
	}
	transport, ok := defaultAvailabilityHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("availability transport = %T, want *http.Transport", defaultAvailabilityHTTPClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 0 {
		t.Fatalf("response header timeout = %s, want none", transport.ResponseHeaderTimeout)
	}
}

func TestBearerProtectionAndSafeReconfigure(t *testing.T) {
	t.Setenv("CPA_CATALOG_TEST_TOKEN", "local-secret-token")
	service := newPluginService(nil)
	firstPath := writeTestCatalog(t, "first")
	if err := service.configure(configYAML(firstPath, nil, "CPA_CATALOG_TEST_TOKEN")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath, Headers: make(http.Header)}
	response, _ := service.handleManagement(request)
	if response.StatusCode != http.StatusUnauthorized || response.Headers.Get("Www-Authenticate") != "Bearer" {
		t.Fatalf("missing token response = %+v", response)
	}
	if bytes.Contains(response.Body, []byte("local-secret-token")) {
		t.Fatalf("error response exposed the configured token: %s", response.Body)
	}
	request.Headers.Set("Authorization", "Bearer wrong-token")
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", response.StatusCode)
	}
	request.Headers.Set("Authorization", "Bearer local-secret-token")
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid token status = %d", response.StatusCode)
	}
	request.Headers["authorization"] = []string{"Bearer wrong-token"}
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("duplicate authorization values status = %d", response.StatusCode)
	}
	before := append([]byte(nil), service.current.Load().catalog...)
	brokenConfig := []byte("catalog_path: /private/local/catalog.json\nunknown_key: ignored\n")
	if err := service.configure(brokenConfig); err == nil || strings.Contains(err.Error(), "/private/local/catalog.json") {
		t.Fatalf("reconfigure error was absent or exposed a path: %v", err)
	}
	request.Headers = make(http.Header)
	request.Headers.Set("Authorization", "Bearer local-secret-token")
	response, _ = service.handleManagement(request)
	if response.StatusCode != http.StatusOK || !bytes.Equal(response.Body, before) {
		t.Fatalf("failed reconfigure replaced the last valid snapshot: %+v", response)
	}
}

func TestConfigureRequiresStrictValidYAMLAndPreservesSnapshot(t *testing.T) {
	service := newPluginService(nil)
	firstPath := writeTestCatalog(t, "first")
	if err := service.configure(configYAML(firstPath, nil, "")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	missingPath := filepath.Join(t.TempDir(), "missing.json")
	for _, raw := range [][]byte{
		[]byte("catalog_path: " + firstPath + "\nunknown: value\n"),
		[]byte("catalog_path: " + firstPath + "\n---\ncatalog_path: second.json\n"),
		[]byte("catalog_path: " + missingPath + "\n"),
	} {
		if err := service.configure(raw); err == nil {
			t.Fatalf("configure(%q) succeeded", raw)
		}
	}
	response, _ := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	if response.StatusCode != http.StatusOK || !bytes.Contains(response.Body, []byte("first")) {
		t.Fatalf("invalid configurations changed the active catalog: %+v", response)
	}
}

func TestDisabledConfigurationDoesNotRequireCatalog(t *testing.T) {
	service := newPluginService(nil)
	disabled := false
	if err := service.configure(configYAML("", &disabled, "MISSING_CATALOG_TOKEN")); err != nil {
		t.Fatalf("disabled configure() error = %v", err)
	}
	if registration := service.registerManagement(); len(registration.Resources) != 0 {
		t.Fatalf("disabled plugin registered resources: %+v", registration.Resources)
	}
	response, _ := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled resource status = %d", response.StatusCode)
	}
}

func TestConcurrentCatalogSnapshotReconfiguration(t *testing.T) {
	service := newPluginService(nil)
	firstPath := writeTestCatalog(t, "first")
	secondPath := writeTestCatalog(t, "second")
	if err := service.configure(configYAML(firstPath, nil, "")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	firstResponse, _ := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	if err := service.configure(configYAML(secondPath, nil, "")); err != nil {
		t.Fatalf("configure(second) error = %v", err)
	}
	secondResponse, _ := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	allowedBodies := map[string]bool{string(firstResponse.Body): true, string(secondResponse.Body): true}
	var wait sync.WaitGroup
	errors := make(chan string, 216)
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < 24; index++ {
			path := firstPath
			if index%2 == 1 {
				path = secondPath
			}
			if err := service.configure(configYAML(path, nil, "")); err != nil {
				errors <- "reconfigure failed"
			}
		}
	}()
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := 0; index < 24; index++ {
				response, err := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
				if err != nil || response.StatusCode != http.StatusOK || !allowedBodies[string(response.Body)] {
					errors <- "request observed an invalid snapshot"
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestAvailableModelsAreFetchedPerRequestWithCurrentBearerToken(t *testing.T) {
	const tokenEnv = "CPA_AVAILABLE_MODELS_TEST_TOKEN"
	t.Setenv(tokenEnv, "")
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count := requestCount.Add(1)
		if request.URL.Path != "/v1/models" || request.Method != http.MethodGet {
			t.Errorf("upstream request = %s %s, want GET /v1/models", request.Method, request.URL.Path)
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", request.Header.Get("Accept"))
		}
		if count == 1 {
			if request.Header.Get("Authorization") != "Bearer token-one" {
				t.Errorf("first Authorization = %q", request.Header.Get("Authorization"))
			}
			_, _ = writer.Write([]byte(`{"object":"list","data":[{"id":"demo","object":"model","owned_by":"fixture"}]}`))
			return
		}
		if request.Header.Get("Authorization") != "Bearer token-two" {
			t.Errorf("second Authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()
	service := newPluginService(server.Client().Transport)
	if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "configured"), server.URL+"/v1/models", tokenEnv)); err != nil {
		t.Fatalf("configure() read or rejected the deferred token: %v", err)
	}
	t.Setenv(tokenEnv, "token-one")
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}
	first, err := service.handleManagement(request)
	if err != nil || first.StatusCode != http.StatusOK {
		t.Fatalf("first resource response = (%+v, %v)", first, err)
	}
	var firstBody struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(first.Body, &firstBody); err != nil || len(firstBody.Models) != 1 || firstBody.Models[0]["display_name"] != "configured" {
		t.Fatalf("first filtered catalog = %s (%v)", first.Body, err)
	}
	t.Setenv(tokenEnv, "token-two")
	second, err := service.handleManagement(request)
	if err != nil || second.StatusCode != http.StatusOK || string(second.Body) != `{"models":[]}` {
		t.Fatalf("second resource response = (%+v, %v), want empty successful catalog", second, err)
	}
	if requestCount.Load() != 2 {
		t.Fatalf("availability requests = %d, want one per resource request", requestCount.Load())
	}
}

func TestAvailableModelsSuccessfulReconfigureChangesInventorySource(t *testing.T) {
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[{"id":"demo"}]}`))
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	defer secondUpstream.Close()
	service := newPluginService(firstUpstream.Client().Transport)
	catalogPath := writeTestCatalog(t, "configured")
	if err := service.configure(configWithAvailabilityYAML(catalogPath, firstUpstream.URL+"/v1/models", "")); err != nil {
		t.Fatalf("configure(first) error = %v", err)
	}
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}
	first, _ := service.handleManagement(request)
	if first.StatusCode != http.StatusOK || !bytes.Contains(first.Body, []byte("\"slug\":\"demo\"")) {
		t.Fatalf("first inventory response = %+v", first)
	}
	if err := service.configure(configWithAvailabilityYAML(catalogPath, secondUpstream.URL+"/v1/models", "")); err != nil {
		t.Fatalf("configure(second) error = %v", err)
	}
	second, _ := service.handleManagement(request)
	if second.StatusCode != http.StatusOK || string(second.Body) != `{"models":[]}` {
		t.Fatalf("second inventory response = %+v, want empty successful catalog", second)
	}
}

func TestAvailableModelsFailuresReturnStaticServiceUnavailable(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		extra  func(http.ResponseWriter, *http.Request)
	}{
		{name: "malformed", status: http.StatusOK, body: `{"data":[{"id":"demo","supported_in_api":"true"}]}`},
		{name: "upstream status", status: http.StatusBadGateway, body: `private upstream diagnostic`},
		{name: "redirect", status: http.StatusFound, extra: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/redirect-target", http.StatusFound)
		}},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", maxAvailableModelsResponseBytes+1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var redirectTargetRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/redirect-target" {
					redirectTargetRequests.Add(1)
					return
				}
				if testCase.extra != nil {
					testCase.extra(writer, request)
					return
				}
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			defer server.Close()
			service := newPluginService(server.Client().Transport)
			if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "must-not-leak"), server.URL+"/v1/models", "")); err != nil {
				t.Fatalf("configure() error = %v", err)
			}
			response, err := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
			if err != nil || response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("resource response = (%+v, %v), want static 503", response, err)
			}
			if string(response.Body) != `{"error":"model availability unavailable"}` || bytes.Contains(response.Body, []byte("must-not-leak")) || bytes.Contains(response.Body, []byte("private upstream diagnostic")) {
				t.Fatalf("failure response exposed stale catalog or upstream details: %s", response.Body)
			}
			if redirectTargetRequests.Load() != 0 {
				t.Fatalf("redirect target requests = %d, want zero", redirectTargetRequests.Load())
			}
		})
	}
}

func TestAvailabilityURLValidationAndFailedReconfigure(t *testing.T) {
	for _, endpoint := range []string{
		"https://models.example.test/v1/models",
		"https://models.example.test/v1/models?tenant=synthetic",
		"http://localhost/v1/models",
		"http://127.0.0.1:8333/v1/models",
		"http://[::1]:8333/v1/models",
	} {
		if err := validateAvailableModelsURL(endpoint); err != nil {
			t.Errorf("validateAvailableModelsURL(%q) error = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"http://models.example.test/v1/models",
		"ftp://models.example.test/v1/models",
		"https://user:password@models.example.test/v1/models",
		"https://models.example.test/v1/models#fragment",
		"https://models.example.test/v1/models#",
		"https://models.example.test/v1/models?client_version=0.1",
		"https://models.example.test/v1/models?CLIENT_VERSION=0.1",
		"https://models.example.test/v1/models?%63lient_version=0.1",
		"https://models.example.test/models",
		"https://models.example.test/v1%2Fmodels",
	} {
		if err := validateAvailableModelsURL(endpoint); err == nil {
			t.Errorf("validateAvailableModelsURL(%q) succeeded", endpoint)
		}
	}
	service := newPluginService(nil)
	firstPath := writeTestCatalog(t, "first")
	if err := service.configure(configYAML(firstPath, nil, "")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	for _, config := range [][]byte{
		configWithAvailabilityYAML(firstPath, "http://models.example.test/v1/models", ""),
		configWithAvailabilityYAML(firstPath, "https://models.example.test/v1/models", "INVALID-NAME"),
		configWithAvailabilityYAML(firstPath, "", "CPA_AVAILABLE_MODELS_TOKEN"),
	} {
		if err := service.configure(config); err == nil {
			t.Fatal("invalid availability configuration succeeded")
		}
	}
	response, _ := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	if response.StatusCode != http.StatusOK || !bytes.Contains(response.Body, []byte("first")) {
		t.Fatalf("failed reconfigure changed active catalog: %+v", response)
	}
}

func TestMissingAvailabilityBearerReturnsStaticServiceUnavailable(t *testing.T) {
	const tokenEnv = "CPA_MISSING_AVAILABLE_MODELS_TOKEN"
	t.Setenv(tokenEnv, "")
	var upstreamRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamRequests.Add(1) }))
	defer server.Close()
	service := newPluginService(server.Client().Transport)
	if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "configured"), server.URL+"/v1/models", tokenEnv)); err != nil {
		t.Fatalf("configure() required an environment value before a request: %v", err)
	}
	response, err := service.handleManagement(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath})
	if err != nil || response.StatusCode != http.StatusServiceUnavailable || string(response.Body) != `{"error":"model availability unavailable"}` {
		t.Fatalf("resource response = (%+v, %v), want static 503", response, err)
	}
	if upstreamRequests.Load() != 0 {
		t.Fatalf("requests made with missing token = %d, want zero", upstreamRequests.Load())
	}
}

func configYAML(path string, enabled *bool, envName string) []byte {
	config := configFile{CatalogPath: path, BearerTokenEnv: envName, Enabled: enabled}
	data, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}
	return data
}

func configWithAvailabilityYAML(path, endpoint, tokenEnv string) []byte {
	data, err := yaml.Marshal(configFile{CatalogPath: path, AvailableModelsURL: endpoint, AvailableModelsTokenEnv: tokenEnv})
	if err != nil {
		panic(err)
	}
	return data
}

func writeTestCatalog(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.json")
	catalog := "{\"models\":[{\"slug\":\"demo\",\"display_name\":\"" + name + "\",\"model_messages\":{\"instructions_template\":\"Synthetic instructions\"},\"supported_reasoning_levels\":[{\"effort\":\"low\",\"description\":\"Light\"}],\"shell_type\":\"shell_command\",\"visibility\":\"list\",\"supported_in_api\":true,\"priority\":1,\"support_verbosity\":false,\"truncation_policy\":{\"mode\":\"bytes\",\"limit\":10000},\"experimental_supported_tools\":[]}] }"
	if err := os.WriteFile(path, []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
