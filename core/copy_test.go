package core

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFile(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	testData := "gordon the gopher "
	for len(testData) < 1024 {
		testData = testData + testData
	}

	srcFile, err := ioutil.TempFile(os.TempDir(), "testcopyfile")
	defer srcFile.Close()
	defer os.Remove(srcFile.Name())

	require.NoError(err)
	dstLocation := srcFile.Name() + "destFile"

	require.NoError(writeAll(srcFile, []byte(testData)))
	require.NoError(srcFile.Close())
	require.NoError(CopyFile(srcFile.Name(), dstLocation))
	defer os.Remove(dstLocation)

	copiedFile, err := ioutil.ReadFile(dstLocation)
	require.NoError(err)

	assert.EqualValues(testData, copiedFile)
}
