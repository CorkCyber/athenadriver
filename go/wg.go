// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// Workgroup is a wrapper of Athena Workgroup.
type Workgroup struct {
	Name   string
	Config *athenatypes.WorkGroupConfiguration
	Tags   *WGTags
}

// NewWG creates a Workgroup. A nil config falls back to
// GetDefaultWGConfig(); nil tags fall back to an empty WGTags.
func NewWG(name string, config *athenatypes.WorkGroupConfiguration, tags *WGTags) *Workgroup {
	if config == nil {
		config = GetDefaultWGConfig()
	}
	if tags == nil {
		tags = NewWGTags()
	}
	return &Workgroup{
		Name:   name,
		Config: config,
		Tags:   tags,
	}
}

// getWG is to get Athena Workgroup from AWS remotely.
func getWG(ctx context.Context, client AthenaClient, Name string) (*athenatypes.WorkGroup, error) {
	if client == nil {
		return nil, ErrAthenaNilClient
	}
	getWorkGroupOutput, err := client.GetWorkGroup(ctx,
		&athena.GetWorkGroupInput{
			WorkGroup: aws.String(Name),
		})
	if err != nil {
		return nil, err
	}
	return getWorkGroupOutput.WorkGroup, nil
}

// CreateWGRemotely is to create a Workgroup remotely.
func (w *Workgroup) CreateWGRemotely(ctx context.Context, athenaClient AthenaClient) error {
	tags := w.Tags.Get()
	var err error
	if len(tags) == 0 {
		_, err = athenaClient.CreateWorkGroup(ctx, &athena.CreateWorkGroupInput{
			Configuration: w.Config,
			Name:          aws.String(w.Name),
		})
	} else {
		_, err = athenaClient.CreateWorkGroup(ctx, &athena.CreateWorkGroupInput{
			Configuration: w.Config,
			Name:          aws.String(w.Name),
			Tags:          w.Tags.Get(),
		})
	}
	return err
}
