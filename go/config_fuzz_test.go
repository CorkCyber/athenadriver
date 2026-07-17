// SPDX-License-Identifier: MIT

package athenadriver

import "testing"

// FuzzNewConfig throws arbitrary bytes at the DSN parser. Any DSN must
// either round-trip losslessly through NewConfig -> Stringify -> NewConfig
// or fail with an error: a panic or a non-idempotent round-trip is a bug.
func FuzzNewConfig(f *testing.F) {
	// Seed with representative shapes.
	f.Add("s3://bucket/prefix/?db=x&region=us-east-1&accessID=id&secretAccessKey=sec")
	f.Add("s3://user:@bucket.example.com/path/?region=eu-west-1&WGRemoteCreation=false")
	f.Add("s3://b/?resultPollIntervalSeconds=3&resultPollBackoffMultiplier=1.5&resultPollMaxIntervalSeconds=30")
	f.Add("s3://b/?DDLQueryTimeout=600&DMLQueryTimeout=1800&catalog=hive")
	f.Add("s3://b/?resultPollIntervalSeconds=notanumber")
	f.Add("s3://b/?region=us-east-1&workgroupName=my_wg&tag=%7Cteam%60data%7Cenv%60prod")
	f.Add("s3://b/?region=us-east-1&webIdentityRoleARN=arn:aws:iam::111:role/x&webIdentityTokenFile=/tmp/token")
	f.Add("s3://b/?region=us-east-1&resultEncryptionOption=SSE_KMS&resultEncryptionKmsKey=arn:aws:kms:us-east-1:111:key/abc")
	f.Add("s3://b/?region=us-east-1&expectedBucketOwner=123456789012")
	f.Add("s3://b/?region=us-east-1&MoneyWise=true&ReadOnly=true&missingAsNil=true")
	f.Add("s3://my_legacy_bucket/?region=us-east-1")
	f.Add("s3://host:9000/?region=us-east-1")
	f.Add("s3:///?region=us-east-1")
	f.Add("")
	f.Add("not-a-url")

	f.Fuzz(func(t *testing.T, dsn string) {
		cfg, err := NewConfig(dsn)
		if err != nil {
			return
		}
		// Round-trip must be idempotent.
		encoded := cfg.Stringify()
		cfg2, err := NewConfig(encoded)
		if err != nil {
			t.Fatalf("re-parse of a valid Stringify output failed: dsn=%q encoded=%q err=%v", dsn, encoded, err)
		}
		if cfg2.Stringify() != encoded {
			t.Fatalf("Stringify not idempotent: first=%q second=%q", encoded, cfg2.Stringify())
		}
	})
}
