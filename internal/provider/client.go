package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"go.temporal.io/sdk/client"
)

type clientKey struct {
	Address   string
	Namespace string
	APIKey    string
}

var (
	clientCache = make(map[clientKey]client.Client)
	clientMu    sync.Mutex
)

func getTemporalClient(ctx context.Context, address, namespace, apiKey string) (client.Client, error) {
	key := clientKey{Address: address, Namespace: namespace, APIKey: apiKey}

	clientMu.Lock()
	defer clientMu.Unlock()

	if c, ok := clientCache[key]; ok {
		return c, nil
	}

	opts := client.Options{
		HostPort:  address,
		Namespace: namespace,
	}

	if apiKey != "" {
		opts.Credentials = client.NewAPIKeyStaticCredentials(apiKey)
		opts.ConnectionOptions = client.ConnectionOptions{
			TLS: &tls.Config{},
		}
	}

	c, err := client.DialContext(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connecting to Temporal at %s: %w", address, err)
	}

	clientCache[key] = c
	return c, nil
}

// evictTemporalClient removes a cached client so the next call to
// getTemporalClient creates a fresh connection. This is used when an
// auth error suggests the cached connection may be stale.
func evictTemporalClient(address, namespace, apiKey string) {
	key := clientKey{Address: address, Namespace: namespace, APIKey: apiKey}

	clientMu.Lock()
	defer clientMu.Unlock()

	if c, ok := clientCache[key]; ok {
		c.Close()
		delete(clientCache, key)
	}
}

// withRetryableClient acquires a Temporal client inside a retry loop and
// calls fn with it. If fn returns an auth error, the cached client is evicted
// so the next attempt gets a fresh connection. If getTemporalClient itself
// fails with an auth error (e.g., during initial dial), it is also retried.
func withRetryableClient(ctx context.Context, address, namespace, apiKey string, fn func(client.Client) error) error {
	return retry.RetryContext(ctx, 2*time.Minute, func() *retry.RetryError {
		tc, clientErr := getTemporalClient(ctx, address, namespace, apiKey)
		if clientErr != nil {
			if isRetryableAuthError(clientErr) {
				return retry.RetryableError(clientErr)
			}
			return retry.NonRetryableError(clientErr)
		}
		if err := fn(tc); err != nil {
			if isRetryableAuthError(err) {
				evictTemporalClient(address, namespace, apiKey)
			}
			return retryableOrNot(err)
		}
		return nil
	})
}
