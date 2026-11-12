package accountsecurity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryCounter struct {
	mu     sync.Mutex
	values map[string]int64
}

func (s *memoryCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]int64{}
	}
	s.values[key]++
	return s.values[key], nil
}

func TestMailLimitDimensionsAndHourRecovery(t *testing.T) {
	for _, scope := range []string{"email", "ip", "network", "domain", "site"} {
		t.Run(scope, func(t *testing.T) {
			limits := DefaultPolicy().MailLimits
			switch scope {
			case "email":
				limits.Email = 1
			case "ip":
				limits.IP = 1
			case "network":
				limits.Network = 1
			case "domain":
				limits.Domain = 1
			case "site":
				limits.Site = 1
			}
			store := &memoryCounter{}
			now := time.Date(2026, 11, 9, 12, 59, 0, 0, time.UTC)
			if err := ReserveMail(t.Context(), store, limits, "private@example.test", "203.0.113.1", now); err != nil {
				t.Fatal(err)
			}
			email, ip := "other@example.test", "203.0.113.2"
			switch scope {
			case "email":
				email = "PRIVATE@example.test"
			case "ip":
				ip = "::ffff:203.0.113.1"
			case "domain":
				ip = "198.51.100.1"
			case "site":
				email = "other@another.test"
				ip = "198.51.100.1"
			}
			for i := 0; i < 2; i++ {
				err := ReserveMail(t.Context(), store, limits, email, ip, now)
				var limited *LimitError
				if !errors.As(err, &limited) || limited.Scope != scope || limited.First != (i == 0) || !limited.Until.Equal(now.Add(time.Minute)) {
					t.Fatal("wrong limit", err)
				}
			}
			if err := ReserveMail(t.Context(), store, limits, email, ip, now.Add(time.Minute)); err != nil {
				t.Fatal("new UTC hour did not recover", err)
			}
			for key := range store.values {
				if strings.Contains(key, "private") || strings.Contains(key, "203.0.113") || strings.Contains(key, "example.test") {
					t.Fatal("plaintext identity in storage", key)
				}
			}
		})
	}
}

func TestMailLimitIPv6NetworksAndConcurrentSiteBudget(t *testing.T) {
	limits := DefaultPolicy().MailLimits
	limits.Network = 1
	store := &memoryCounter{}
	now := time.Now()
	if err := ReserveMail(t.Context(), store, limits, "a@example.test", "2001:db8:1234:5678::1", now); err != nil {
		t.Fatal(err)
	}
	var limited *LimitError
	if err := ReserveMail(t.Context(), store, limits, "b@example.test", "2001:db8:1234:5678:ffff::2", now); !errors.As(err, &limited) || limited.Scope != "network" {
		t.Fatal(err)
	}
	if err := ReserveMail(t.Context(), store, limits, "c@example.test", "2001:db8:1234:5679::2", now); err != nil {
		t.Fatal(err)
	}
	limits = DefaultPolicy().MailLimits
	limits.Site = 3
	store = &memoryCounter{}
	var accepted atomic.Int32
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := ReserveMail(t.Context(), store, limits, fmt.Sprintf("u%d@domain%d.test", i, i), fmt.Sprintf("198.51.%d.1", i), now)
			if err == nil {
				accepted.Add(1)
			} else {
				var limited *LimitError
				if !errors.As(err, &limited) || limited.Scope != "site" {
					failed.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 3 || failed.Load() != 0 {
		t.Fatal("concurrent budget bypass", accepted.Load(), failed.Load())
	}
}
