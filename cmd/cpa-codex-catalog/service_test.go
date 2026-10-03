package main

import (
	"bytes"
	"encoding/json"
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
	service := &pluginService{}
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
	service := &pluginService{}
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
	service := &pluginService{}
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
	service := &pluginService{}
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
	service := &pluginService{}
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

func configYAML(path string, enabled *bool, envName string) []byte {
	config := configFile{CatalogPath: path, BearerTokenEnv: envName, Enabled: enabled}
	data, err := yaml.Marshal(config)
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
