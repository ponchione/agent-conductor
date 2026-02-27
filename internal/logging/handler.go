package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
)

// Handler is a slog.Handler that emits pretty-printed JSON with all attributes
// promoted to top-level keys. Groups are not supported; WithGroup returns the
// receiver unchanged.
type Handler struct {
	opts  slog.HandlerOptions
	mu    *sync.Mutex
	w     io.Writer
	attrs []slog.Attr
}

// NewHandler creates a new Handler writing to w with the given options.
// If opts is nil, default options are used (Info level).
func NewHandler(w io.Writer, opts *slog.HandlerOptions) *Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &Handler{
		opts:  *opts,
		mu:    &sync.Mutex{},
		w:     w,
		attrs: nil,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle formats the record as pretty-printed JSON and writes it to the writer.
// All pre-accumulated attrs and the record's own attrs are promoted to top-level keys.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	m := make(map[string]any, 3+len(h.attrs)+r.NumAttrs())
	m["time"] = r.Time.UTC()
	m["level"] = r.Level.String()
	m["msg"] = r.Message

	for _, a := range h.attrs {
		m[a.Key] = a.Value.Any()
	}

	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.w.Write(append(b, '\n'))
	return err
}

// WithAttrs returns a new Handler with the given attrs appended.
// The returned handler shares the same mutex and writer as the receiver.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copied := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(copied, h.attrs)
	copy(copied[len(h.attrs):], attrs)
	return &Handler{
		opts:  h.opts,
		mu:    h.mu,
		w:     h.w,
		attrs: copied,
	}
}

// WithGroup returns the receiver unchanged — groups are not used in this codebase.
func (h *Handler) WithGroup(_ string) slog.Handler {
	return h
}
