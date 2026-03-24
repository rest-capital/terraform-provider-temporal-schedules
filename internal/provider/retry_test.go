package provider

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsRetryableAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "unauthenticated",
			err:  status.Error(codes.Unauthenticated, "invalid API key"),
			want: true,
		},
		{
			name: "permission denied",
			err:  status.Error(codes.PermissionDenied, "access denied"),
			want: true,
		},
		{
			name: "not found",
			err:  status.Error(codes.NotFound, "schedule not found"),
			want: false,
		},
		{
			name: "internal",
			err:  status.Error(codes.Internal, "server error"),
			want: false,
		},
		{
			name: "unavailable",
			err:  status.Error(codes.Unavailable, "service unavailable"),
			want: false,
		},
		{
			name: "non-gRPC error",
			err:  fmt.Errorf("some random error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableAuthError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableAuthError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryableOrNot(t *testing.T) {
	t.Run("auth error is retryable", func(t *testing.T) {
		err := status.Error(codes.Unauthenticated, "not yet propagated")
		result := retryableOrNot(err)
		if result == nil {
			t.Fatal("expected non-nil RetryError")
		}
		// RetryableError wraps with Retryable=true
		if result.Err == nil {
			t.Fatal("expected wrapped error")
		}
	})

	t.Run("non-auth error is non-retryable", func(t *testing.T) {
		err := status.Error(codes.NotFound, "not found")
		result := retryableOrNot(err)
		if result == nil {
			t.Fatal("expected non-nil RetryError")
		}
	})
}

func TestRetryContext_AuthErrorResolves(t *testing.T) {
	var callCount atomic.Int32

	err := retry.RetryContext(context.Background(), 30*time.Second, func() *retry.RetryError {
		n := callCount.Add(1)
		if n <= 3 {
			return retryableOrNot(status.Error(codes.Unauthenticated, "not yet propagated"))
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := callCount.Load(); got < 4 {
		t.Fatalf("expected at least 4 calls, got %d", got)
	}
}

func TestRetryContext_NonAuthErrorFailsImmediately(t *testing.T) {
	var callCount atomic.Int32

	err := retry.RetryContext(context.Background(), 30*time.Second, func() *retry.RetryError {
		callCount.Add(1)
		return retryableOrNot(status.Error(codes.NotFound, "schedule not found"))
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call for non-retryable error, got %d", got)
	}
}

func TestRetryContext_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := retry.RetryContext(ctx, 2*time.Minute, func() *retry.RetryError {
		return retryableOrNot(status.Error(codes.Unauthenticated, "not yet propagated"))
	})

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
