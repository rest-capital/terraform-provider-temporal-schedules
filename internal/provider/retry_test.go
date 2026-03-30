package provider

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"go.temporal.io/api/serviceerror"
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
			name: "permission denied raw gRPC",
			err:  status.Error(codes.PermissionDenied, "access denied"),
			want: true,
		},
		{
			name: "permission denied serviceerror",
			err:  serviceerror.NewPermissionDenied("access denied", ""),
			want: true,
		},
		{
			name: "wrapped permission denied serviceerror",
			err:  fmt.Errorf("operation failed: %w", serviceerror.NewPermissionDenied("access denied", "")),
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

func TestIsRetryableConnectionError(t *testing.T) {
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
			name: "unavailable - transport closing",
			err:  status.Error(codes.Unavailable, "transport is closing"),
			want: true,
		},
		{
			name: "unavailable - connection refused",
			err:  status.Error(codes.Unavailable, "connection refused"),
			want: true,
		},
		{
			name: "canceled is not connection error",
			err:  status.Error(codes.Canceled, "context canceled"),
			want: false,
		},
		{
			name: "not found is not connection error",
			err:  status.Error(codes.NotFound, "schedule not found"),
			want: false,
		},
		{
			name: "unauthenticated is not connection error",
			err:  status.Error(codes.Unauthenticated, "invalid API key"),
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
			got := isRetryableConnectionError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableConnectionError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
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
			name: "unavailable",
			err:  status.Error(codes.Unavailable, "transport is closing"),
			want: true,
		},
		{
			name: "permission denied serviceerror",
			err:  serviceerror.NewPermissionDenied("access denied", ""),
			want: true,
		},
		{
			name: "canceled is not retryable",
			err:  status.Error(codes.Canceled, "context canceled"),
			want: false,
		},
		{
			name: "not found",
			err:  status.Error(codes.NotFound, "not found"),
			want: false,
		},
		{
			name: "internal",
			err:  status.Error(codes.Internal, "server error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableError() = %v, want %v", got, tt.want)
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

func TestRetryContext_UnavailableErrorRetries(t *testing.T) {
	var callCount atomic.Int32

	err := retry.RetryContext(context.Background(), 30*time.Second, func() *retry.RetryError {
		n := callCount.Add(1)
		if n <= 2 {
			return retryableOrNot(status.Error(codes.Unavailable, "transport is closing"))
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := callCount.Load(); got < 3 {
		t.Fatalf("expected at least 3 calls, got %d", got)
	}
}
