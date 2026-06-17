// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"fmt"
	"strings"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// genQueryResultsOutputByToken builds a GetQueryResultsOutput for a given
// paginator NextToken.
type genQueryResultsOutputByToken func(token string) (*athena.GetQueryResultsOutput, error)

// singlePage wraps a one-shot fixture: returns `page` for the initial
// paginator call (empty NextToken) and ErrTestMockGeneric for any
// follow-up call.
func singlePage(page *athena.GetQueryResultsOutput) genQueryResultsOutputByToken {
	return func(token string) (*athena.GetQueryResultsOutput, error) {
		if token != "" {
			return nil, ErrTestMockGeneric
		}
		return page, nil
	}
}

// buildPage assembles a one-shot GetQueryResultsOutput from columns and
// rows. updateCount >= 0 sets ResultOutput.UpdateCount; -1 leaves it nil.
func buildPage(cols []athenatypes.ColumnInfo, rows []athenatypes.Row, updateCount int64) *athena.GetQueryResultsOutput {
	out := &athena.GetQueryResultsOutput{
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{ColumnInfo: cols},
			Rows:              rows,
		},
	}
	if updateCount >= 0 {
		out.UpdateCount = &updateCount
	}
	return out
}

type mockAthenaClient struct {
	queryToResultsGenMap map[string]genQueryResultsOutputByToken

	CreateWGStatus bool
	GetWGStatus    bool
	WGDisabled     bool
}

func newMockAthenaClient() *mockAthenaClient {
	col := func(name, ty string) athenatypes.ColumnInfo { return newColumnInfo(name, ty) }
	colNil := func(name string) athenatypes.ColumnInfo { return newColumnInfo(name, nil) }
	one := func(c athenatypes.ColumnInfo) []athenatypes.ColumnInfo { return []athenatypes.ColumnInfo{c} }

	return &mockAthenaClient{
		queryToResultsGenMap: map[string]genQueryResultsOutputByToken{
			"SELECT_OK":                  MultiplePagesQueryResponse,
			"SELECT_GetQueryResults_ERR": MultiplePagesQueryFailedResponse,
			"SELECT_EMPTY_ROW_IN_PAGE":   MultiplePagesEmptyRowInPageResponse,
			"show": singlePage(newRandomHeaderResultPage(
				one(col("partition", "string")), nil, 6)),
			"RowsNextFailed": func(token string) (*athena.GetQueryResultsOutput, error) {
				if token != "" {
					return nil, ErrTestMockGeneric
				}
				next := "p1"
				return newRandomHeaderResultPage(createTestColumns(), &next, 5), nil
			},
			"1coloumn0row": singlePage(buildPage(one(colNil("a")), nil, -1)),
			"1coloumn0row_valid": singlePage(buildPage(
				one(colNil("rows")), nil, 1024)),
			"column_more_than_row_fields": singlePage(buildPage(
				[]athenatypes.ColumnInfo{colNil("c1"), colNil("c2")},
				[]athenatypes.Row{randRow(one(colNil("c1")))},
				1024)),
			"row_fields_more_than_column": singlePage(buildPage(
				one(colNil("c1")),
				[]athenatypes.Row{randRow([]athenatypes.ColumnInfo{colNil("c1"), colNil("c2")})},
				1024)),
			"missing_data_resp": singlePage(buildPage(
				one(col("c1", "integer")),
				[]athenatypes.Row{missingDataRow(one(col("c1", "integer")))},
				1024)),
			"missing_data_resp2": singlePage(buildPage(
				one(col("c2", "string")),
				[]athenatypes.Row{missingDataRow(one(col("c2", "string")))},
				1024)),
			"PING_OK_QID":                          pingPage(),
			"SELECTExecContext_OK_QID":             pingPage(),
			"SELECTQueryContext_OK_QID":            pingPage(),
			"00000000-0000-0000-0000-000000000000": pingPage(),
			"pc:get_query_id":                      pingPage(),
			"FAILED_AFTER_GETQID": singlePage(buildPage(
				one(col("c1", "integer")),
				[]athenatypes.Row{missingDataRow(one(col("c1", "integer")))},
				1024)),
		},
	}
}

