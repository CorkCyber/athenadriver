// SPDX-License-Identifier: MIT

package athenadriver

import (
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// WGTags is a wrapper of []*athena.Tag.
type WGTags struct {
	tags []athenatypes.Tag
}

// NewWGTags is to create a new WGTags.
func NewWGTags() *WGTags {
	return &WGTags{tags: make([]athenatypes.Tag, 0, 2)}
}

// AddTag is to add tag.
func (t *WGTags) AddTag(k string, v string) {
	t.tags = append(t.tags, athenatypes.Tag{
		Key:   &k,
		Value: &v})
}

// Get is a getter.
func (t *WGTags) Get() []athenatypes.Tag {
	if t == nil {
		return nil
	}
	return t.tags
}
