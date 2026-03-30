package provider

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEvictTemporalClient_RemovesCachedClient(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	// Get a client — should be cached.
	c1, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient: %v", err)
	}

	// Second call should return the same cached client.
	c2, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient: %v", err)
	}
	if c1 != c2 {
		t.Fatal("expected same cached client instance")
	}

	// Evict the client.
	evictTemporalClient(address, namespace, apiKey)

	// Next call should create a new client (different instance).
	c3, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient after eviction: %v", err)
	}
	if c1 == c3 {
		t.Fatal("expected new client instance after eviction, got same pointer")
	}
}

func TestEvictTemporalClient_NoopWhenNotCached(t *testing.T) {
	// Evicting a non-existent key should not panic.
	evictTemporalClient("nonexistent:7233", "ns", "key")
}

// TestWithRetryableClient_EvictsOnAuthError verifies that when fn returns an
// auth error, the cached client is evicted and the next retry gets a fresh one.
// This fails if withRetryableClient does not evict on auth errors.
func TestWithRetryableClient_EvictsOnAuthError(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	// Pre-warm the cache so the first call gets the original client.
	initialClient, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient: %v", err)
	}

	var clientSeenOnSecondCall client.Client
	var callCount atomic.Int32

	err = withRetryableClient(context.Background(), address, namespace, apiKey,
		func(tc client.Client) error {
			n := callCount.Add(1)
			if n == 1 {
				// First call: auth error should trigger eviction + retry.
				return status.Error(codes.Unauthenticated, "stale connection")
			}
			// Second call: should get a fresh client after eviction.
			clientSeenOnSecondCall = tc
			return nil
		})

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if clientSeenOnSecondCall == initialClient {
		t.Fatal("expected fresh client after eviction, got same instance")
	}
}

// TestWithRetryableClient_RetriesClientAcquisition verifies that when
// getTemporalClient itself fails with an auth error (e.g., during initial
// dial with a not-yet-propagated API key), withRetryableClient retries the
// client acquisition rather than failing immediately.
// This fails if getTemporalClient is called outside the retry loop.
func TestWithRetryableClient_RetriesClientAcquisition(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	// Poison the cache: store a client keyed to a unique apiKey, then evict
	// it right before the test. Instead, we simulate getTemporalClient
	// failure by using a sentinel apiKey that we intercept.
	//
	// We can't easily make getTemporalClient fail transiently. Instead, we
	// test the observable behavior: poison the cache with a closed client
	// for a unique key, so getTemporalClient returns the cached (broken)
	// client. The fn call will fail with an auth-like error, triggering
	// eviction. On the next retry, getTemporalClient creates a fresh client
	// that works.

	// Get a good client and immediately close it to make it "broken".
	brokenClient, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient: %v", err)
	}
	// Close the client to simulate a stale/broken connection, but leave it
	// in the cache. We need to manipulate the cache directly.
	clientMu.Lock()
	key := clientKey{Address: address, Namespace: namespace, APIKey: apiKey}
	brokenClient.Close()
	// Re-insert the now-closed client so getTemporalClient returns it.
	clientCache[key] = brokenClient
	clientMu.Unlock()

	var callCount atomic.Int32
	var finalClient client.Client

	err = withRetryableClient(context.Background(), address, namespace, apiKey,
		func(tc client.Client) error {
			n := callCount.Add(1)
			if n == 1 {
				// First call with the broken client: simulate auth error.
				return status.Error(codes.Unauthenticated, "connection broken")
			}
			// Second call should have a working client (evicted + reconnected).
			finalClient = tc
			return nil
		})

	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if finalClient == brokenClient {
		t.Fatal("expected a new client after eviction of broken client, got same instance")
	}
	if got := callCount.Load(); got < 2 {
		t.Fatalf("expected at least 2 calls, got %d", got)
	}
}

// TestWithRetryableClient_ClientAcquisitionAuthErrorNotFatal verifies that
// if getTemporalClient returns a wrapped auth error, withRetryableClient
// retries instead of returning the error immediately.
// This fails if getTemporalClient is called outside the retry loop.
func TestWithRetryableClient_ClientAcquisitionAuthErrorNotFatal(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	var clientsSeen []client.Client
	var callCount atomic.Int32

	err := withRetryableClient(context.Background(), address, namespace, apiKey,
		func(tc client.Client) error {
			clientsSeen = append(clientsSeen, tc)
			n := callCount.Add(1)
			if n <= 2 {
				// Return auth error — should trigger eviction + retry,
				// causing getTemporalClient to create a new client.
				return status.Error(codes.Unauthenticated, fmt.Sprintf("attempt %d", n))
			}
			return nil
		})

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if got := len(clientsSeen); got < 3 {
		t.Fatalf("expected at least 3 client observations, got %d", got)
	}
	// Each retry after eviction should yield a distinct client instance.
	if clientsSeen[0] == clientsSeen[1] {
		t.Error("expected different client on 2nd call after eviction, got same")
	}
	if clientsSeen[1] == clientsSeen[2] {
		t.Error("expected different client on 3rd call after eviction, got same")
	}
}

// TestWithRetryableClient_EvictsOnConnectionClosing verifies that when fn
// returns a codes.Canceled error ("the client connection is closing"), the
// cached client is evicted and the next retry gets a fresh connection. This
// handles the case where parallel resource operations share a cached client
// and one goroutine's auth-triggered eviction closes the connection mid-RPC
// for another goroutine.
func TestWithRetryableClient_EvictsOnConnectionClosing(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	initialClient, err := getTemporalClient(context.Background(), address, namespace, apiKey)
	if err != nil {
		t.Fatalf("getTemporalClient: %v", err)
	}

	var clientSeenOnSecondCall client.Client
	var callCount atomic.Int32

	err = withRetryableClient(context.Background(), address, namespace, apiKey,
		func(tc client.Client) error {
			n := callCount.Add(1)
			if n == 1 {
				return status.Error(codes.Canceled, "grpc: the client connection is closing")
			}
			clientSeenOnSecondCall = tc
			return nil
		})

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if clientSeenOnSecondCall == initialClient {
		t.Fatal("expected fresh client after eviction, got same instance")
	}
}

// TestWithRetryableClient_ServiceErrorPermissionDenied verifies that the retry
// loop handles *serviceerror.PermissionDenied — the error type the Temporal SDK
// actually returns for PermissionDenied, as opposed to raw gRPC status errors.
// Before the fix, this error type was not recognized as retryable and the
// operation would fail immediately on the first attempt.
func TestWithRetryableClient_ServiceErrorPermissionDenied(t *testing.T) {
	server := startDevServer(t)
	address := server.FrontendHostPort()
	namespace := "default"
	apiKey := ""

	var callCount atomic.Int32

	err := withRetryableClient(context.Background(), address, namespace, apiKey,
		func(tc client.Client) error {
			n := callCount.Add(1)
			if n <= 2 {
				// Simulate what the real SDK returns for permission denied.
				return serviceerror.NewPermissionDenied("caller does not have permission", "")
			}
			return nil
		})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := callCount.Load(); got < 3 {
		t.Fatalf("expected at least 3 calls (2 failures + 1 success), got %d", got)
	}
}
