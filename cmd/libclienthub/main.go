package main

/*
#include <stdlib.h>

typedef void (*chub_log_callback)(int level, const char* msg);
typedef void (*chub_status_callback)(int handle, int status, const char* detail);
typedef void (*chub_event_callback)(int handle, const char* eventJSON);

static void call_log_cb(chub_log_callback fn, int level, const char* msg) {
    if (fn) fn(level, msg);
}
static void call_status_cb(chub_status_callback fn, int handle, int status, const char* detail) {
    if (fn) fn(handle, status, detail);
}
static void call_event_cb(chub_event_callback fn, int handle, const char* eventJSON) {
    if (fn) fn(handle, eventJSON);
}
*/
import "C"
import (
	"sync"
	"unsafe"

	"github.com/cltx/clienthub/pkg/cbridge"
)

var (
	hub     *cbridge.Hub
	hubOnce sync.Once

	cbMu     sync.RWMutex
	logCB    C.chub_log_callback
	statusCB C.chub_status_callback
	eventCB  C.chub_event_callback
)

func getHub() *cbridge.Hub {
	hubOnce.Do(func() {
		hub = cbridge.NewHub()
	})
	return hub
}

// --- Lifecycle ---

//export CHubInit
func CHubInit() {
	getHub()
}

//export CHubFree
func CHubFree() {
	if hub != nil {
		hub.Registry.Clear()
	}
}

// --- Callbacks ---

//export CHubSetLogCallback
func CHubSetLogCallback(fn C.chub_log_callback) {
	cbMu.Lock()
	logCB = fn
	cbMu.Unlock()

	getHub().Callbacks.SetLogFunc(func(level int, msg string) {
		cbMu.RLock()
		f := logCB
		cbMu.RUnlock()
		if f == nil {
			return
		}
		cs := C.CString(msg)
		defer C.free(unsafe.Pointer(cs))
		C.call_log_cb(f, C.int(level), cs)
	})
}

//export CHubSetStatusCallback
func CHubSetStatusCallback(fn C.chub_status_callback) {
	cbMu.Lock()
	statusCB = fn
	cbMu.Unlock()

	getHub().Callbacks.SetStatusFunc(func(handle, status int, detail string) {
		cbMu.RLock()
		f := statusCB
		cbMu.RUnlock()
		if f == nil {
			return
		}
		cs := C.CString(detail)
		defer C.free(unsafe.Pointer(cs))
		C.call_status_cb(f, C.int(handle), C.int(status), cs)
	})
}

//export CHubSetEventCallback
func CHubSetEventCallback(fn C.chub_event_callback) {
	cbMu.Lock()
	eventCB = fn
	cbMu.Unlock()

	getHub().Callbacks.SetEventFunc(func(handle int, eventJSON string) {
		cbMu.RLock()
		f := eventCB
		cbMu.RUnlock()
		if f == nil {
			return
		}
		cs := C.CString(eventJSON)
		defer C.free(unsafe.Pointer(cs))
		C.call_event_cb(f, C.int(handle), cs)
	})
}

// --- Client operations ---

//export CHubClientCreate
func CHubClientCreate(configJSON *C.char) C.int {
	return C.int(getHub().ClientCreate(C.GoString(configJSON)))
}

//export CHubClientStart
func CHubClientStart(handle C.int) {
	getHub().ClientStart(int(handle))
}

//export CHubClientStop
func CHubClientStop(handle C.int) {
	getHub().ClientStop(int(handle))
}

//export CHubClientDestroy
func CHubClientDestroy(handle C.int) {
	getHub().ClientDestroy(int(handle))
}

//export CHubClientGetStatus
func CHubClientGetStatus(handle C.int) *C.char {
	return C.CString(getHub().ClientGetStatus(int(handle)))
}

// --- Manager operations ---

//export CHubManagerCreate
func CHubManagerCreate(addr, secret *C.char) C.int {
	return C.int(getHub().ManagerCreate(C.GoString(addr), C.GoString(secret)))
}

//export CHubManagerListClients
func CHubManagerListClients(handle C.int) *C.char {
	return C.CString(getHub().ManagerListClients(int(handle)))
}

//export CHubManagerListTunnels
func CHubManagerListTunnels(handle C.int) *C.char {
	return C.CString(getHub().ManagerListTunnels(int(handle)))
}

//export CHubManagerListForwards
func CHubManagerListForwards(handle C.int) *C.char {
	return C.CString(getHub().ManagerListForwards(int(handle)))
}

//export CHubManagerAddForward
func CHubManagerAddForward(handle C.int, paramsJSON *C.char) *C.char {
	return C.CString(getHub().ManagerAddForward(int(handle), C.GoString(paramsJSON)))
}

//export CHubManagerRemoveForward
func CHubManagerRemoveForward(handle C.int, paramsJSON *C.char) *C.char {
	return C.CString(getHub().ManagerRemoveForward(int(handle), C.GoString(paramsJSON)))
}

//export CHubManagerKickClient
func CHubManagerKickClient(handle C.int, name *C.char) *C.char {
	return C.CString(getHub().ManagerKickClient(int(handle), C.GoString(name)))
}

//export CHubManagerStatus
func CHubManagerStatus(handle C.int) *C.char {
	return C.CString(getHub().ManagerStatus(int(handle)))
}

//export CHubManagerListExpose
func CHubManagerListExpose(handle C.int, clientName *C.char) *C.char {
	return C.CString(getHub().ManagerListExpose(int(handle), C.GoString(clientName)))
}

//export CHubManagerAddExpose
func CHubManagerAddExpose(handle C.int, paramsJSON *C.char) *C.char {
	return C.CString(getHub().ManagerAddExpose(int(handle), C.GoString(paramsJSON)))
}

//export CHubManagerRemoveExpose
func CHubManagerRemoveExpose(handle C.int, paramsJSON *C.char) *C.char {
	return C.CString(getHub().ManagerRemoveExpose(int(handle), C.GoString(paramsJSON)))
}

//export CHubManagerDestroy
func CHubManagerDestroy(handle C.int) {
	getHub().ManagerDestroy(int(handle))
}

// --- Memory management ---

//export CHubFreeString
func CHubFreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

func main() {}
