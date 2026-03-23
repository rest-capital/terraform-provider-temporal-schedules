package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

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
