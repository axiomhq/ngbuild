package core

import (
	"errors"
	"io"
	"sync/atomic"
	"time"
)

type refcount uint64

func (ref *refcount) Add() {
	atomic.AddUint64((*uint64)(ref), 1)
}

func (ref *refcount) Remove() {
	atomic.AddUint64((*uint64)(ref), ^uint64(0))
}

func (ref *refcount) Get() uint64 {
	return atomic.LoadUint64((*uint64)(ref))
}

type build struct {
	config *BuildConfig
	token  string

	parentApp App

	stdpipes *stdpipes
	ref      refcount

	finished uint64
	exitCode int
}

func newBuild(app App, token string, config *BuildConfig) *build {
	return &build{
		parentApp: app,
		token:     token,
		config:    config,
	}
}

func (b *build) hasFinished() bool {
	if b == nil {
		return true
	}
	return atomic.LoadUint64(&b.finished) > 0
}

// Start will start the given build, it will error with ErrAlreadyStarted if the build is already running
func (b *build) Start() error { return nil }

// Stop will stop the given build, it will error with ErrAlreadyStopped if the build has finished
func (b *build) Stop() error { return nil }

// Ref will add a reference to this build, the build will not cleanup until all references are dropped
func (b *build) Ref() {
	if b == nil {
		return
	}

	b.ref.Add()
}

// Unref will remove a reference to this build
func (b *build) Unref() {
	if b == nil {
		return
	}

	b.ref.Remove()
	if b.ref.Get() < 1 {
		// TODO cleanup
	}
}

// NewBuild will construct a new Build using this build as a base,
// it is essentally a retry system
func (b *build) NewBuild() (token string, err error) {

	return
}

func (b *build) Group() string {
	if b == nil || b.config == nil {
		return ""
	}

	return b.config.Group
}

func (b *build) Token() string {
	if b == nil {
		return ""
	}
	return b.token
}

// Stdout will return an io.Reader that will provide the stdout for this build
func (b *build) Stdout() (io.Reader, error) {
	if b == nil {
		return nil, errors.New("b is nil")
	}

	if b.stdpipes == nil {
		return nil, ErrProcessNotStarted
	}

	return b.stdpipes.NewStdoutReader(), nil
}

// Stderr will return an io.Reader that will provide the stdin for this build
func (b *build) Stderr() (io.Reader, error) {
	if b == nil {
		return nil, errors.New("b is nil")
	}

	if b.stdpipes == nil {
		return nil, ErrProcessNotStarted
	}

	return b.stdpipes.NewStderrReader(), nil
}

// ExitCode will return the process exit code, will error ErrProcessNotFinished
func (b *build) ExitCode() (int, error) {
	if b == nil {
		return 0, errors.New("b is nil")
	}

	if b.hasFinished() {
		return b.exitCode, nil
	}

	return 0, ErrProcessNotFinished
}

// Artifact will return a list of filepaths for the given artifact name
func (b *build) Artifact(name string) []string {
	return nil
}

// BuildTime will return how long the build took, will return 0 if build hasn't started yet
func (b *build) BuildTime() time.Duration {
	return time.Duration(0)
}

// History will return an array of previous Build's in this builds group
func (b *build) History() []Build {
	return nil
}
