package cbridge

import "sync"

// Status constants exposed to callers.
const (
	StatusDisconnected = 0
	StatusConnecting   = 1
	StatusConnected    = 2
	StatusReconnecting = 3
)

// LogFunc is called with (level, message). Levels: 0=debug, 1=info, 2=warn, 3=error.
type LogFunc func(level int, msg string)

// StatusFunc is called with (handle, status, detail).
type StatusFunc func(handle, status int, detail string)

// EventFunc is called with (handle, eventJSON).
type EventFunc func(handle int, eventJSON string)

// Callbacks holds the registered callback functions.
type Callbacks struct {
	mu       sync.RWMutex
	logFn    LogFunc
	statusFn StatusFunc
	eventFn  EventFunc
}

func NewCallbacks() *Callbacks {
	return &Callbacks{}
}

func (cb *Callbacks) SetLogFunc(fn LogFunc)       { cb.mu.Lock(); cb.logFn = fn; cb.mu.Unlock() }
func (cb *Callbacks) SetStatusFunc(fn StatusFunc)  { cb.mu.Lock(); cb.statusFn = fn; cb.mu.Unlock() }
func (cb *Callbacks) SetEventFunc(fn EventFunc)    { cb.mu.Lock(); cb.eventFn = fn; cb.mu.Unlock() }

func (cb *Callbacks) FireLog(level int, msg string) {
	cb.mu.RLock()
	fn := cb.logFn
	cb.mu.RUnlock()
	if fn != nil {
		fn(level, msg)
	}
}

func (cb *Callbacks) FireStatus(handle, status int, detail string) {
	cb.mu.RLock()
	fn := cb.statusFn
	cb.mu.RUnlock()
	if fn != nil {
		fn(handle, status, detail)
	}
}

func (cb *Callbacks) FireEvent(handle int, eventJSON string) {
	cb.mu.RLock()
	fn := cb.eventFn
	cb.mu.RUnlock()
	if fn != nil {
		fn(handle, eventJSON)
	}
}
