// SPDX-License-Identifier: MIT

package athenadriver

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/stretchr/testify/assert"
)

// newRowsWithMetadata builds a *Rows with hand-crafted ColumnInfo and an
// optional QueryExecution — enough to exercise the metadata accessors
// without going through mock paginators.
func newRowsWithMetadata(qid string, qx *athenatypes.QueryExecution, cols []athenatypes.ColumnInfo) *Rows {
	return &Rows{
		queryID:        qid,
		queryExecution: qx,
		ResultOutput: &athena.GetQueryResultsOutput{
			ResultSet: &athenatypes.ResultSet{
				ResultSetMetadata: &athenatypes.ResultSetMetadata{
					ColumnInfo: cols,
				},
			},
		},
	}
}

func TestRows_QueryID(t *testing.T) {
	r := newRowsWithMetadata("qid-1234", nil, nil)
	assert.Equal(t, "qid-1234", r.QueryID())
}

func TestRows_StatementType(t *testing.T) {
	// No QueryExecution -> "".
	r := newRowsWithMetadata("q", nil, nil)
	assert.Equal(t, "", r.StatementType())

	// With QueryExecution.
	r = newRowsWithMetadata("q", &athenatypes.QueryExecution{
		StatementType: athenatypes.StatementTypeDml,
	}, nil)
	assert.Equal(t, "DML", r.StatementType())
}

func TestRows_SubstatementType(t *testing.T) {
	// No QueryExecution.
	r := newRowsWithMetadata("q", nil, nil)
	assert.Equal(t, "", r.SubstatementType())

	// QueryExecution present but SubstatementType nil.
	r = newRowsWithMetadata("q", &athenatypes.QueryExecution{}, nil)
	assert.Equal(t, "", r.SubstatementType())

	// Populated.
	sub := "SELECT"
	r = newRowsWithMetadata("q", &athenatypes.QueryExecution{
		SubstatementType: &sub,
	}, nil)
	assert.Equal(t, "SELECT", r.SubstatementType())
}

func colInfo(name, typ string) athenatypes.ColumnInfo {
	return athenatypes.ColumnInfo{Name: aws.String(name), Type: aws.String(typ)}
}

func TestRows_ColumnTypeScanType(t *testing.T) {
	// Known type -> concrete Go type.
	r := newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{colInfo("c", "integer")})
	assert.Equal(t, reflect.TypeFor[int32](), r.ColumnTypeScanType(0))

	// Unknown type -> scanTypeUnknown.
	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{colInfo("c", "no_such_type")})
	assert.Equal(t, scanTypeUnknown, r.ColumnTypeScanType(0))

	// Nil type pointer -> scanTypeUnknown.
	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{Name: aws.String("c")}})
	assert.Equal(t, scanTypeUnknown, r.ColumnTypeScanType(0))
}

func TestRows_ColumnTypeNullable(t *testing.T) {
	r := newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{
		Name:     aws.String("c"),
		Nullable: athenatypes.ColumnNullableNullable,
	}})
	nullable, ok := r.ColumnTypeNullable(0)
	assert.True(t, ok)
	assert.True(t, nullable)

	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{
		Name:     aws.String("c"),
		Nullable: athenatypes.ColumnNullableNotNull,
	}})
	nullable, ok = r.ColumnTypeNullable(0)
	assert.True(t, ok)
	assert.False(t, nullable)

	// UNKNOWN is what Athena currently reports -> ok=false.
	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{
		Name:     aws.String("c"),
		Nullable: athenatypes.ColumnNullableUnknown,
	}})
	_, ok = r.ColumnTypeNullable(0)
	assert.False(t, ok)
}

func TestRows_ColumnTypePrecisionScale(t *testing.T) {
	// Decimal -> precision/scale returned.
	r := newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{
		Name:      aws.String("c"),
		Type:      aws.String("decimal"),
		Precision: 20,
		Scale:     4,
	}})
	p, s, ok := r.ColumnTypePrecisionScale(0)
	assert.True(t, ok)
	assert.Equal(t, int64(20), p)
	assert.Equal(t, int64(4), s)

	// Non-decimal -> ok=false.
	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{colInfo("c", "integer")})
	_, _, ok = r.ColumnTypePrecisionScale(0)
	assert.False(t, ok)

	// Nil type -> ok=false.
	r = newRowsWithMetadata("q", nil, []athenatypes.ColumnInfo{{Name: aws.String("c")}})
	_, _, ok = r.ColumnTypePrecisionScale(0)
	assert.False(t, ok)
}
