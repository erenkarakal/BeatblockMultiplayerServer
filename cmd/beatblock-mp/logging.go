package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

type compactHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	attrs []slog.Attr
}

func newLogger(w io.Writer) *slog.Logger {
	return slog.New(&compactHandler{
		mu: &sync.Mutex{},
		w:  w,
	})
}

func (h *compactHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *compactHandler) Handle(_ context.Context, r slog.Record) error {
	level := "i"
	switch {
	case r.Level >= slog.LevelError:
		level = "e"
	case r.Level >= slog.LevelWarn:
		level = "w"
	}

	line := fmt.Sprintf("[%s] %s %s", level, r.Time.Format("15:04:05"), r.Message)

	appendAttr := func(a slog.Attr) {
		if a.Key == "" {
			return
		}
		value := a.Value.Any()
		if value == nil {
			return
		}
		line += fmt.Sprintf(" %s=%v", a.Key, value)
	}
	for _, attr := range h.attrs {
		appendAttr(attr)
	}
	r.Attrs(func(attr slog.Attr) bool {
		appendAttr(attr)
		return true
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := fmt.Fprintln(h.w, line)
	return err
}

func (h *compactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := &compactHandler{
		mu: h.mu,
		w:  h.w,
	}
	next.attrs = append(next.attrs, h.attrs...)
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *compactHandler) WithGroup(string) slog.Handler {
	return h
}
