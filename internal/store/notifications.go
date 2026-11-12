package store

import (
	"context"
	"errors"
	"sync"

	"github.com/redis/go-redis/v9"
)

// Notifications are hints only. Consumers subscribe before reading durable
// state and recheck after a timeout, so reconnects never lose committed work.
type Notifications struct {
	mu        sync.Mutex
	listeners map[string]map[chan struct{}]struct{}
	client    *redis.Client
	pubsub    *redis.PubSub
	cancel    context.CancelFunc
}

func newNotifications(ctx context.Context, c *redis.Client) (*Notifications, error) {
	n := &Notifications{listeners: map[string]map[chan struct{}]struct{}{}, client: c}
	if c == nil {
		return n, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	n.cancel = cancel
	n.pubsub = c.Subscribe(ctx, "weir:worker-wake")
	if _, err := n.pubsub.Receive(ctx); err != nil {
		cancel()
		n.pubsub.Close()
		return nil, err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-n.pubsub.Channel():
				if !ok {
					return
				}
				n.local(m.Payload)
			}
		}
	}()
	return n, nil
}

func (n *Notifications) local(topic string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.listeners[topic] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (n *Notifications) Publish(ctx context.Context, topic string) error {
	if len(topic) == 0 || len(topic) > 128 {
		return errors.New("invalid notification topic")
	}
	n.local(topic)
	if n.client != nil {
		return n.client.Publish(ctx, "weir:worker-wake", topic).Err()
	}
	return nil
}

func (n *Notifications) Subscribe(topic string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	if n.listeners[topic] == nil {
		n.listeners[topic] = map[chan struct{}]struct{}{}
	}
	n.listeners[topic][ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.listeners[topic], ch)
		if len(n.listeners[topic]) == 0 {
			delete(n.listeners, topic)
		}
	}
}

func (n *Notifications) Close() {
	if n.cancel != nil {
		n.cancel()
	}
	if n.pubsub != nil {
		n.pubsub.Close()
	}
}
