package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupSharesWork(t *testing.T) {
	var g Group
	var runs atomic.Int32
	release := make(chan struct{})
	fn := func(ctx context.Context) (int, error) {
		runs.Add(1)
		<-release
		return 7, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := Do(context.Background(), &g, "k", time.Second, fn); err != nil || v != 7 {
				t.Errorf("got %v %v", v, err)
			}
		}()
	}
	for g.InFlight() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if runs.Load() != 1 || g.Coalesced() != 9 {
		t.Fatalf("runs=%d coalesced=%d", runs.Load(), g.Coalesced())
	}
}

func TestGroupCancelsAbandonedWork(t *testing.T) {
	var g Group
	stopped := make(chan struct{})
	fn := func(ctx context.Context) (int, error) {
		<-ctx.Done()
		close(stopped)
		return 0, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Do(ctx, &g, "k", time.Minute, fn); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller should get its own ctx error, got %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("work kept running after every caller left")
	}
	if g.Canceled() != 1 || g.InFlight() != 0 {
		t.Fatalf("canceled=%d inflight=%d", g.Canceled(), g.InFlight())
	}
}

func TestGroupKeepsWorkWhileSomeoneWaits(t *testing.T) {
	var g Group
	release := make(chan struct{})
	fn := func(ctx context.Context) (string, error) {
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	impatient, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() {
		_, err := Do(impatient, &g, "k", time.Minute, fn)
		res <- err
	}()
	for g.InFlight() == 0 {
		time.Sleep(time.Millisecond)
	}
	patient := make(chan string, 1)
	go func() {
		v, _ := Do(context.Background(), &g, "k", time.Minute, fn)
		patient <- v
	}()
	for g.Coalesced() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-res
	close(release)
	if v := <-patient; v != "done" {
		t.Fatalf("the remaining caller lost the result: %q", v)
	}
}

func TestGetOrLoadSharedCachesValuesNotErrors(t *testing.T) {
	c := New(10)
	var g Group
	var loads atomic.Int32
	fail := true
	load := func(ctx context.Context) (string, time.Duration, error) {
		loads.Add(1)
		if fail {
			return "", 0, errors.New("upstream down")
		}
		return "v", time.Minute, nil
	}
	if _, err := GetOrLoadShared(context.Background(), c, &g, "k", time.Second, load); err == nil {
		t.Fatal("want error")
	}
	fail = false
	for i := 0; i < 3; i++ {
		if v, err := GetOrLoadShared(context.Background(), c, &g, "k", time.Second, load); err != nil || v != "v" {
			t.Fatalf("got %q %v", v, err)
		}
	}
	if loads.Load() != 2 {
		t.Fatalf("an error must not be cached and a value must be: loads=%d", loads.Load())
	}
}
