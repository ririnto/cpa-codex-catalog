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
	"context"
	"encoding/json"
	"net/http"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const maxRequestBytes = 2 << 20

var pluginVersion = "0.3.0"
var service = newPluginService()

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  struct {
		ResponseInterceptor bool `json:"response_interceptor"`
	} `json:"capabilities"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	_ = host
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
	if method == nil || response == nil {
		writeResponse(response, errorEnvelope("invalid_request", "invalid plugin request", http.StatusBadRequest))
		return 1
	}
	methodName := C.GoString(method)
	if uint64(requestLen) > maxRequestBytes {
		if methodName == pluginabi.MethodResponseInterceptAfter {
			raw, _ := successEnvelope(pluginapi.ResponseInterceptResponse{})
			writeResponse(response, raw)
			return 0
		}
		writeResponse(response, errorEnvelope("invalid_request", "invalid plugin request", http.StatusBadRequest))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(methodName, requestBytes)
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
	case pluginabi.MethodResponseInterceptAfter:
		var interceptRequest pluginapi.ResponseInterceptRequest
		if err := json.Unmarshal(request, &interceptRequest); err != nil {
			return errorEnvelope("invalid_intercept_request", "invalid response", http.StatusBadRequest), nil
		}
		response, err := service.InterceptResponse(context.Background(), interceptRequest)
		if err != nil {
			return nil, err
		}
		return successEnvelope(response)
	default:
		return errorEnvelope("unknown_method", "unknown plugin method", http.StatusBadRequest), nil
	}
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
