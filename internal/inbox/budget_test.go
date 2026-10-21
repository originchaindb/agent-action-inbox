package inbox

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRequestCeilingPersistsAndFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	for i := 0; i < 3; i++ {
		if err := reserveRequest(path, 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := reserveRequest(path, 3); err == nil {
		t.Fatal("request ceiling exceeded")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reserveRequest(path, 3); err == nil {
		t.Fatal("corrupt ledger reset the budget")
	}
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := reserveRequest(path, 3); err == nil {
		t.Fatal("stale lock must block spending")
	}
}

func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reserveRequest(path, 1) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("expected one reservation; got %d", successes.Load())
	}
}
