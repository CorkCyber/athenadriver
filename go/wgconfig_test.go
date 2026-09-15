// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
)

// A workgroup created remotely from the default config must not enforce its
// own (absent) ResultConfiguration, otherwise every query against it fails
// with "No output location provided" despite the driver sending a valid
// OutputLocation per query.
func TestGetDefaultWGConfig_DoesNotEnforceEmptyResultConfig(t *testing.T) {
	c := GetDefaultWGConfig()
	if aws.ToBool(c.EnforceWorkGroupConfiguration) {
		assert.NotNil(t, c.ResultConfiguration, "enforcement on requires a ResultConfiguration")
		assert.NotEmpty(t, aws.ToString(c.ResultConfiguration.OutputLocation))
	}
	// Default shape today: enforcement off, client-side OutputLocation wins.
	assert.False(t, aws.ToBool(c.EnforceWorkGroupConfiguration))
}
