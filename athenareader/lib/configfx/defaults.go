// SPDX-License-Identifier: MIT

package configfx

// Built-in default placeholders for the `-b` flag and AWS credential
// chain. Replace via athenareader.config or environment in normal use;
// the dummies here just make the CLI start when nothing is configured.
const (
	defaultOutputBucket    = "s3://qr-athena-query-result-prod/Henry/"
	defaultAccessID        = "dummy"
	defaultSecretAccessKey = "dummy"
)
