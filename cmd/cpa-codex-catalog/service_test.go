package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

var generatedCodexCatalog = []byte(`{"host_revision":"synthetic","models":[{"slug":"alpha","display_name":"Alpha","supported_in_api":true,"support_verbosity":false,"priority":1,"supported_reasoning_levels":[{"effort":"low","description":"Light"},{"effort":"high","description":"Deep"}],"experimental_supported_tools":[],"truncation_policy":{"mode":"tokens","limit":8192},"shell_type":"shell_command","visibility":"list","model_messages":{"instructions_template":"Host prompt","tools":{"multi_agent":{"spawn_agent":{"description":"Host description","parameters":"Host parameters"},"send_message":{"description":"Preserved tool"}}}},"future_metadata":{"enabled":true},"input_modalities":["text","image"]}]}`)

func TestResponseInterceptorAppliesSparseCodexOverrides(t *testing.T) {
	service := newPluginService()
	config := []byte(`defaults:
  description: Shared description
  input_modalities: [text]
  model_messages:
    tools:
      multi_agent:
        spawn_agent:
          description: Default tool description
models:
  alpha:
    display_name: Alpha override
    model_messages:
      tools:
        multi_agent:
          spawn_agent:
            parameters: Model parameters
  dormant-model:
    display_name: Dormant model
`)
	if err := service.configure(config); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	response, err := service.InterceptResponse(context.Background(), codexListRequest(generatedCodexCatalog))
	if err != nil {
		t.Fatalf("InterceptResponse() error = %v", err)
	}
	if !containsString(response.ClearHeaders, "Content-Length") || !containsString(response.ClearHeaders, "ETag") {
		t.Fatalf("changed body headers to clear = %#v", response.ClearHeaders)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body, &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["host_revision"] != "synthetic" {
		t.Fatalf("unknown host response field was lost: %#v", result)
	}
	models := result["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one host model", models)
	}
	model := models[0].(map[string]any)
	if model["slug"] != "alpha" || model["display_name"] != "Alpha override" || model["description"] != "Shared description" {
		t.Fatalf("model metadata = %#v", model)
	}
	if !equalStrings(model["input_modalities"], []string{"text"}) {
		t.Fatalf("input_modalities = %#v, want replaced array", model["input_modalities"])
	}
	if model["future_metadata"].(map[string]any)["enabled"] != true {
		t.Fatalf("future host metadata was lost: %#v", model)
	}
	message := model["model_messages"].(map[string]any)
	if message["instructions_template"] != "Host prompt" {
		t.Fatalf("host prompt was changed: %#v", message)
	}
	tools := message["tools"].(map[string]any)["multi_agent"].(map[string]any)
	spawn := tools["spawn_agent"].(map[string]any)
	if spawn["description"] != "Default tool description" || spawn["parameters"] != "Model parameters" || tools["send_message"].(map[string]any)["description"] != "Preserved tool" {
		t.Fatalf("nested override fields = %#v", tools)
	}
}

func TestResponseInterceptorPreservesEmptyAndDormantModels(t *testing.T) {
	service := newPluginService()
	if err := service.configure([]byte("models:\n  dormant-model:\n    display_name: Dormant\n")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	for _, body := range [][]byte{
		[]byte(`{"models":[]}`),
		generatedCodexCatalog,
	} {
		response, err := service.InterceptResponse(context.Background(), codexListRequest(body))
		if err != nil {
			t.Fatalf("InterceptResponse() error = %v", err)
		}
		if !bytes.Equal(response.Body, body) {
			t.Fatalf("unmatched response changed: %s", response.Body)
		}
	}
}

func TestResponseInterceptorLeavesOtherModelListsAndExecutionsAlone(t *testing.T) {
	service := newPluginService()
	if err := service.configure([]byte("defaults:\n  display_name: Patched\n")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	codexShape := []byte(`{"models":[{"slug":"alpha","model_messages":{},"supported_reasoning_levels":[]}]}`)
	tests := []struct {
		name    string
		request pluginapi.ResponseInterceptRequest
	}{
		{name: "generic OpenAI inventory", request: codexListRequest([]byte(`{"object":"list","data":[{"id":"alpha","object":"model"}]}`))},
		{name: "Claude model list", request: func() pluginapi.ResponseInterceptRequest {
			request := codexListRequest(codexShape)
			request.SourceFormat = "claude"
			return request
		}()},
		{name: "execution response with model", request: func() pluginapi.ResponseInterceptRequest {
			request := codexListRequest(codexShape)
			request.Model = "alpha"
			request.RequestedModel = "alpha"
			request.RequestBody = []byte(`{"input":[]}`)
			request.OriginalRequest = []byte(`{"model":"alpha"}`)
			return request
		}()},
		{name: "tool response with model array", request: func() pluginapi.ResponseInterceptRequest {
			request := codexListRequest(codexShape)
			request.Model = "alpha"
			request.RequestBody = []byte(`{"tools":[{"type":"function","name":"models"}]}`)
			return request
		}()},
		{name: "non-200 model response", request: func() pluginapi.ResponseInterceptRequest {
			request := codexListRequest(codexShape)
			request.StatusCode = http.StatusBadGateway
			return request
		}()},
		{name: "stream response", request: func() pluginapi.ResponseInterceptRequest {
			request := codexListRequest(codexShape)
			request.Stream = true
			return request
		}()},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response, err := service.InterceptResponse(context.Background(), testCase.request)
			if err != nil {
				t.Fatalf("InterceptResponse() error = %v", err)
			}
			if !bytes.Equal(response.Body, testCase.request.Body) {
				t.Fatalf("body changed from %s to %s", testCase.request.Body, response.Body)
			}
		})
	}
}

