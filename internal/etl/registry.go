// Registry maps stable handler keys ("mail", "git", "markdown", "facts", …) to
// a Handler. The runner (#98) resolves handlers through the Registry and calls
// them concurrently, so every method is safe for concurrent use.
package etl

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Handler processes one ETL blob/object. One Handler per format (#96): the
// registry dispatches by handler key; the runner (#98) fills in MIME/magic
// sniffing and lazy-child traversal on top of this interface.
type Handler interface {
	// Name is the stable registry key (e.g. "mail", "git", "markdown", "facts").
	Name() string
	// Handle processes one source object. Implementations are expected to be
	// safe for concurrent use.
	Handle(ctx context.Context, path string) error
}

// Registry is a concurrency-safe map of handler key → Handler.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds h under h.Name(). It returns an error if a handler with the
// same key already exists.
func (r *Registry) Register(h Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := h.Name()
	if key == "" {
		return fmt.Errorf("etl: handler with empty name")
	}
	if _, ok := r.handlers[key]; ok {
		return fmt.Errorf("etl: duplicate handler key %q", key)
	}
	r.handlers[key] = h
	return nil
}

// Lookup returns the handler registered under name. The bool reports whether
// the key is present.
func (r *Registry) Lookup(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// Names returns all registered keys in deterministic (sorted) order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of registered handlers.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers)
}
