// SPDX-License-Identifier: MIT

package athenadriver

// AthenaResult is the result of an Athena query execution.
type AthenaResult struct {
	rowAffected int64
}

// LastInsertId always returns -1: Athena has no notion of auto-generated
// row IDs.
func (a AthenaResult) LastInsertId() (int64, error) {
	return -1, nil
}

// RowsAffected returns the number of rows affected by the query.
func (a AthenaResult) RowsAffected() (int64, error) {
	return a.rowAffected, nil
}
