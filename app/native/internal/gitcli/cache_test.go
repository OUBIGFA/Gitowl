package gitcli

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestSharedReadRetriesAfterLeaderDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New("unused", 1024)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		entered := make(chan struct{})
		leader := make(chan error, 1)
		go func() {
			_, err := e.load(ctx, "shared", func() ([]byte, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			})
			leader <- err
		}()
		<-entered
		follower := make(chan error, 1)
		go func() {
			data, err := e.load(context.Background(), "shared", func() ([]byte, error) {
				return []byte("available"), nil
			})
			if err == nil && string(data) != "available" {
				err = errors.New("shared read lost the surviving caller's result")
			}
			follower <- err
		}()
		// Wait until both readers are blocked, then advance virtual time.
		synctest.Wait()
		time.Sleep(time.Second)
		if err := <-leader; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("leader: %v", err)
		}
		if err := <-follower; err != nil {
			t.Fatalf("healthy follower inherited another request's deadline: %v", err)
		}
	})
}

func TestCancelledReadDoesNotReturnCachedSuccess(t *testing.T) {
	e := New("unused", 1024)
	e.cache.put("cached", []byte("content"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.load(ctx, "cached", func() ([]byte, error) {
		t.Fatal("cancelled read started work")
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request returned success: %v", err)
	}
}

func BenchmarkImmutableCacheHit(b *testing.B) {
	e := New("unused", DefaultCacheBytes)
	e.cache.put("object", make([]byte, 64<<10))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, err := e.load(ctx, "object", func() ([]byte, error) {
			b.Fatal("cached object was reloaded")
			return nil, nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