func pingPage() genQueryResultsOutputByToken {
	c := newColumnInfo("_col0", "integer")
	return singlePage(buildPage(
		[]athenatypes.ColumnInfo{c},
		[]athenatypes.Row{randRow([]athenatypes.ColumnInfo{c})},
		1024))
}

// GetQueryResults dispatches on QueryExecutionId, with two QIDs reserved
// for forcing a transport-level error (one from the first call, one from
// a follow-up paginator call).
func (m *mockAthenaClient) GetQueryResults(_ context.Context, query *athena.GetQueryResultsInput, _ ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error) {
	if *query.QueryExecutionId == "GetQueryResultsWithContext_return_error" {
		return nil, ErrTestMockGeneric
	}
	token := ""
	if query.NextToken != nil {
		token = *query.NextToken
	}
	if token == "GetQueryResultsWithContext_return_error" {
		return nil, ErrTestMockGeneric
	}
	return m.queryToResultsGenMap[*query.QueryExecutionId](token)
}

func (m *mockAthenaClient) CreateWorkGroup(_ context.Context, _ *athena.CreateWorkGroupInput, _ ...func(*athena.Options)) (*athena.CreateWorkGroupOutput, error) {
	if !m.CreateWGStatus {
		return nil, ErrTestMockGeneric
	}
	return &athena.CreateWorkGroupOutput{}, nil
}

func (m *mockAthenaClient) GetWorkGroup(_ context.Context, _ *athena.GetWorkGroupInput, _ ...func(*athena.Options)) (*athena.GetWorkGroupOutput, error) {
	if !m.GetWGStatus {
		return nil, ErrTestMockGeneric
	}
	state := athenatypes.WorkGroupStateEnabled
	if m.WGDisabled {
		state = athenatypes.WorkGroupStateDisabled
	}
	return &athena.GetWorkGroupOutput{WorkGroup: &athenatypes.WorkGroup{State: state}}, nil
}

// queryToQID maps StartQueryExecution input strings to the QID the
// QueryExecution flow should subsequently observe. Each entry triggers a
// successful StartQueryExecution; the body of the test is then driven by
// the matching qidStates entry in GetQueryExecution.
var queryToQID = map[string]string{
	"select 1":                      "PING_OK_QID",
	"SELECTExecContext_OK":          "SELECTExecContext_OK_QID",
	"SELECTQueryContext_OK":         "SELECTQueryContext_OK_QID",
	"SELECTQueryContext_'OK'":       "SELECTQueryContext_OK_QID",
	"SELECTQueryContext_?":          "SELECTQueryContext_OK_QID",
	"SELECTQueryContext_CANCEL_OK":  "SELECTQueryContext_CANCEL_OK_QID",
	"SELECTQueryContext_AWS_CANCEL": "SELECTQueryContext_AWS_CANCEL_QID",
	"SELECTQueryContext_AWS_FAIL":   "SELECTQueryContext_AWS_FAIL_QID",
	"SELECTQueryContext_CANCEL_FAIL": "SELECTQueryContext_CANCEL_FAIL_QID",
	"SELECTQueryContext_TIMEOUT":    "SELECTQueryContext_TIMEOUT_QID",
	"When_StartQueryExecution_Succeed_but_GetQueryExecutionWithContext_return_nil_and_error": "When_StartQueryExecution_Succeed_but_GetQueryExecutionWithContext_return_nil_and_error_QID",
	"StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateCancelled":       "QueryExecutionStateCancelled_QID",
	"StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateFailed":          "QueryExecutionStateFailed_QID",
}

