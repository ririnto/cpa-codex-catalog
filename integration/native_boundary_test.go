//go:build darwin || linux

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// TestNativeCallbackInterceptsCatalogBeyondTwoMiB verifies large native RPCs are patched and unsafe or bounded lengths are rejected.
func TestNativeCallbackInterceptsCatalogBeyondTwoMiB(t *testing.T) {
	extension := ".so"
	if runtime.GOOS == "darwin" {
		extension = ".dylib"
	}
	artifact, err := filepath.Abs(filepath.Join("..", "build", "plugins", runtime.GOOS, runtime.GOARCH, "cpa-codex-catalog"+extension))
	if err != nil {
		t.Fatalf("resolve native plugin artifact: %v", err)
	}
	plugin, err := openNativePlugin(artifact)
	if err != nil {
		t.Fatalf("load native plugin artifact: %v", err)
	}
	defer plugin.close()
	configRequest, err := json.Marshal(struct {
		ConfigYAML []byte `json:"config_yaml"`
	}{ConfigYAML: []byte("defaults:\n  description: native boundary override\n")})
	if err != nil {
		t.Fatalf("encode synthetic lifecycle request: %v", err)
	}
	status, response, err := plugin.call(pluginabi.MethodPluginRegister, configRequest)
	if err != nil {
		t.Fatalf("call native plugin registration callback: %v", err)
	}
	if status != 0 {
		t.Fatalf("registration callback status = %d, want 0", status)
	}
	var registration pluginabi.Envelope
	if err := json.Unmarshal(response, &registration); err != nil {
		t.Fatalf("decode native registration envelope: %v", err)
	}
	if !registration.OK {
		t.Fatalf("native registration returned an RPC error: %s", response)
	}
	catalog := largeSyntheticCatalog(t, loadSeedBytes(t, "generated-codex-catalog.json"), 2_097_537)
	rpcRequest, err := json.Marshal(pluginapi.ResponseInterceptRequest{
		SourceFormat: "openai",
		Body:         catalog,
		StatusCode:   http.StatusOK,
	})
	if err != nil {
		t.Fatalf("encode synthetic interception request: %v", err)
	}
	if len(rpcRequest) <= 2<<20 {
		t.Fatalf("encoded RPC request is %d bytes, want more than 2 MiB", len(rpcRequest))
	}
	status, response, err = plugin.call(pluginabi.MethodResponseInterceptAfter, rpcRequest)
	if err != nil {
		t.Fatalf("call native response interception callback: %v", err)
	}
	if status != 0 {
		t.Fatalf("response interception callback status = %d with envelope %s, want 0", status, response)
	}
	var envelope pluginabi.Envelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode native interception envelope: %v", err)
	}
	if !envelope.OK {
		t.Fatalf("native interception returned an RPC error: %s", response)
	}
	var intercepted pluginapi.ResponseInterceptResponse
	if err := json.Unmarshal(envelope.Result, &intercepted); err != nil {
		t.Fatalf("decode native interception result: %v", err)
	}
	var patched struct {
		Padding string `json:"native_padding"`
		Models  []struct {
			Slug        string `json:"slug"`
			Description string `json:"description"`
		} `json:"models"`
	}
	if err := json.Unmarshal(intercepted.Body, &patched); err != nil {
		t.Fatalf("decode native patched catalog: %v", err)
	}
	if len(patched.Models) != 1 || patched.Models[0].Slug != "codex-fixture-model" || patched.Models[0].Description != "native boundary override" {
		t.Fatalf("native callback did not apply the override to the large catalog: %#v", patched.Models)
	}
	var original struct {
		Padding string `json:"native_padding"`
	}
	if err := json.Unmarshal(catalog, &original); err != nil {
		t.Fatalf("decode original synthetic catalog: %v", err)
	}
	if patched.Padding != original.Padding {
		t.Fatal("native callback changed preserved host metadata")
	}
	boundedRequest := bytes.Repeat([]byte(" "), (2<<20)+1)
	status, response, err = plugin.call(pluginabi.MethodPluginReconfigure, boundedRequest)
	if err != nil {
		t.Fatalf("call oversized native lifecycle request: %v", err)
	}
	if status != 1 {
		t.Fatalf("oversized lifecycle callback status = %d, want 1", status)
	}
	assertNativeInvalidRequest(t, response)
	status, response, err = plugin.callWithLength(pluginabi.MethodResponseInterceptAfter, []byte("{}"), nativePluginCIntMax()+1)
	if err != nil {
		t.Fatalf("call native callback with unrepresentable length: %v", err)
	}
	if status != 1 {
		t.Fatalf("unrepresentable request length callback status = %d, want 1", status)
	}
	assertNativeInvalidRequest(t, response)
}

func assertNativeInvalidRequest(t *testing.T, response []byte) {
	t.Helper()
	var rejected pluginabi.Envelope
	if err := json.Unmarshal(response, &rejected); err != nil {
		t.Fatalf("decode safe length rejection envelope: %v", err)
	}
	if rejected.OK || rejected.Error == nil || rejected.Error.Code != "invalid_request" {
		t.Fatalf("unrepresentable request length envelope = %#v, want safe invalid_request", rejected)
	}
}

func largeSyntheticCatalog(t *testing.T, base []byte, length int) []byte {
	t.Helper()
	base = bytes.TrimSpace(base)
	if len(base) == 0 || base[len(base)-1] != '}' {
		t.Fatalf("synthetic catalog base must end with an object")
	}
	prefix := append(append([]byte(nil), base[:len(base)-1]...), []byte(`,"native_padding":"`)...)
	suffix := []byte(`"}`)
	paddingLength := length - len(prefix) - len(suffix)
	if paddingLength < 0 {
		t.Fatalf("catalog length %d is too small for synthetic fixture", length)
	}
	var catalog bytes.Buffer
	catalog.Grow(length)
	catalog.Write(prefix)
	catalog.WriteString(strings.Repeat("x", paddingLength))
	catalog.Write(suffix)
	if catalog.Len() != length {
		t.Fatalf("synthetic catalog length = %d, want %d", catalog.Len(), length)
	}
	return catalog.Bytes()
}
