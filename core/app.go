package core

import (
	"errors"
	"sync"
)

type app struct {
	m    sync.RWMutex
	name string

	builds map[string][]Build
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

}

// Listen will provide a channel to select on for a given regular expression
// returned map is the captured groups and values
func (a *app) Listen(event string) <-chan map[string]string {
	return make(chan map[string]string, 1)
}

func (a *app) NewBuild(group string, config *BuildConfig) (token string, err error) {
	if a == nil {
		return "", errors.New("a is nil")
	}

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

	build := newBuild(token, config)
	a.m.Lock()
	a.builds[group] = append(a.builds[group], build)
	a.m.Unlock()

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