func (m *mockAthenaClient) StartQueryExecution(_ context.Context, s *athena.StartQueryExecutionInput, _ ...func(options *athena.Options)) (*athena.StartQueryExecutionOutput, error) {
	q := *s.QueryString
	if qid, ok := queryToQID[strings.ToLower(q)]; ok {
		return &athena.StartQueryExecutionOutput{QueryExecutionId: &qid}, nil
	}
	if qid, ok := queryToQID[q]; ok {
		return &athena.StartQueryExecutionOutput{QueryExecutionId: &qid}, nil
	}
	switch q {
	case "StartQueryExecution_nil_error":
		return nil, ErrTestMockGeneric
	case "FAILED_AFTER_GETQID":
		qid := "FAILED_AFTER_GETQID_123"
		return &athena.StartQueryExecutionOutput{QueryExecutionId: &qid}, fmt.Errorf("FAILED_AFTER_GETQID_FAILED")
	case "FAILED_AFTER_GETQID2":
		qid := "FAILED_AFTER_GETQID_123"
		smithyErr := &smithyhttp.ResponseError{Err: fmt.Errorf("FAILED_AFTER_GETQID_FAILED")}
		return &athena.StartQueryExecutionOutput{QueryExecutionId: &qid},
			&awshttp.ResponseError{ResponseError: smithyErr, RequestID: "unk"}
	}
	return nil, nil
}

type qeOpt func(*athenatypes.QueryExecution)

func qe(qid string, state athenatypes.QueryExecutionState, opts ...qeOpt) *athena.GetQueryExecutionOutput {
	ex := &athenatypes.QueryExecution{
		Query:            &qid,
		QueryExecutionId: &qid,
		Status:           &athenatypes.QueryExecutionStatus{State: state},
	}
	for _, o := range opts {
		o(ex)
	}
	return &athena.GetQueryExecutionOutput{QueryExecution: ex}
}

func withDataScanned(n int64) qeOpt {
	return func(e *athenatypes.QueryExecution) {
		e.Statistics = &athenatypes.QueryExecutionStatistics{DataScannedInBytes: &n}
	}
}

func withStatementType(s athenatypes.StatementType) qeOpt {
	return func(e *athenatypes.QueryExecution) { e.StatementType = s }
}

func withStateChangeReason(r string) qeOpt {
	return func(e *athenatypes.QueryExecution) { e.Status.StateChangeReason = &r }
}

// qidQueryExecutions is the static side of the GetQueryExecution mock.
// QIDs that should produce an error from GetQueryExecution live in
// qidErrors below.
var qidQueryExecutions = map[string]*athena.GetQueryExecutionOutput{
	"PING_OK_QID": qe("PING_OK_QID", athenatypes.QueryExecutionStateSucceeded),
	"SELECTExecContext_OK_QID": qe("SELECTExecContext_OK_QID",
		athenatypes.QueryExecutionStateSucceeded, withDataScanned(123)),
	"SELECTQueryContext_OK_QID": qe("SELECTQueryContext_OK_QID",
		athenatypes.QueryExecutionStateSucceeded, withStatementType(athenatypes.StatementTypeDdl)),
	"SELECTQueryContext_CANCEL_OK_QID": qe("SELECTQueryContext_CANCEL_OK_QID",
		athenatypes.QueryExecutionStateQueued,
		withStatementType(athenatypes.StatementTypeDdl), withDataScanned(123)),
	"SELECTQueryContext_AWS_CANCEL_QID": qe("SELECTQueryContext_AWS_CANCEL_QID",
		athenatypes.QueryExecutionStateCancelled, withDataScanned(123)),
	"SELECTQueryContext_AWS_FAIL_QID": qe("SELECTQueryContext_AWS_FAIL_QID",
		athenatypes.QueryExecutionStateFailed, withStateChangeReason("something_broken")),
	"SELECTQueryContext_CANCEL_FAIL_QID": qe("SELECTQueryContext_CANCEL_FAIL_QID",
		athenatypes.QueryExecutionStateQueued),
	"SELECTQueryContext_TIMEOUT_QID": qe("SELECTQueryContext_TIMEOUT_QID",
		athenatypes.QueryExecutionStateQueued,
		withStatementType(athenatypes.StatementType("TIMEOUT_NOW"))),
	"c89088ab-595d-4ee6-a9ce-73b55aeb8900": qe("SELECTQueryContext_CANCEL_OK_QID",
		athenatypes.QueryExecutionStateQueued,
		withStatementType(athenatypes.StatementTypeDdl), withDataScanned(123)),
}

