package core

// We need a way of essentially multiplexing one io.Reader over many io.Readers
// this does that, it lets all integrations have their own stderr/reader io.Readers
// all of them contain all the data and will block their Reads as expected

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

type stdreader struct {
	parent   *stdpipes
	position int
}

func (s *stdreader) Read(p []byte) (n int, err error) {
	if s == nil {
		return 0, errors.New("stdreader is nil")
	}

	if s.parent == nil {
		return 0, errors.New("lost connection to std pipe")
	}

	if len(p) < 1 {
		return 0, errors.New("p is too small to read any data")
	}

	cachedData, closed := s.parent.GetCache(s.position)
	if len(cachedData) == 0 && closed == true {
		return 0, io.EOF
	}

	n = copy(p, cachedData)
	s.position += n
	return
}

type stdpipes struct {
	m sync.RWMutex

	reader     io.ReadCloser
	readCache  bytes.Buffer
	readWait   *sync.Cond
	readClosed uint64

	cacheSize uint64

	Done chan struct{}
}

// newStdpipes will return a new stdpipes structure to manage the given pipes
func newStdpipes(readerPipe io.ReadCloser) *stdpipes {
	pipes := &stdpipes{
		readWait: sync.NewCond(&sync.Mutex{}),
		reader:   readerPipe,

		Done: make(chan struct{}, 1),
	}

	go pipes.readLoop()

	return pipes
}

func (p *stdpipes) getclosed() bool {
	return atomic.LoadUint64(&p.readClosed) > 0
}

func (p *stdpipes) getpipe() io.Reader {
	return p.reader
}

func (p *stdpipes) getcache() *bytes.Buffer {
	return &p.readCache
}

func (p *stdpipes) getwaiter() *sync.Cond {
	return p.readWait
}

func writeall(dst *bytes.Buffer, src []byte) error {
	n := len(src)
	for n > 0 {
		wn, err := dst.Write(src[:n])
		if err != nil {
			return err
		}
		n -= wn
	}

	return nil
}

func (p *stdpipes) readLoop() {
	for shouldExit := false; shouldExit == false; {
		var buf [1024]byte
		var n int
		var err error

		if n, err = p.getpipe().Read(buf[:]); err != nil {
			atomic.StoreUint64(&p.readClosed, 1)
			if err != io.EOF {
				logcritf("pipe read errored: %s", err)
			}
			shouldExit = true
		}

		p.m.Lock()
		if err = writeall(p.getcache(), buf[:n]); err != nil {
			atomic.StoreUint64(&p.readClosed, 1)
			logcritf("pipe write errored: %s", err)

			shouldExit = true
		}

		atomic.AddUint64(&p.cacheSize, uint64(n))
		p.m.Unlock()

		waiter := p.getwaiter()
		waiter.Broadcast()
	}

	if p.getclosed() {
		p.Done <- struct{}{}
	}
}

// NewreaderReader will return an io.Reader that can read from the reader pipe
func (p *stdpipes) NewReader() io.Reader {
	reader := stdreader{parent: p}
	return &reader
}

// newdata will return new if there is any new activity
// it will apply locks for easy use in conditionals
func (p *stdpipes) hasNewData(pipetype, oldlen int) bool {
	p.m.RLock()
	defer p.m.RUnlock()

	return p.getcache().Len() > oldlen || p.getclosed()
}

// GetCache will return the cache of the given pipetype at the given
// seek position, it will block if position == len(totalCache)
func (p *stdpipes) GetCache(position int) (buf []byte, closed bool) {
	defer func() {
		p.m.Unlock()
	}()

	// if the current position is at the end of the cache and the input pipe isn't closed
	// then we need to wait on new data.
	p.readWait.L.Lock()
	for uint64(position) >= atomic.LoadUint64(&p.cacheSize) && p.getclosed() == false {
		p.readWait.Wait()
	}
	p.m.Lock()
	p.readWait.L.Unlock()

	cache := p.getcache().Bytes()
	if len(cache) <= position {
		buf = nil
		closed = p.getclosed()
	} else {
		cache = cache[position:]
		buf = make([]byte, len(cache))
		copy(buf, cache)
	}

	return
}

func (p *stdpipes) Close() {
	p.reader.Close() //nolint (errcheck)
	p.Done <- struct{}{}
}
