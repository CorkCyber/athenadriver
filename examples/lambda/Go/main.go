// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	drv "github.com/CorkCyber/athenadriver/v2/go"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type response struct {
	QueryResult string `json:"result"`
}

// Make sure to select a role which can query Athena!
// https://epsagon.com/blog/getting-started-with-aws-lambda-and-go/
// https://docs.aws.amazon.com/lambda/latest/dg/golang-package.html
func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// 1. Leave credentials unset so the driver resolves them via the
	// standard AWS SDK v2 chain — on Lambda that's the function's
	// execution role, picked up from the container's IMDS/env credentials.
	conf := drv.NewNoOpsConfig()
	if err := conf.SetOutputBucket("s3://athena-query-result/lambda/"); err != nil {
		return events.APIGatewayProxyResponse{}, err
	}
	// 2. Open Connection.
	db, err := sql.Open(drv.DriverName, conf.Stringify())
	if err != nil {
		return events.APIGatewayProxyResponse{Body: string(err.Error()), StatusCode: 500}, err
	}
	// 3. Query
	rows, err := db.QueryContext(ctx, "select 123")
	if err != nil {
		return events.APIGatewayProxyResponse{Body: string(err.Error()), StatusCode: 500}, err
	}
	defer rows.Close()
	resp := &response{
		QueryResult: drv.ColsRowsToCSV(rows),
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return events.APIGatewayProxyResponse{}, err
	}
	return events.APIGatewayProxyResponse{Body: string(body), StatusCode: 200}, nil
}
func main() {
	lambda.Start(handleRequest)
}
