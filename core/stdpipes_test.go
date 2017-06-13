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

func (m *mockReader) Close() error {
	return nil
}

func TestStdPipes(t *testing.T) {
	assert := assert.New(t)
	testMarker := []byte("::testmarker::")
	totalJobs := 32
	dataRepeatTotal := 64

	stdoutmock := &mockReader{data: make(chan []byte, 0)}

	stdoutmock.readFn = func(p []byte) (int, error) {
		data, ok := <-stdoutmock.data
		if ok {
			return copy(p, data), nil
		} else {
			return 0, io.EOF
		}
	}

	piper := newStdpipes(stdoutmock)
	stdoutReaders := make([]io.Reader, totalJobs)

	for i := 0; i < totalJobs; i++ {
		stdoutReaders[i] = piper.NewReader()
	}

	stdoutmock.data <- testMarker

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader, ok := stdoutReaders[i].(*stdreader)
		assert.True(ok)

		buf := make([]byte, len(testMarker))
		n, err := stdoutReader.Read(buf)
		assert.EqualValues(n, len(testMarker), "1out")
		assert.NoError(err, "1out")
		assert.Equal(testMarker, buf, "1out")
		assert.Equal(len(testMarker), stdoutReader.position, "1out")
	})

	// First set of reads passed, make sure we can read multiple times
	position := len(testMarker)
	testMarker = []byte("~~SecondMarker~~")

	go func() {
		for datarepeat := 0; datarepeat < dataRepeatTotal; datarepeat++ {
			stdoutmock.data <- testMarker
		}
	}()

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader := stdoutReaders[i].(*stdreader)
		subPos := position
		buf := make([]byte, len(testMarker))
		for datarepeat := 0; datarepeat < dataRepeatTotal; datarepeat++ {
			n, err := stdoutReader.Read(buf)
			assert.EqualValues(n, len(testMarker))
			assert.NoError(err)
			assert.Equal(testMarker, buf)
			assert.Equal(subPos+len(testMarker), stdoutReader.position)
			subPos += n
		}
	})

	// make sure closing the pipes passes back io.EOF
	close(stdoutmock.data)

	RunConcurrent(totalJobs, func(i int) {
		stdoutReader := stdoutReaders[i]

		buf := make([]byte, 1)
		n, err := stdoutReader.Read(buf)
		assert.Zero(n)
		assert.EqualError(err, io.EOF.Error())
	})
}
