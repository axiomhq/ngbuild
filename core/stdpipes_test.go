package core

import (
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func RunConcurrent(count int, fn func(int)) {
	wg := sync.WaitGroup{}
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(index int) {
			fn(index)
			wg.Done()
		}(i)
	}

	wg.Wait()
}

type mockReader struct {
	readFn func(p []byte) (n int, err error)
	data   chan []byte
}

func (m *mockReader) Read(p []byte) (int, error) {
	return m.readFn(p)
}

func TestStdPipes(t *testing.T) {
	assert := assert.New(t)
	testMarker := []byte("::testmarker::")
	totalJobs := 10

	stdoutmock := &mockReader{data: make(chan []byte, 1)}
	stderrmock := &mockReader{data: make(chan []byte, 1)}

	stdoutmock.readFn = func(p []byte) (int, error) {
		data, ok := <-stdoutmock.data
		if ok {
			return copy(p, data), nil
		} else {
			return 0, io.EOF
		}
	}

	stderrmock.readFn = func(p []byte) (int, error) {
		data, ok := <-stderrmock.data
		if ok {
			return copy(p, data), nil
		} else {
			return 0, io.EOF
		}
	}

	piper := newStdpipes(stdoutmock, stderrmock)
	stdoutReaders := make([]io.Reader, totalJobs)
	stderrReaders := make([]io.Reader, totalJobs)

	for i := 0; i < totalJobs; i++ {
		stdoutReaders[i] = piper.NewStdoutReader()
		stderrReaders[i] = piper.NewStderrReader()
	}

	stdoutmock.data <- testMarker
	stderrmock.data <- testMarker

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader, ok := stdoutReaders[i].(*stdreader)
		assert.True(ok)
		stderrReader, ok := stderrReaders[i].(*stdreader)
		assert.True(ok)
		buf := make([]byte, len(testMarker))
		n, err := stdoutReader.Read(buf)
		assert.EqualValues(n, len(testMarker), "1out")
		assert.NoError(err, "1out")
		assert.Equal(testMarker, buf, "1out")
		assert.Equal(len(testMarker), stdoutReader.position, "1out")

		buf = make([]byte, len(testMarker))
		n, err = stderrReader.Read(buf)
		assert.EqualValues(n, len(testMarker))
		assert.NoError(err)
		assert.Equal(testMarker, buf)
		assert.Equal(len(testMarker), stderrReader.position)

	})

	// First set of reads passed, make sure we can read multiple times
	position := len(testMarker)
	testMarker = []byte("~~SecondMarker~~")
	stdoutmock.data <- testMarker
	stderrmock.data <- testMarker

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader := stdoutReaders[i].(*stdreader)
		stderrReader := stderrReaders[i].(*stdreader)
		buf := make([]byte, len(testMarker))
		n, err := stdoutReader.Read(buf)
		assert.EqualValues(n, len(testMarker))
		assert.NoError(err)
		assert.Equal(testMarker, buf)
		assert.Equal(position+len(testMarker), stdoutReader.position)

		buf = make([]byte, len(testMarker))
		n, err = stderrReader.Read(buf)
		assert.EqualValues(n, len(testMarker))
		assert.NoError(err)
		assert.Equal(testMarker, buf)
		assert.Equal(position+len(testMarker), stderrReader.position)
	})

	// make sure closing the pipes passes back io.EOF
	close(stdoutmock.data)
	close(stderrmock.data)

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader := stdoutReaders[i]
		stderrReader := stderrReaders[i]

		buf := make([]byte, 1)
		n, err := stdoutReader.Read(buf)
		assert.Zero(n)
		assert.EqualError(err, io.EOF.Error())

		n, err = stderrReader.Read(buf)
		assert.Zero(n)
		assert.EqualError(err, io.EOF.Error())
	})
}
