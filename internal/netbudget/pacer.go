// Package netbudget bounds aggregate HTTP body transfers without throttling
// control requests or unrelated proxy traffic.
package netbudget

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

type Pacer struct {
	mu   sync.Mutex
	rate int64
	next time.Time
}

func (p *Pacer) SetRate(bytesPerSecond int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rate != bytesPerSecond {
		p.rate, p.next = bytesPerSecond, time.Time{}
	}
}

func (p *Pacer) wait(ctx context.Context, n int) error {
	p.mu.Lock()
	if p.rate <= 0 || n == 0 {
		p.mu.Unlock()
		return ctx.Err()
	}
	now := time.Now()
	if p.next.Before(now) {
		p.next = now
	}
	p.next = p.next.Add(time.Duration(int64(n) * int64(time.Second) / p.rate))
	delay := time.Until(p.next)
	p.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type body struct {
	io.ReadCloser
	ctx   context.Context
	pacer *Pacer
}

func (b *body) Read(buffer []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) > 16<<10 {
		buffer = buffer[:16<<10]
	}
	n, err := b.ReadCloser.Read(buffer)
	if waitErr := b.pacer.wait(b.ctx, n); waitErr != nil {
		return 0, waitErr
	}
	return n, err
}

type transport struct {
	next  http.RoundTripper
	pacer *Pacer
}

func (t transport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	if req.Body != nil {
		copy.Body = &body{req.Body, req.Context(), t.pacer}
		copy.GetBody = nil
	}
	resp, err := t.next.RoundTrip(copy)
	if err == nil && resp.Body != nil {
		resp.Body = &body{resp.Body, req.Context(), t.pacer}
	}
	return resp, err
}

func (p *Pacer) Client(client *http.Client) *http.Client {
	copy := *client
	next := copy.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	copy.Transport = transport{next, p}
	return &copy
}

type writer struct {
	ctx   context.Context
	next  io.Writer
	pacer *Pacer
}

func (w writer) Write(body []byte) (int, error) {
	total := 0
	for len(body) > 0 {
		n := min(len(body), 16<<10)
		if err := w.pacer.wait(w.ctx, n); err != nil {
			return total, err
		}
		written, err := w.next.Write(body[:n])
		total += written
		if err != nil {
			return total, err
		}
		if written != n {
			return total, io.ErrShortWrite
		}
		body = body[n:]
	}
	return total, nil
}

func (p *Pacer) Writer(ctx context.Context, next io.Writer) io.Writer {
	return writer{ctx: ctx, next: next, pacer: p}
}
