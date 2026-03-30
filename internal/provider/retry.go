package provider

import (
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

// retryableOrNot wraps an error as RetryableError if it's a transient auth error,
// or NonRetryableError otherwise.
func retryableOrNot(err error) *retry.RetryError {
	if isRetryableAuthError(err) {
		return retry.RetryableError(err)
	}
	return retry.NonRetryableError(err)
}
