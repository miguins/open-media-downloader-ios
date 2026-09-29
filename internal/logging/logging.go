// Package logging attaches trace IDs and other request- or job-scoped attributes to structured logs.
package logging

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

// TraceKey is the attribute that correlates every log line of one request, job, or background pass.
const TraceKey = "trace_id"

type scopeKey struct{}

// scope holds attributes shared by every context derived from the one that created it.
// It is mutable so inner handlers can enrich logs written by outer ones, such as the access log.
type scope struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

func (s *scope) set(attrs []slog.Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, attr := range attrs {
		index := slices.IndexFunc(s.attrs, func(existing slog.Attr) bool { return existing.Key == attr.Key })
		if index >= 0 {
			s.attrs[index] = attr
		} else {
			s.attrs = append(s.attrs, attr)
		}
	}
}

func (s *scope) snapshot() []slog.Attr {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.attrs)
}

func scopeFrom(ctx context.Context) *scope {
	s, _ := ctx.Value(scopeKey{}).(*scope)

	return s
}

// With returns a child of ctx with a new scope holding the parent's attributes, with args replacing
// attributes of the same key. Later additions to the child do not affect the parent.
func With(ctx context.Context, args ...any) context.Context {
	child := &scope{}
	if parent := scopeFrom(ctx); parent != nil {
		child.attrs = parent.snapshot()
	}
	child.set(toAttrs(args))

	return context.WithValue(ctx, scopeKey{}, child)
}

// Add sets args on the scope of ctx, so they appear in every log that uses a context sharing that
// scope, including the ancestors that created it. It does nothing when ctx has no scope.
func Add(ctx context.Context, args ...any) {
	if s := scopeFrom(ctx); s != nil {
		s.set(toAttrs(args))
	}
}

// NewTrace returns a child of ctx with a fresh trace ID.
func NewTrace(ctx context.Context) context.Context {
	return With(ctx, TraceKey, id.New())
}

// TraceID returns the trace ID of ctx, or "" when it has none.
func TraceID(ctx context.Context) string {
	s := scopeFrom(ctx)
	if s == nil {
		return ""
	}
	for _, attr := range s.snapshot() {
		if attr.Key == TraceKey && attr.Value.Kind() == slog.KindString {
			return attr.Value.String()
		}
	}

	return ""
}

func toAttrs(args []any) []slog.Attr {
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "", 0)
	record.Add(args...)
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)

		return true
	})

	return attrs
}

// New returns a JSON logger writing to w at level that includes scope attributes from each context.
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(&handler{inner: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})})
}

// handler adds the scope attributes of the record's context before delegating.
type handler struct {
	inner slog.Handler
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	if s := scopeFrom(ctx); s != nil {
		record = record.Clone()
		record.AddAttrs(s.snapshot()...)
	}

	return h.inner.Handle(ctx, record)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name)}
}
