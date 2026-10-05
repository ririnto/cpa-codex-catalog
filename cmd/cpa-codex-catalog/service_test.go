package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	observedErrors := make(chan string, 216)
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < 24; index++ {
			path := firstPath
			if index%2 == 1 {
				path = secondPath
			}
			if err := service.configure(configYAML(path, nil, "")); err != nil {
				observedErrors <- "reconfigure failed"
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
					observedErrors <- "request observed an invalid snapshot"
				}
			}
		}()
	}
	wait.Wait()
	close(observedErrors)
	for err := range observedErrors {
		t.Error(err)
	}
}

func TestAvailableModelsAreFetchedPerRequestWithCurrentBearerToken(t *testing.T) {
	const tokenEnv = "CPA_AVAILABLE_MODELS_TEST_TOKEN"
	t.Setenv(tokenEnv, "")
	var requestCount int
	service := newPluginService(func(callbackID string, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
		requestCount++
		if callbackID != "callback-fixture" {
			t.Errorf("callback ID = %q, want callback-fixture", callbackID)
		}
		if request.Method != http.MethodGet || request.URL != "https://models.example.test/v1/models" {
			t.Errorf("inventory request = %s %s", request.Method, request.URL)
		}
		if !request.Direct || !request.DisableRedirects || request.MaxResponseBytes != maxAvailableModelsResponseBytes {
			t.Errorf("inventory transport options = %+v", request)
		}
		if request.Headers.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", request.Headers.Get("Accept"))
		}
		if requestCount == 1 {
			if request.Headers.Get("Authorization") != "Bearer token-one" {
				t.Errorf("first Authorization = %q", request.Headers.Get("Authorization"))
			}
			return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"object":"list","data":[{"id":"demo","object":"model","owned_by":"fixture"}]}`)}, nil
		}
		if request.Headers.Get("Authorization") != "Bearer token-two" {
			t.Errorf("second Authorization = %q", request.Headers.Get("Authorization"))
		}
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"object":"list","data":[]}`)}, nil
	})
	if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "configured"), "https://models.example.test/v1/models", tokenEnv)); err != nil {
		t.Fatalf("configure() read or rejected the deferred token: %v", err)
	}
	t.Setenv(tokenEnv, "token-one")
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}
	first, err := service.handleManagementWithCallback(request, "callback-fixture")
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
	second, err := service.handleManagementWithCallback(request, "callback-fixture")
	if err != nil || second.StatusCode != http.StatusOK || string(second.Body) != `{"models":[]}` {
		t.Fatalf("second resource response = (%+v, %v), want empty successful catalog", second, err)
	}
	if requestCount != 2 {
		t.Fatalf("availability requests = %d, want one per resource request", requestCount)
	}
}

func TestAvailableModelsSuccessfulReconfigureChangesInventorySource(t *testing.T) {
	service := newPluginService(func(_ string, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
		if request.URL == "https://first.example.test/v1/models" {
			return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"demo"}]}`)}, nil
		}
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`)}, nil
	})
	catalogPath := writeTestCatalog(t, "configured")
	if err := service.configure(configWithAvailabilityYAML(catalogPath, "https://first.example.test/v1/models", "")); err != nil {
		t.Fatalf("configure(first) error = %v", err)
	}
	request := pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}
	first, _ := service.handleManagementWithCallback(request, "callback-fixture")
	if first.StatusCode != http.StatusOK || !bytes.Contains(first.Body, []byte("\"slug\":\"demo\"")) {
		t.Fatalf("first inventory response = %+v", first)
	}
	if err := service.configure(configWithAvailabilityYAML(catalogPath, "https://second.example.test/v1/models", "")); err != nil {
		t.Fatalf("configure(second) error = %v", err)
	}
	second, _ := service.handleManagementWithCallback(request, "callback-fixture")
	if second.StatusCode != http.StatusOK || string(second.Body) != `{"models":[]}` {
		t.Fatalf("second inventory response = %+v, want empty successful catalog", second)
	}
}

func TestAvailableModelsFailuresReturnStaticServiceUnavailable(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		response pluginapi.HTTPResponse
		err      error
	}{
		{name: "malformed", response: pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"demo","supported_in_api":"true"}]}`)}},
		{name: "upstream status", response: pluginapi.HTTPResponse{StatusCode: http.StatusBadGateway, Body: []byte(`private upstream diagnostic`)}},
		{name: "redirect", response: pluginapi.HTTPResponse{StatusCode: http.StatusFound}},
		{name: "oversized", response: pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(strings.Repeat("x", maxAvailableModelsResponseBytes+1))}},
		{name: "host callback unavailable", err: errors.New("private callback detail")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var callbackCalls int
			service := newPluginService(func(_ string, _ pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
				callbackCalls++
				return testCase.response, testCase.err
			})
			if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "must-not-leak"), "https://models.example.test/v1/models", "")); err != nil {
				t.Fatalf("configure() error = %v", err)
			}
			response, err := service.handleManagementWithCallback(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}, "callback-fixture")
			if err != nil || response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("resource response = (%+v, %v), want static 503", response, err)
			}
			if string(response.Body) != `{"error":"model availability unavailable"}` || bytes.Contains(response.Body, []byte("must-not-leak")) || bytes.Contains(response.Body, []byte("private upstream diagnostic")) || bytes.Contains(response.Body, []byte("private callback detail")) {
				t.Fatalf("failure response exposed stale catalog or upstream details: %s", response.Body)
			}
			if callbackCalls != 1 {
				t.Fatalf("host callback calls = %d, want one", callbackCalls)
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
	var callbackCalls int
	service := newPluginService(func(_ string, _ pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
		callbackCalls++
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK}, nil
	})
	if err := service.configure(configWithAvailabilityYAML(writeTestCatalog(t, "configured"), "https://models.example.test/v1/models", tokenEnv)); err != nil {
		t.Fatalf("configure() required an environment value before a request: %v", err)
	}
	response, err := service.handleManagementWithCallback(pluginapi.ManagementRequest{Method: http.MethodGet, Path: resourcePath}, "callback-fixture")
	if err != nil || response.StatusCode != http.StatusServiceUnavailable || string(response.Body) != `{"error":"model availability unavailable"}` {
		t.Fatalf("resource response = (%+v, %v), want static 503", response, err)
	}
	if callbackCalls != 0 {
		t.Fatalf("requests made with missing token = %d, want zero", callbackCalls)
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
