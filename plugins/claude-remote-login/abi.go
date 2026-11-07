package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; cliproxy_host_call_fn call; cliproxy_host_free_fn free_buffer; } cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; cliproxy_plugin_call_fn call; cliproxy_plugin_free_fn free_buffer; cliproxy_plugin_shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
static const cliproxy_host_api* stored_host;
static void store_host_api(const cliproxy_host_api* host) { stored_host = host; }
static int call_host_api(const char* method, const uint8_t* request, size_t length, cliproxy_buffer* response) {
    if (!stored_host || !stored_host->call) return 1;
    return stored_host->call(stored_host->host_ctx, method, request, length, response);
}
static void free_host_buffer(void* ptr, size_t length) {
    if (stored_host && stored_host->free_buffer && ptr) stored_host->free_buffer(ptr, length);
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}
type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var application = newService(callHost)

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil || host.abi_version != 1 {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = 1
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, length C.size_t, response *C.cliproxy_buffer) (result C.int) {
	if response == nil {
		return 1
	}
	response.ptr = nil
	response.len = 0
	defer func() {
		if recover() != nil {
			writeEnvelope(response, nil, fmt.Errorf("plugin internal error"))
			result = 0
		}
	}()
	if method == nil || length > 2*1024*1024 {
		writeEnvelope(response, nil, fmt.Errorf("invalid plugin request"))
		return 0
	}
	var body []byte
	if request != nil && length > 0 {
		body = C.GoBytes(unsafe.Pointer(request), C.int(length))
	}
	value, err := application.dispatch(C.GoString(method), body)
	writeEnvelope(response, value, err)
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
	application.mu.Lock()
	application.sessions = make(map[string]*loginSession)
	application.mu.Unlock()
}

func writeEnvelope(response *C.cliproxy_buffer, value any, err error) {
	output := envelope{OK: err == nil}
	if err != nil {
		output.Error = &envelopeError{"plugin_error", err.Error()}
	} else {
		raw, errJSON := json.Marshal(value)
		if errJSON != nil {
			output.OK = false
			output.Error = &envelopeError{"encoding_error", "Unable to encode plugin response."}
		} else {
			output.Result = raw
		}
	}
	raw, errJSON := json.Marshal(output)
	if errJSON != nil {
		return
	}
	response.ptr = C.CBytes(raw)
	response.len = C.size_t(len(raw))
}
func callHost(method string, payload any) (json.RawMessage, error) {
	request, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	cRequest := C.CBytes(request)
	defer C.free(cRequest)
	var response C.cliproxy_buffer
	code := C.call_host_api(cMethod, (*C.uint8_t)(cRequest), C.size_t(len(request)), &response)
	if response.ptr == nil {
		return nil, fmt.Errorf("host callback returned no response")
	}
	defer C.free_host_buffer(response.ptr, response.len)
	raw := C.GoBytes(response.ptr, C.int(response.len))
	var output envelope
	if json.Unmarshal(raw, &output) != nil || !output.OK || code != 0 {
		return nil, fmt.Errorf("host callback failed: %s", method)
	}
	return output.Result, nil
}
