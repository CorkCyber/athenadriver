// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAthenaResult_LastInsertId(t *testing.T) {
	a := AthenaResult{}
	r, e := a.LastInsertId()
	assert.Equal(t, int64(-1), r)
	assert.Nil(t, e)
}

func TestAthenaResult_RowsAffected(t *testing.T) {
	a := AthenaResult{}
	r, e := a.RowsAffected()
	assert.Equal(t, int64(0), r)
	assert.Nil(t, e)
}
