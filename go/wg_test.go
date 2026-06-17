// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWG(t *testing.T) {
	wgTags := NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu")
	wgTags.AddTag("Uber ID", "123456")
	wgTags.AddTag("Uber Role", "SDE")
	wg := NewWG("henry_wu", nil, wgTags)
	assert.Equal(t, "henry_wu", wg.Name)
	assert.Equal(t, len(wg.Tags.Get()), 3)
}

func TestGetWG(t *testing.T) {
	w, e := getWG(context.Background(), nil, "SELECT_OK")
	assert.Nil(t, w)
	assert.NotNil(t, e)

	athenaClient := newMockAthenaClient()
	w, e = getWG(context.Background(), athenaClient, "SELECT_OK")
	assert.Nil(t, w)
	assert.NotNil(t, e)

	athenaClient.GetWGStatus = true
	w, e = getWG(context.Background(), athenaClient, "SELECT_OK")
	assert.NotNil(t, w)
	assert.Nil(t, e)
}

func TestWorkgroup_CreateWGRemotely(t *testing.T) {
	wgTags := NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu")
	wgTags.AddTag("Uber ID", "123456")
	wgTags.AddTag("Uber Role", "SDE")
	wg := NewWG("henry_wu", nil, wgTags)
	athenaClient := newMockAthenaClient()
	e := wg.CreateWGRemotely(context.Background(), athenaClient)
	assert.NotNil(t, e)
	athenaClient.CreateWGStatus = true
	e = wg.CreateWGRemotely(context.Background(), athenaClient)
	assert.Nil(t, e)
}

func TestWorkgroup_CreateWGRemotely2(t *testing.T) {
	wgTags := NewWGTags()
	wg := NewWG("henry_wu", nil, wgTags)
	athenaClient := newMockAthenaClient()
	e := wg.CreateWGRemotely(context.Background(), athenaClient)
	assert.NotNil(t, e)
	athenaClient.CreateWGStatus = true
	e = wg.CreateWGRemotely(context.Background(), athenaClient)
	assert.Nil(t, e)
}
