package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A client that has sent headers but not its body must not hold the
// configuration transaction lock (also shared with the runtime file poller).
func TestSlowPutBodyDoesNotBlockConfigTransaction(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "standalone"
		if shared {
			name = "shared-runtime-transaction"
		}
		t.Run(name, func(t *testing.T) {
			h, _ := newAdminTest(t)
			if shared {
				var transactionMu sync.Mutex
				h.opts.ConfigTransaction = func(fn func(func(context.Context) error)) {
					transactionMu.Lock()
					defer transactionMu.Unlock()
					fn(nil)
				}
			}
			reader, writer := io.Pipe()
			reading := make(chan struct{})
			body := &signaledBodyReader{ReadCloser: reader, reading: reading}
			req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/servers/remote", body)
			response := httptest.NewRecorder()
			putDone := make(chan struct{})
			go func() {
				defer close(putDone)
				h.putServer(response, req, "remote")
			}()
			defer func() {
				_ = writer.Close()
				_ = reader.Close()
				<-putDone
			}()
			select {
			case <-reading:
			case <-time.After(2 * time.Second):
				t.Fatal("PUT did not begin reading its body")
			}
			transactionDone := make(chan struct{})
			go func() {
				h.withConfigTransaction(func(func(context.Context) error) {})
				close(transactionDone)
			}()
			select {
			case <-transactionDone:
			case <-time.After(time.Second):
				// Release and join both goroutines even on the buggy implementation.
				_ = writer.Close()
				<-transactionDone
				t.Fatal("slow PUT body blocked an unrelated configuration transaction")
			}
		})
	}
}

type signaledBodyReader struct {
	io.ReadCloser
	reading chan struct{}
	once    sync.Once
}

func (r *signaledBodyReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.reading) })
	return r.ReadCloser.Read(p)
}
