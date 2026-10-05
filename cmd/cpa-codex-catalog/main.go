package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static void clear_host_api(void) {
	stored_host = NULL;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (ptr == NULL) {
		return;
	}
	if (stored_host != NULL && stored_host->free_buffer != NULL) {
		stored_host->free_buffer(ptr, len);
		return;
	}
	free(ptr);
}

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const maxRequestBytes = 2 << 20

var pluginVersion = "0.2.0"
var service = newPluginService(callHostHTTPDoBounded)

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type managementRPCRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type hostHTTPBoundedRequest struct {
	pluginapi.HTTPRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  struct {
		ManagementAPI bool `json:"management_api"`
	} `json:"capabilities"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil || response == nil || uint64(requestLen) > maxRequestBytes {
		writeResponse(response, errorEnvelope("invalid_request", "invalid plugin request", http.StatusBadRequest))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", "plugin request failed", http.StatusInternalServerError))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	C.clear_host_api()
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var lifecycle lifecycleRequest
		if err := json.Unmarshal(request, &lifecycle); err != nil {
			return errorEnvelope("invalid_config", "invalid plugin configuration", http.StatusBadRequest), nil
		}
		if err := service.configure(lifecycle.ConfigYAML); err != nil {
			return errorEnvelope("invalid_config", "invalid plugin configuration", http.StatusBadRequest), nil
		}
		return successEnvelope(currentRegistration())
	case pluginabi.MethodManagementRegister:
		return successEnvelope(service.registerManagement())
	case pluginabi.MethodManagementHandle:
		var managementRequest managementRPCRequest
		if err := json.Unmarshal(request, &managementRequest); err != nil {
			return errorEnvelope("invalid_management_request", "invalid resource request", http.StatusBadRequest), nil
		}
		response, err := service.handleManagementWithCallback(managementRequest.ManagementRequest, managementRequest.HostCallbackID)
		if err != nil {
			return nil, err
		}
		return successEnvelope(response)
	default:
		return errorEnvelope("unknown_method", "unknown plugin method", http.StatusBadRequest), nil
	}
}

func callHostHTTPDoBounded(callbackID string, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	if strings.TrimSpace(callbackID) == "" || !request.Direct || !request.DisableRedirects || request.MaxResponseBytes <= 0 {
		return pluginapi.HTTPResponse{}, errors.New("bounded host HTTP callback is unavailable")
	}
	rawRequest, errMarshal := json.Marshal(hostHTTPBoundedRequest{HTTPRequest: request, HostCallbackID: callbackID})
	if errMarshal != nil {
		return pluginapi.HTTPResponse{}, errors.New("bounded host HTTP callback is unavailable")
	}
	cMethod := C.CString(pluginabi.MethodHostHTTPDoBounded)
	defer C.free(unsafe.Pointer(cMethod))
	requestBuffer := C.CBytes(rawRequest)
	defer C.free(requestBuffer)
	var response C.cliproxy_buffer
	callStatus := C.call_host_api(cMethod, (*C.uint8_t)(requestBuffer), C.size_t(len(rawRequest)), &response)
	if response.ptr != nil {
		defer C.free_host_buffer(response.ptr, response.len)
	}
	if callStatus != 0 || response.ptr == nil || response.len == 0 {
		return pluginapi.HTTPResponse{}, errors.New("bounded host HTTP callback failed")
	}
	rawResponse := C.GoBytes(unsafe.Pointer(response.ptr), C.int(response.len))
	var envelope pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &envelope); errUnmarshal != nil || !envelope.OK {
		return pluginapi.HTTPResponse{}, errors.New("bounded host HTTP callback failed")
	}
	var result pluginapi.HTTPResponse
	if errUnmarshal := json.Unmarshal(envelope.Result, &result); errUnmarshal != nil {
		return pluginapi.HTTPResponse{}, errors.New("bounded host HTTP callback failed")
	}
	return result, nil
}

func currentRegistration() registration {
	result := registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginID,
			Version:          pluginVersion,
			Author:           "ririnto",
			GitHubRepository: "https://github.com/ririnto/cpa-codex-catalog",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enables the catalog resource. Defaults to true."},
				{Name: "catalog_path", Type: pluginapi.ConfigFieldTypeString, Description: "Required when enabled. Path to the base Codex model catalog, relative to the plugin process working directory when not absolute."},
				{Name: "overrides_path", Type: pluginapi.ConfigFieldTypeString, Description: "Optional overrides JSON path, relative to the plugin process working directory when not absolute."},
				{Name: "bearer_token_env", Type: pluginapi.ConfigFieldTypeString, Description: "Optional environment variable name for bearer protection of the resource."},
				{Name: "available_models_url", Type: pluginapi.ConfigFieldTypeString, Description: "Optional authenticated host /v1/models URL for filtering catalog membership."},
				{Name: "available_models_token_env", Type: pluginapi.ConfigFieldTypeString, Description: "Optional environment variable name for the model inventory bearer token."},
			},
		},
	}
	result.Capabilities.ManagementAPI = true
	return result
}

func successEnvelope(result any) ([]byte, error) {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return errorEnvelope("plugin_error", "plugin request failed", http.StatusInternalServerError), nil
	}
	return json.Marshal(pluginabi.Envelope{OK: true, Result: resultBytes})
}

func errorEnvelope(code, message string, status int) []byte {
	result, err := pluginabi.NewErrorEnvelope(code, message, status)
	if err != nil {
		return []byte("{\"ok\":false,\"error\":{\"code\":\"plugin_error\",\"message\":\"plugin request failed\"}}")
	}
	return result
}

func writeResponse(response *C.cliproxy_buffer, data []byte) {
	if response == nil {
		return
	}
	if len(data) == 0 {
		return
	}
	buffer := C.CBytes(data)
	response.ptr = buffer
	response.len = C.size_t(len(data))
}