func TestConfigureRejectsInvalidCandidateAndKeepsActiveSnapshot(t *testing.T) {
	service := newPluginService()
	if err := service.configure([]byte("defaults:\n  description: Active\n")); err != nil {
		t.Fatalf("configure() error = %v", err)
	}
	for _, raw := range [][]byte{
		[]byte("defaults: invalid\n"),
		[]byte("unknown_field: value\n"),
		[]byte("defaults:\n  slug: changed\n"),
		[]byte("models:\n  alpha: invalid\n"),
		[]byte("models:\n  alpha:\n    priority: too-high\n"),
		[]byte("defaults:\n  description: First\n---\ndefaults:\n  description: Second\n"),
	} {
		if err := service.configure(raw); err == nil || strings.Contains(err.Error(), "too-high") {
			t.Fatalf("configure(%q) error = %v, want a redacted validation failure", raw, err)
		}
	}
	response, err := service.InterceptResponse(context.Background(), codexListRequest(generatedCodexCatalog))
	if err != nil {
		t.Fatalf("InterceptResponse() error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body, &result); err != nil {
		t.Fatal(err)
	}
	model := result["models"].([]any)[0].(map[string]any)
	if model["description"] != "Active" {
		t.Fatalf("failed configuration replaced active overrides: %#v", model)
	}
}

func TestPluginRegistrationDeclaresResponseInterceptor(t *testing.T) {
	registration := currentRegistration()
	if registration.Metadata.Version != "0.3.0" {
		t.Fatalf("plugin version = %q", registration.Metadata.Version)
	}
	if !registration.Capabilities.ResponseInterceptor {
		t.Fatal("registration did not declare ResponseInterceptor")
	}
	if len(registration.Metadata.ConfigFields) != 3 || registration.Metadata.ConfigFields[1].Name != "defaults" || registration.Metadata.ConfigFields[2].Name != "models" {
		t.Fatalf("config fields = %#v", registration.Metadata.ConfigFields)
	}
}

func TestHandleMethodReconfiguresInlineOverrides(t *testing.T) {
	service = newPluginService()
	request, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte("defaults:\n  description: RPC configured\n")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := handleMethod(pluginabi.MethodPluginRegister, request)
	if err != nil {
		t.Fatalf("handleMethod() error = %v", err)
	}
	if !bytes.Contains(result, []byte(`"response_interceptor":true`)) {
		t.Fatalf("registration response omitted interceptor capability: %s", result)
	}
	response, err := service.InterceptResponse(context.Background(), codexListRequest(generatedCodexCatalog))
	if err != nil || !bytes.Contains(response.Body, []byte("RPC configured")) {
		t.Fatalf("configured response = %s, error = %v", response.Body, err)
	}
}

func codexListRequest(body []byte) pluginapi.ResponseInterceptRequest {
	return pluginapi.ResponseInterceptRequest{
		SourceFormat:    "openai",
		ResponseHeaders: http.Header{"Content-Type": []string{"application/json"}},
		Body:            append([]byte(nil), body...),
		StatusCode:      http.StatusOK,
	}
}

func equalStrings(value any, expected []string) bool {
	values, ok := value.([]any)
	if !ok || len(values) != len(expected) {
		return false
	}
	for index, item := range values {
		if item != expected[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
