package provider

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isRetryableAuthError returns true if the error is an authentication/authorization
// error that may resolve on its own (e.g., API key eventual consistency).
func isRetryableAuthError(err error) bool {
	if err == nil {
		return false
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
