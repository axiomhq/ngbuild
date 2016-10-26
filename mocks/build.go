package mocks

import (
	"io"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/watchly/ngbuild/core"
)

type Build struct {
	mock.Mock
}

func (m *Build) Start() error {
	return m.Called().Error(0)
}

func (m *Build) Stop() error {
	return m.Called().Error(0)
}

func (m *Build) Ref() {
	m.Called()
}

func (m *Build) Unref() {
	m.Called()
}

func (m *Build) Token() string {
	return m.Called().String(0)
}

func (m *Build) Group() string {
	return m.Called().String(0)
}

func (m *Build) NewBuild() (token string, err error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *Build) Stdout() (io.Reader, error) {
	args := m.Called()
	return (args.Get(0)).(io.Reader), args.Error(1)
}

func (m *Build) Stderr() (io.Reader, error) {
	args := m.Called()
	return (args.Get(0)).(io.Reader), args.Error(1)
}

func (m *Build) ExitCode() (int, error) {
	args := m.Called()
	if args.Get(1) != nil {
		return 0, args.Error(1)
	}
	return (args.Get(0)).(int), nil
}

func (m *Build) Artifact(name string) []string {
	return (m.Called(name).Get(0)).([]string)
}

func (m *Build) BuildTime() time.Duration {
	return (m.Called().Get(0)).(time.Duration)
}

func (m *Build) History() []core.Build {
	args := m.Called()
	if args.Get(0) != nil {
		return (m.Called().Get(0)).([]core.Build)
	}
	return nil
}
