// SPDX-License-Identifier: MIT

package configfx

// defaultOutputBucket is the built-in default for the `-b` flag; replace
// it via athenareader.config in normal use. Credentials come from the AWS
// default credential chain, never from a built-in placeholder.
const defaultOutputBucket = "s3://qr-athena-query-result-prod/Henry/"