var qidErrors = map[string]error{
	"When_StartQueryExecution_Succeed_but_GetQueryExecutionWithContext_return_nil_and_error_QID": ErrTestMockGeneric,
	"QueryExecutionStateCancelled_QID": context.Canceled,
	"QueryExecutionStateFailed_QID":    ErrTestMockFailedByAthena,
}

func (m *mockAthenaClient) GetQueryExecution(_ context.Context, input *athena.GetQueryExecutionInput, _ ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error) {
	qid := *input.QueryExecutionId
	if err, ok := qidErrors[qid]; ok {
		return nil, err
	}
	if out, ok := qidQueryExecutions[qid]; ok {
		return out, nil
	}
	return nil, ErrTestMockGeneric
}

// stopQueryNoError is the set of QIDs whose StopQueryExecution should
// succeed silently. Any other QID maps to ErrTestMockGeneric.
var stopQueryNoError = map[string]struct{}{
	"SELECTQueryContext_CANCEL_OK_QID":     {},
	"c89088ab-595d-4ee6-a9ce-73b55aeb8954": {},
}

func (m *mockAthenaClient) StopQueryExecution(_ context.Context, input *athena.StopQueryExecutionInput, _ ...func(*athena.Options)) (*athena.StopQueryExecutionOutput, error) {
	if _, ok := stopQueryNoError[*input.QueryExecutionId]; ok {
		return &athena.StopQueryExecutionOutput{}, nil
	}
	return nil, ErrTestMockGeneric
}

// --- multi-page paginator fixtures ----------------------------------------

func MultiplePagesQueryResponse(token string) (*athena.GetQueryResultsOutput, error) {
	columns := createTestColumns()
	switch token {
	case "":
		next := "a1"
		return newRandomHeaderResultPage(columns, &next, 6), nil
	case "a1":
		next := "a2"
		return newRandomHeaderlessResultPage(columns, &next, 10), nil
	case "a2":
		next := "a3"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a3":
		next := "a4"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a4":
		return newRandomHeaderlessResultPage(columns, nil, 10), nil
	default:
		return nil, ErrTestMockGeneric
	}
}

// MultiplePagesQueryFailedResponse forces a GetQueryResults transport-level
// failure on the page after "a3" via the reserved NextToken sentinel.
func MultiplePagesQueryFailedResponse(token string) (*athena.GetQueryResultsOutput, error) {
	columns := createTestColumns()
	switch token {
	case "":
		next := "a1"
		return newRandomHeaderResultPage(columns, &next, 6), nil
	case "a1":
		next := "a2"
		return newRandomHeaderlessResultPage(columns, &next, 3), nil
	case "a2":
		next := "a3"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a3":
		next := "GetQueryResultsWithContext_return_error"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a4":
		return newRandomHeaderlessResultPage(columns, nil, 10), nil
	default:
		return nil, ErrTestMockGeneric
	}
}

// MultiplePagesEmptyRowInPageResponse exercises the "page with no rows"
// branch of fetchNextPage, followed by a forced error two pages later.
func MultiplePagesEmptyRowInPageResponse(token string) (*athena.GetQueryResultsOutput, error) {
	columns := createTestColumns()
	switch token {
	case "":
		next := "a1"
		return newRandomHeaderResultPage(columns, &next, 6), nil
	case "a1":
		next := "a2"
		return newRandomHeaderlessResultPage(columns, &next, 0), nil
	case "a2":
		next := "a3"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a3":
		next := "GetQueryResultsWithContext_return_error"
		return newRandomHeaderlessResultPage(columns, &next, 5), nil
	case "a4":
		return newRandomHeaderlessResultPage(columns, nil, 10), nil
	default:
		return nil, ErrTestMockGeneric
	}
}

func createTestColumns() []athenatypes.ColumnInfo {
	return []athenatypes.ColumnInfo{
		newColumnInfo("test_array", "array"),
		newColumnInfo("active", "boolean"),
		newColumnInfo("company_name", "string"),
		newColumnInfo("project", "string"),
		newColumnInfo("uid", "integer"),
		newColumnInfo("regitser_date", "date"),
		newColumnInfo("regitser_ts", "timestamp"),
	}
}
