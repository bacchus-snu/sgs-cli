// Package cleanup provides a global cleanup registry for handling interrupts.
package cleanup

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// CleanupFunc is a function that cleans up a resource.
type CleanupFunc func(ctx context.Context)

var (
	mu          sync.Mutex
	cleanups    []CleanupFunc
	interrupted bool         // Set when interrupt is being handled
	done        chan struct{} // Closed when cleanup is complete
)

// Register adds a cleanup function to be called on interrupt.
// Cleanup functions are called in reverse order (LIFO).
func Register(fn CleanupFunc) {
	mu.Lock()
	defer mu.Unlock()
	cleanups = append(cleanups, fn)
}

// Unregister removes the most recently added cleanup function.
// Call this after a resource is successfully cleaned up normally.
func Unregister() {
	mu.Lock()
	defer mu.Unlock()
	if len(cleanups) > 0 {
		cleanups = cleanups[:len(cleanups)-1]
	}
}

// RunAll runs all registered cleanup functions in reverse order and clears the list.
func RunAll() {
	mu.Lock()
	fns := make([]CleanupFunc, len(cleanups))
	copy(fns, cleanups)
	cleanups = nil
	mu.Unlock()

	ctx := context.Background()
	// Run in reverse order (LIFO)
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i](ctx)
	}
}

// WasInterrupted returns true if an interrupt signal was received.
// Use this to check if error handling should be skipped.
func WasInterrupted() bool {
	mu.Lock()
	defer mu.Unlock()
	return interrupted
}

// WaitForCleanup blocks until interrupt cleanup is complete (or returns immediately if not interrupted).
func WaitForCleanup() {
	mu.Lock()
	if !interrupted {
		mu.Unlock()
		return
	}
	ch := done
	mu.Unlock()
	if ch != nil {
		<-ch
	}
}

// InterruptibleContext returns a context that is cancelled when SIGINT/SIGTERM is received.
// IMPORTANT: This captures the signal and PREVENTS the default "kill process" behavior.
// When interrupted, cleanup functions run, then the process exits with code 1.
func InterruptibleContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	sigCh := make(chan os.Signal, 1)
	// Notify captures the signal and PREVENTS default handling (process termination)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			// Mark as interrupted and create done channel
			mu.Lock()
			interrupted = true
			done = make(chan struct{})
			mu.Unlock()

			// Ignore further signals during cleanup
			signal.Ignore(os.Interrupt, syscall.SIGTERM)
			fmt.Fprintln(os.Stderr, "\nInterrupted, cleaning up...")
			cancel() // Cancel context so operations abort
			// Run all registered cleanup functions with a fresh context
			RunAll()
			close(done)
			os.Exit(1)
		case <-ctx.Done():
			// Context cancelled normally, stop listening
			signal.Stop(sigCh)
		}
	}()

	return ctx, func() {
		signal.Stop(sigCh)
		cancel()
	}
}
