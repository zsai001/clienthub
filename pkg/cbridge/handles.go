package cbridge

import "sync"

// HandleRegistry maps integer handles to Go objects so that C callers can
// reference them by a simple int. Thread-safe.
type HandleRegistry struct {
	mu      sync.RWMutex
	objects map[int]any
	nextID  int
}

func NewHandleRegistry() *HandleRegistry {
	return &HandleRegistry{
		objects: make(map[int]any),
		nextID:  1,
	}
}

func (r *HandleRegistry) Put(obj any) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	r.objects[id] = obj
	return id
}

func (r *HandleRegistry) Get(id int) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	obj, ok := r.objects[id]
	return obj, ok
}

func (r *HandleRegistry) Delete(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.objects, id)
}

func (r *HandleRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.objects {
		delete(r.objects, id)
	}
}
