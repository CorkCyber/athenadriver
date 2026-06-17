// SPDX-License-Identifier: MIT

package constants

// To run integration test, please replace the below information to match your own AWS account.
const (
	OutputBucket     = "s3://qr-athena-query-result-prod/Henry/"
	OutputBucketDev  = "s3://query-results-bucket-henrywu/"
	OutputBucketProd = "s3://qr-athena-query-result-prod/Henry/"
	Region           = "us-east-2"
	AccessID         = "dummy"
	SecretAccessKey  = "dummy"
)
