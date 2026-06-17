// SPDX-License-Identifier: MIT

package athenadriver

import (
	"errors"
	"fmt"
)

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
	ErrServiceLimitOverride         = fmt.Errorf("service limit override must be greater than %d", PoolInterval)
)
