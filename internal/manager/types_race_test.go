package manager

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestGenerationCallTimeoutConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gen := newGeneration(1, "test", nil, nil, 1, time.Second, ctx, cancel, 1)
	lease, err := gen.acquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lease.Release()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			gen.SetCallTimeout(time.Duration(i+1) * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			_ = lease.CallTimeout()
		}
	}()
	wg.Wait()
}
