package manager

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatorStopDrainsBeforeCancellingGeneration(t *testing.T) {
	coordCtx, coordCancel := context.WithCancel(context.Background())
	genCtx, genCancel := context.WithCancel(coordCtx)
	gen := newGeneration(1, "test", nil, nil, 1, time.Second, genCtx, genCancel, 1)
	lease, err := gen.acquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	coord := &Coordinator{
		currentGen:   gen,
		state:        StateReady,
		ctx:          coordCtx,
		cancel:       coordCancel,
		drainTimeout: time.Second,
		stoppedCh:    make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		coord.Stop()
		close(done)
	}()

	select {
	case <-genCtx.Done():
		t.Fatal("generation context was cancelled before the active lease drained")
	case <-time.After(50 * time.Millisecond):
	}

	lease.Release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Coordinator.Stop did not complete after lease release")
	}

	select {
	case <-genCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("generation context remained open after stop completed")
	}
}
