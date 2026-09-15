// SPDX-License-Identifier: MIT

package athenadriver

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// GetDefaultWGConfig returns a WorkGroupConfiguration with a 1 GiB scan
// cap, workgroup enforcement OFF, CloudWatch metrics on, and
// requester-pays off.
//
// Enforcement is off on purpose: this config has no ResultConfiguration,
// and enforcement=true would let the workgroup's empty output location
// override the driver's per-query OutputLocation, failing every query
// with "No output location provided". Pass your own config with a
// ResultConfiguration to enable enforcement.
func GetDefaultWGConfig() *athenatypes.WorkGroupConfiguration {
	return &athenatypes.WorkGroupConfiguration{
		BytesScannedCutoffPerQuery:      aws.Int64(DefaultBytesScannedCutoffPerQuery),
		EnforceWorkGroupConfiguration:   aws.Bool(false),
		PublishCloudWatchMetricsEnabled: aws.Bool(true),
		RequesterPaysEnabled:            aws.Bool(false),
	}
}
