package core

import (
	"errors"
	"sync"
)

type app struct {
	m    sync.RWMutex
	name string

	builds map[string][]Build
	bus    *appbus
}

// NewApp will return a new app with the given name, the name should also be the directory name that the app will
// search for config data in
func NewApp(name string) App {
	return &app{
		name:   name,
		builds: make(map[string][]Build),
		bus:    newAppBus(),
	}
}

// Name is the apps name
func (a *app) Name() string {
	if a == nil {
		return ""
	}

	return a.name
}

// Config will apply the config at namespace onto the given conf structure
// consider this like json.Unmarshal()
func (a *app) Config(namespace string, conf interface{}) error {
	return nil
}

// SendEvent will send the given string on the apps event bus
func (a *app) SendEvent(event string) {
	if a == nil {
		return
	}

	a.bus.Emit(event)
}

// Listen will provide a channel to select on for a given regular expression
// returned map is the captured groups and values
func (a *app) Listen(expr string, listener func(map[string]string)) EventHandler {
	if a == nil {
		return EventHandler(0)
	}

	handler, _ := a.bus.AddListener(expr, listener)
	return handler
}

func (a *app) RemoveEventHandler(handler EventHandler) {
	if a == nil {
		return
	}

	a.bus.RemoveHandler(handler)
}

func (a *app) NewBuild(group string, config *BuildConfig) (token string, err error) {
	if a == nil {
		return "", errors.New("a is nil")
	}

	a.m.Lock()
	defer a.m.Unlock()
	for {
		token = generateToken()
		if build, err := a.GetBuild(token); err == nil {
			return "", err
		} else if build != nil {
			// token already exists
			continue
		}

		break
	}

	build := newBuild(a, token, config)
	a.builds[group] = append(a.builds[group], build)

	return token, nil
}

func (a *app) GetBuild(token string) (Build, error) {
	if a == nil {
		return nil, errors.New("a is nil")
	}

	a.m.RLock()
	defer a.m.RUnlock()
	for _, value := range a.builds {
		for _, build := range value {
			if build.Token() == token {
				return build, nil
			}
		}
	}

	return nil, nil
}
