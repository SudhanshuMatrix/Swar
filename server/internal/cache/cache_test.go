package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpiry(t *testing.T) {
	c := New(10)
	now := time.Unix(1000, 0)
	c.SetClock(func() time.Time { return now })
	c.Set("k", 1, time.Minute)
	if v, ok := c.Get("k"); !ok || v.(int) != 1 {
		t.Fatal("want hit")
	}
	now = now.Add(61 * time.Second)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expired entry must not be served")
	}
	if c.Len() != 0 {
		t.Fatal("expired entry should be removed on access")
	}
}

func TestLRUBound(t *testing.T) {
	c := New(3)
	for i := 0; i < 3; i++ {
		c.Set(fmt.Sprint(i), i, time.Hour)
	}
	c.Get("0") // 0 becomes most recent; 1 is now LRU
	c.Set("3", 3, time.Hour)
	if _, ok := c.Get("1"); ok {
		t.Fatal("LRU entry should have been evicted")
	}
	if _, ok := c.Get("0"); !ok {
		t.Fatal("recently used entry evicted")
	}
	if c.Len() != 3 || c.Stats().Evictions != 1 {
		t.Fatalf("len=%d stats=%+v", c.Len(), c.Stats())
	}
}

func TestNonPositiveTTLNotStored(t *testing.T) {
	c := New(3)
	c.Set("k", 1, 0)
	if _, ok := c.Get("k"); ok {
		t.Fatal("ttl 0 must not be cached")
	}
}

func TestConcurrentMissesCoalesce(t *testing.T) {
	c := New(100)
	var calls atomic.Int32
	release := make(chan struct{})
	load := func(ctx context.Context) (string, time.Duration, error) {
		calls.Add(1)
		<-release
		return "v", time.Minute, nil
	}
	var wg sync.WaitGroup
	results := make(chan string, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := GetOrLoad(context.Background(), c, "key", time.Second, load)
			if err != nil {
				t.Error(err)
			}
			results <- v
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	for v := range results {
		if v != "v" {
			t.Fatalf("got %q", v)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("loader ran %d times, want 1", n)
	}
	if v, err := GetOrLoad(context.Background(), c, "key", time.Second, load); err != nil || v != "v" || calls.Load() != 1 {
		t.Fatal("second call must be a cache hit")
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	c := New(10)
	var calls int
	boom := errors.New("boom")
	load := func(ctx context.Context) (int, time.Duration, error) {
		calls++
		if calls == 1 {
			return 0, time.Minute, boom
		}
		return 7, time.Minute, nil
	}
	if _, err := GetOrLoad(context.Background(), c, "k", time.Second, load); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if v, err := GetOrLoad(context.Background(), c, "k", time.Second, load); err != nil || v != 7 {
		t.Fatalf("want retry to succeed: %v %v", v, err)
	}
}

func TestCallerCancelDoesNotFailOthers(t *testing.T) {
	c := New(10)
	release := make(chan struct{})
	load := func(ctx context.Context) (int, time.Duration, error) {
		select {
		case <-release:
			return 42, time.Minute, nil
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		}
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() {
		_, err := GetOrLoad(ctx1, c, "k", time.Second, load)
		done1 <- err
	}()
	time.Sleep(20 * time.Millisecond)
	done2 := make(chan int, 1)
	go func() {
		v, _ := GetOrLoad(context.Background(), c, "k", time.Second, load)
		done2 <- v
	}()
	time.Sleep(20 * time.Millisecond)
	cancel1()
	if err := <-done1; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller should see its own cancellation, got %v", err)
	}
	close(release)
	if v := <-done2; v != 42 {
		t.Fatalf("other waiter should still get the value, got %d", v)
	}
}

func TestDeletePrefix(t *testing.T) {
	c := New(10)
	c.Set("a:1", 1, time.Hour)
	c.Set("a:2", 1, time.Hour)
	c.Set("b:1", 1, time.Hour)
	if n := c.DeletePrefix("a:"); n != 2 || c.Len() != 1 {
		t.Fatalf("n=%d len=%d", n, c.Len())
	}
}

func TestClear(t *testing.T) {
	c := New(10)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	if n := c.Clear(); n != 2 || c.Len() != 0 {
		t.Fatalf("Clear() = %d, Len() = %d", n, c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("entry survived Clear")
	}
	c.Set("c", 3, time.Minute)
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatal("cache unusable after Clear")
	}
}
