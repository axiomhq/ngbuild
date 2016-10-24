package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyConfig(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	previousBaseDir := configBaseDir
	configBaseDir = "testdata"
	configCache = make(map[string]config) // clear out the cache

	mc, err := loadMasterConfig()
	require.NoError(err)

	ac, err := loadAppConfig("testapp")
	require.NoError(err)
	assert.NotEqual(mc["Foo"], ac["Foo"], "ensures that the app config overrides master")

	type NormalConf struct {
		Foo string
		Bar string
	}

	normal := NormalConf{}
	err = applyConfig("testapp", &normal)
	require.NoError(err)
	assert.EqualValues(NormalConf{"a different string", "something else"}, normal)

	type IntegrationConf struct {
		Foo string
		Baz string
	}
	integration := IntegrationConf{}
	err = applyIntegrationConfig("testapp", "testintegration", &integration)
	require.NoError(err)
	assert.EqualValues(IntegrationConf{"A different bar", "FooBarBaz"}, integration)

	configBaseDir = previousBaseDir
}
