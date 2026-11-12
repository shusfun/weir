package gate

import (
	"context"
	"sync"
)

// Configuration downloads go ahead of queued background downloads. The single
// slot covers updates, relays and kernel installation, not the control channel.
type downloadCoordinator struct {
	mu       sync.Mutex
	active   bool
	priority int
	wake     chan struct{}
}

func (d *downloadCoordinator) acquire(ctx context.Context, priority bool) (func(), error) {
	d.mu.Lock()
	if priority {
		d.priority++
	}
	for {
		if err := ctx.Err(); err != nil {
			if priority {
				d.priority--
			}
			d.signalLocked()
			d.mu.Unlock()
			return nil, err
		}
		if !d.active && (priority || d.priority == 0) {
			d.active = true
			if priority {
				d.priority--
			}
			d.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { d.mu.Lock(); d.active = false; d.signalLocked(); d.mu.Unlock() }) }, nil
		}
		if d.wake == nil {
			d.wake = make(chan struct{})
		}
		wake := d.wake
		d.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-wake:
		}
		d.mu.Lock()
	}
}

func (d *downloadCoordinator) signalLocked() {
	if d.wake != nil {
		close(d.wake)
		d.wake = nil
	}
}
