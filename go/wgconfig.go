// SPDX-License-Identifier: MIT

package athenadriver

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// GetDefaultWGConfig returns a WorkGroupConfiguration with a 1 GiB scan
// cap, workgroup enforcement on, CloudWatch metrics on, and
// requester-pays off.
func GetDefaultWGConfig() *athenatypes.WorkGroupConfiguration {
	return &athenatypes.WorkGroupConfiguration{
		BytesScannedCutoffPerQuery:      aws.Int64(DefaultBytesScannedCutoffPerQuery),
		EnforceWorkGroupConfiguration:   aws.Bool(true),
		PublishCloudWatchMetricsEnabled: aws.Bool(true),
		RequesterPaysEnabled:            aws.Bool(false),
	}
}
