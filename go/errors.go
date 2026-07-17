// SPDX-License-Identifier: MIT

package athenadriver

import "errors"

// Various errors the driver might return. Can change between driver versions.
var (
	ErrInvalidQuery                 = errors.New("query is not valid")
	ErrConfigInvalidConfig          = errors.New("driver config is invalid")
	ErrConfigOutputLocation         = errors.New("output location must starts with s3")
	ErrConfigRegion                 = errors.New("region is required")
	ErrConfigWGPointer              = errors.New("workgroup pointer is nil")
	ErrConfigAccessIDRequired       = errors.New("AWS access ID is required")
	ErrConfigAccessKeyRequired      = errors.New("AWS access Key is required")
	ErrQueryUnknownType             = errors.New("query parameter type is unknown")
	ErrQueryBufferOF                = errors.New("query buffer overflow")
	ErrQueryTimeout                 = errors.New("query timeout")
	ErrAthenaTransactionUnsupported = errors.New("Athena doesn't support transaction statements")
	ErrAthenaNilClient              = errors.New("athenaClient must not be nil")
	ErrTestMockGeneric              = errors.New("some_mock_error_for_test")
	ErrTestMockFailedByAthena       = errors.New("the reason why Athena failed the query")
)

// QueryFailureError is returned when Athena reports a query in the FAILED
// state. It carries the structured failure info from the API's
// QueryExecutionStatus.AthenaError so callers can distinguish retryable
// system errors from user SQL errors via errors.As:
//
//	var qErr *athenadriver.QueryFailureError
//	if errors.As(err, &qErr) && qErr.Retryable { ... }
type QueryFailureError struct {
	// Message is Athena's StateChangeReason (or the AthenaError message
	// when no reason is reported).
	Message string
	// ErrorCategory classifies the failure: 1 = system, 2 = user,
	// 3 = other. Zero when Athena reported no structured error.
	ErrorCategory int32
	// ErrorType is Athena's fine-grained error code; see the Athena
	// error-type reference. Zero when unreported.
	ErrorType int32
	// Retryable is true when Athena believes the query might succeed if
	// resubmitted.
	Retryable bool
}

func (e *QueryFailureError) Error() string {
	return e.Message
}
