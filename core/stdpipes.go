package core

// We need a way of essentially multiplexing one io.Reader over many io.Readers
// this does that, it lets all integrations have their own stderr/stdout io.Readers
// all of them contain all the data and will block their Reads as expected

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

const (
	_ = iota
	typeStdout
	typeStderr
)

type stdreader struct {
	m        sync.Mutex
	parent   *stdpipes
	pipetype int
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

	cachedData, closed := s.parent.GetCache(s.pipetype, s.position)
	if len(cachedData) == 0 && closed == true {
		return 0, io.EOF
	}

	n = copy(p, cachedData)
	s.position += n
	return
}

type stdpipes struct {
	m sync.RWMutex

	stdout io.Reader
	stderr io.Reader

	stdoutCache bytes.Buffer
	stderrCache bytes.Buffer

	stdoutWait *sync.Cond
	stderrWait *sync.Cond

	stdoutClosed uint64
	stderrClosed uint64

	Done chan struct{}
}

// newStdpipes will return a new stdpipes structure to manage the given pipes
func newStdpipes(stdoutPipe io.Reader, stderrPipe io.Reader) *stdpipes {
	pipes := &stdpipes{
		stdoutWait: sync.NewCond(&sync.Mutex{}),
		stderrWait: sync.NewCond(&sync.Mutex{}),

		stdout: stdoutPipe,
		stderr: stderrPipe,

		Done: make(chan struct{}, 1),
	}

	go pipes.readLoop(typeStdout)
	go pipes.readLoop(typeStderr)

	return pipes
}

func (p *stdpipes) getclosed(pipetype int) bool {
	switch pipetype {
	case typeStdout:
		return atomic.LoadUint64(&p.stdoutClosed) > 0
	case typeStderr:
		return atomic.LoadUint64(&p.stderrClosed) > 0
	default:
		return true
	}
}

func (p *stdpipes) getpipe(pipetype int) io.Reader {
	switch pipetype {
	case typeStdout:
		return p.stdout
	case typeStderr:
		return p.stderr
	default:
		return nil
	}
}

func (p *stdpipes) getcache(pipetype int) *bytes.Buffer {
	switch pipetype {
	case typeStdout:
		return &p.stdoutCache
	case typeStderr:
		return &p.stderrCache
	default:
		return nil
	}
}

func (p *stdpipes) getwaiter(pipetype int) *sync.Cond {
	switch pipetype {
	case typeStdout:
		return p.stdoutWait
	case typeStderr:
		return p.stderrWait
	default:
		return nil
	}
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

func (p *stdpipes) readLoop(pipetype int) {
	for shouldExit := false; shouldExit == false; {
		var buf [1024]byte
		var n int
		var err, werr error

		n, err = p.getpipe(pipetype).Read(buf[:])
		p.m.Lock()

		werr = writeall(p.getcache(pipetype), buf[:n])
		if err != nil || werr != nil {
			switch pipetype {
			case typeStdout:
				atomic.StoreUint64(&p.stdoutClosed, 1)
			case typeStderr:
				atomic.StoreUint64(&p.stderrClosed, 1)
			}
			shouldExit = true
		}

		waiter := p.getwaiter(pipetype)
		p.m.Unlock()
		waiter.Broadcast()
	}

	if p.getclosed(typeStdout) && p.getclosed(typeStderr) {
		p.Done <- struct{}{}
	}
}

// NewStdoutReader will return an io.Reader that can read from the stdout pipe
func (p *stdpipes) NewStdoutReader() io.Reader {
	reader := stdreader{parent: p, pipetype: typeStdout}
	return &reader
}

// NewStderrReader will return an io.Reader that can read from the stderr pipe
func (p *stdpipes) NewStderrReader() io.Reader {
	reader := stdreader{parent: p, pipetype: typeStderr}
	return &reader
}

// newdata will return new if there is any new activity
// it will apply locks for easy use in conditionals
func (p *stdpipes) hasNewData(pipetype, oldlen int) bool {
	p.m.RLock()
	defer p.m.RUnlock()

	return p.getcache(pipetype).Len() > oldlen || p.getclosed(pipetype)
}

// GetCache will return the cache of the given pipetype at the given
// seek position, it will block if position == len(totalCache)
func (p *stdpipes) GetCache(pipetype, position int) (buf []byte, closed bool) {
	p.m.Lock()
	defer p.m.Unlock()

	if oldlen := p.getcache(pipetype).Len(); oldlen == position {
		// early exit if we are already closed
		if p.getclosed(pipetype) {
			return nil, true
		}

		// reached end of current cache, wait for new data
		waiter := p.getwaiter(pipetype)
		waiter.L.Lock()

		// this is a little messy, normally you do
		/* for checkCondition() == false {
		 *     waiter.Wait()
		 * }
		 */
		// but we need to hold a lock during checkCondition()
		// this requires that we hold the mutex from our previous lock until we have done our first check
		// this stops a race between other instances of GetCache() getting the p.m lock and not unlocking
		// because we have the waiter.L lock
		firstLoop := true
		for {
			if firstLoop == false {
				p.m.Lock()
			} else {
				firstLoop = false
			}

			if p.getcache(pipetype).Len() > oldlen || p.getclosed(pipetype) {
				p.m.Unlock()
				break
			}
			p.m.Unlock()

			waiter.Wait()
		}

		// more data or we closed
		p.m.Lock()
		waiter.L.Unlock()
	}

	cache := p.getcache(pipetype).Bytes()
	if len(cache) <= position {
		buf = nil
		closed = p.getclosed(pipetype)
	} else {
		cache = cache[position:]
		buf = make([]byte, len(cache))
		copy(buf, cache)
	}

	return
}
