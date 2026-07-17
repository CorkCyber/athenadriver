// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceLimitOverride(t *testing.T) {
	s := &ServiceLimitOverride{}
	assert.Zero(t, s.DDLQueryTimeout)
	assert.Zero(t, s.DMLQueryTimeout)

	s.DDLQueryTimeout = 30 * 60
	s.DMLQueryTimeout = 60 * 60
	assert.Equal(t, 30*60, s.DDLQueryTimeout)
	assert.Equal(t, 60*60, s.DMLQueryTimeout)
}
