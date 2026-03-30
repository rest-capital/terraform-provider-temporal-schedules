package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"go.temporal.io/api/serviceerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isRetryableAuthError returns true if the error is an authentication/authorization
// error that may resolve on its own (e.g., API key eventual consistency).
// The Temporal SDK wraps gRPC errors into its own serviceerror types (e.g.,
// *serviceerror.PermissionDenied), which status.FromError cannot unwrap.
// We check for the SDK's error type first, then fall back to raw gRPC status
// for errors that the SDK doesn't wrap (e.g., Unauthenticated).
func isRetryableAuthError(err error) bool {
	if err == nil {
		return false
	}
	var permDenied *serviceerror.PermissionDenied
	if errors.As(err, &permDenied) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	return st.Code() == codes.Unauthenticated || st.Code() == codes.PermissionDenied
}

// isRetryableConnectionError returns true if the error is a transient gRPC
// connection error that may resolve with a fresh client. When Terraform applies
// multiple resources in parallel, a concurrent auth-triggered eviction can close
// a shared cached client mid-RPC, producing grpc.ErrClientConnClosing
// (codes.Canceled, "the client connection is closing"). We also handle
// codes.Unavailable for transport-level failures (e.g., server-side connection
// close), though the Temporal SDK's own retry interceptor typically handles
// Unavailable before it reaches us.
//
// To distinguish a closed connection (codes.Canceled) from an intentional
// context cancellation (also codes.Canceled), the caller's context is required.
// If ctx is already done, we treat the error as non-retryable.
func isRetryableConnectionError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable:
		return true
	case codes.Canceled:
		// Only retry if the context is still alive — a cancelled context means
		// the caller (e.g., Terraform) intentionally aborted the operation.
		return ctx.Err() == nil
	default:
		return false
	}
}

// isRetryableError returns true if the error is any retryable error — either
// a transient auth error or a transient connection error.
func isRetryableError(ctx context.Context, err error) bool {
	return isRetryableAuthError(err) || isRetryableConnectionError(ctx, err)
}

// retryableOrNot wraps an error as RetryableError if it's a transient error
// (auth or connection), or NonRetryableError otherwise.
func retryableOrNot(ctx context.Context, err error) *retry.RetryError {
	if isRetryableError(ctx, err) {
		return retry.RetryableError(err)
	}
	return retry.NonRetryableError(err)
}
