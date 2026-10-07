package main

/*
#include <stdint.h>
#include <stdlib.h>

/// A pointer and length for request or response bytes; returned buffers use the matching API free callback.
typedef struct {
	/// Points to the first byte in the buffer.
	void* ptr;
	/// Number of bytes beginning at ptr.
	size_t len;
} cliproxy_buffer;

/// Host RPC callback: receives context, method and request bytes, fills a response buffer, and returns host status.
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
/// Releases the response buffer returned by the host call callback.
typedef void (*cliproxy_host_free_fn)(void*, size_t);

/// Host callbacks and context made available to a native plugin.
typedef struct {
	/// ABI version implemented by the host.
	uint32_t abi_version;
	/// Opaque context passed to the host call callback.
	void* host_ctx;
	/// Host RPC callback.
	cliproxy_host_call_fn call;
	/// Host buffer-release callback.
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

/// Plugin RPC callback: receives method and request bytes, fills a response buffer, and returns call status.
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
/// Releases a response buffer returned by the plugin call callback.
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
/// No-argument callback invoked when the host shuts down the plugin.
typedef void (*cliproxy_plugin_shutdown_fn)(void);

/// Function table returned to the host when the plugin is initialized.
typedef struct {
	/// ABI version supported by the plugin.
	uint32_t abi_version;
	/// Plugin RPC entry point.
	cliproxy_plugin_call_fn call;
	/// Callback that releases buffers returned by call.
	cliproxy_plugin_free_fn free_buffer;
	/// Callback invoked when the host shuts down the plugin.
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

/// Dispatches a NUL-terminated method using requestLen request bytes and writes a JSON envelope to response.
/// A zero return means an envelope was produced, which may report an RPC-level error; nonzero reports call failure.
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
/// Releases a buffer returned by cliproxyPluginCall; a null pointer is safe and the length argument is ignored.
extern void cliproxyPluginFree(void*, size_t);
/// Handles plugin shutdown; the current implementation has no shutdown work.
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
	// ConfigYAML is the plugin configuration supplied during registration or reconfiguration.
	ConfigYAML []byte `json:"config_yaml"`
}

type registration struct {
	// SchemaVersion identifies the registration schema understood by the host.
	SchemaVersion uint32 `json:"schema_version"`
	// Metadata describes this plugin to the host.
	Metadata pluginapi.Metadata `json:"metadata"`
	// Capabilities lists the hooks implemented by this plugin.
	Capabilities struct {
		// ResponseInterceptor reports whether the plugin provides response interception.
		ResponseInterceptor bool `json:"response_interceptor"`
	} `json:"capabilities"`
}

func main() {}

// cliproxy_plugin_init fills caller-owned writable plugin storage with this library's ABI version and callbacks.
// It ignores host, returns 1 when plugin is nil, and returns 0 after initialization.
//
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

// cliproxyPluginCall dispatches method using requestLen bytes at request and writes a C-allocated JSON envelope to response.
// method must be a NUL-terminated C string, request must point to requestLen readable bytes when requestLen is positive,
// and response must be writable. Release response.ptr with cliproxyPluginFree when it is non-nil.
// It returns 0 when an envelope is produced, even if that envelope reports an RPC-level error; it returns 1 for
// invalid arguments, oversized non-interception requests, or dispatch failures.
//
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

// cliproxyPluginFree releases a response buffer returned by cliproxyPluginCall; nil is safe and length is ignored.
//
//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

// cliproxyPluginShutdown handles the host shutdown callback; it currently has no work to do.
//
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
