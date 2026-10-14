//go:build darwin || linux

package integration

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <limits.h>
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>

typedef struct {
	void *ptr;
	size_t len;
} native_buffer;

typedef int (*native_plugin_call_fn)(char *, uint8_t *, size_t, native_buffer *);
typedef void (*native_plugin_free_fn)(void *, size_t);

typedef struct {
	void *library;
	native_plugin_call_fn call;
	native_plugin_free_fn free_buffer;
} native_plugin;

static native_plugin *native_plugin_open(const char *path, const char **error_message) {
	void *library = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (library == NULL) {
		*error_message = dlerror();
		return NULL;
	}
	dlerror();
	native_plugin_call_fn call = (native_plugin_call_fn)dlsym(library, "cliproxyPluginCall");
	const char *lookup_error = dlerror();
	if (lookup_error != NULL) {
		*error_message = lookup_error;
		dlclose(library);
		return NULL;
	}
	dlerror();
	native_plugin_free_fn free_buffer = (native_plugin_free_fn)dlsym(library, "cliproxyPluginFree");
	lookup_error = dlerror();
	if (lookup_error != NULL) {
		*error_message = lookup_error;
		dlclose(library);
		return NULL;
	}
	native_plugin *plugin = (native_plugin *)malloc(sizeof(native_plugin));
	if (plugin == NULL) {
		*error_message = "allocate native plugin handle";
		dlclose(library);
		return NULL;
	}
	plugin->library = library;
	plugin->call = call;
	plugin->free_buffer = free_buffer;
	return plugin;
}

static void native_plugin_close(native_plugin *plugin) {
	if (plugin != NULL) {
		dlclose(plugin->library);
		free(plugin);
	}
}

static int native_plugin_call(native_plugin *plugin, char *method, uint8_t *request, size_t request_len, native_buffer *response) {
	return plugin->call(method, request, request_len, response);
}

static void native_plugin_free_response(native_plugin *plugin, native_buffer *response) {
	plugin->free_buffer(response->ptr, response->len);
}

static int native_plugin_c_int_max(void) {
	return INT_MAX;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

type nativePlugin struct {
	plugin *C.native_plugin
}

func openNativePlugin(path string) (*nativePlugin, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var errorMessage *C.char
	plugin := C.native_plugin_open(cPath, &errorMessage)
	if plugin == nil {
		if errorMessage == nil {
			return nil, errors.New("native plugin could not be loaded")
		} else {
			return nil, fmt.Errorf("native plugin could not be loaded: %s", C.GoString(errorMessage))
		}
	} else {
		return &nativePlugin{plugin: plugin}, nil
	}
}

func (plugin *nativePlugin) close() {
	if plugin.plugin != nil {
		C.native_plugin_close(plugin.plugin)
		plugin.plugin = nil
	}
}

func (plugin *nativePlugin) call(method string, request []byte) (int, []byte, error) {
	return plugin.callWithLength(method, request, uint64(len(request)))
}

func (plugin *nativePlugin) callWithLength(method string, request []byte, requestLen uint64) (int, []byte, error) {
	if plugin.plugin == nil {
		return 0, nil, errors.New("native plugin is closed")
	}
	nativeLength := C.size_t(requestLen)
	if uint64(nativeLength) != requestLen {
		return 0, nil, errors.New("native request length does not fit size_t")
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var requestPointer *C.uint8_t
	if len(request) > 0 {
		requestPointer = (*C.uint8_t)(unsafe.Pointer(&request[0]))
	}
	var response C.native_buffer
	status := int(C.native_plugin_call(plugin.plugin, cMethod, requestPointer, nativeLength, &response))
	if response.ptr != nil {
		defer C.native_plugin_free_response(plugin.plugin, &response)
	}
	if response.ptr == nil {
		return status, nil, nil
	} else if uint64(response.len) > uint64(C.INT_MAX) {
		return status, nil, errors.New("native response length does not fit C.int")
	} else {
		return status, C.GoBytes(response.ptr, C.int(response.len)), nil
	}
}

func nativePluginCIntMax() uint64 {
	return uint64(C.native_plugin_c_int_max())
}
