package mocks

import (
	"github.com/stretchr/testify/mock"
	"github.com/watchly/ngbuild/core"
)

type App struct {
	mock.Mock
}

func (m *App) Name() string {
	return m.Called().String(0)
}

func (m *App) Config(namespace string, conf interface{}) error {
	return m.Called(namespace, conf).Error(0)
}

func (m *App) SendEvent(event string) {
	m.Called(event)
}

func (m *App) Listen(event string, listener func(map[string]string)) core.EventHandler {
	return (core.EventHandler)(m.Called(event, listener).Int(0))
}

func (m *App) RemoveEventHandler(handler core.EventHandler) {
	m.Called(handler)
}

func (m *App) NewBuild(group string, config *core.BuildConfig) (token string, err error) {
	args := m.Called(group, config)
	return args.String(0), args.Error(1)
}

func (m *App) GetBuild(token string) (core.Build, error) {
	args := m.Called(token)
	if args.Get(0) != nil {
		return (args.Get(0)).(core.Build), nil
	}
	return nil, args.Error(1)
}
