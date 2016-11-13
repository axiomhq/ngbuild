package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// getAppsLocation will check directories for a ngbuild.conf and an apps/ directory from there
func getAppsLocation() (string, error) {
	dir, err := getNGBuildDirectory()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "apps"), nil
}

// GetApps will return App objects for all the apps we can find on this machine
func GetApps() []App {
	appsLocation, err := getAppsLocation()
	if err != nil {
		return []App{}
	}

	perAppDirs, err := filepath.Glob(filepath.Join(appsLocation, "*"))
	if err != nil {
		logcritf("Couldn't glob %s: %s", appsLocation, err)
		return []App{}
	}

	apps := []App{}
	for _, appDir := range perAppDirs {
		splitDirs := strings.Split(appDir, string(filepath.Separator))
		if len(splitDirs) < 2 {
			logcritf("Could not deterimine app name for :%s", appDir)
			continue
		}
		name := splitDirs[len(splitDirs)-1]
		enabledIntegrations := struct {
			EnabledIntegrations []string `mapstructure:"enabledIntegrations"`
		}{}
		applyConfig(name, &enabledIntegrations)

		integrations := GetIntegrations()
		// christ this code, will remove all but the 'enabledIntegrations' from our integrations list
		if len(enabledIntegrations.EnabledIntegrations) > 0 {
			for finished := false; finished == false; {
				finished = true
				for i, integration := range integrations {
					foundInEnabled := false
					for _, enabledIntegration := range enabledIntegrations.EnabledIntegrations {
						if enabledIntegration == integration.Identifier() {
							foundInEnabled = true
							break
						}
					}

					if foundInEnabled == false {
						integrations = append(integrations[:i], integrations[i+1:]...)
						finished = false
						break
					}
				}
			}
		}
		app := newApp(name, appDir, integrations)

		apps = append(apps, app)
	}

	return apps
}

type app struct {
	m           sync.RWMutex
	name        string
	appLocation string

	builds       map[string][]Build
	integrations []Integration

	bus *appbus
}

// NewApp will return a new app with the given name, the name should also be the directory name that the app will
// search for config data in
func newApp(name, appLocation string, integrations []Integration) App {
	app := &app{
		name:         name,
		appLocation:  appLocation,
		builds:       make(map[string][]Build),
		bus:          newAppBus(),
		integrations: integrations,
	}

	for _, integration := range integrations {
		integration.AttachToApp(app)
	}
	return app
}

// Name is the apps name
func (a *app) Name() string {
	if a == nil {
		return ""
	}

	return a.name
}

// GetAppLocation will return the app config location on disk
func (a *app) AppLocation() string {
	if a == nil {
		return ""
	}

	return a.appLocation
}

func (a *app) Shutdown() {
	if a == nil {
		return
	}
	for _, builds := range a.builds {
		for _, build := range builds {
			if build.HasStopped() == false {
				build.Stop()
			}
		}
	}
}

// Config will apply the config at namespace onto the given conf structure
// consider this like json.Unmarshal()
func (a *app) Config(integrationName string, conf interface{}) error {
	if a == nil {
		return errors.New("a is nil")
	}

	return applyIntegrationConfig(a.name, integrationName, conf)
}

// GlobalConfig will fill the interface s with config values taken from
// the global config informatio
func (a *app) GlobalConfig(conf interface{}) error {
	if a == nil {
		return errors.New("a is nil")
	}

	return applyConfig(a.name, conf)
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
	var appcfg struct {
		BuildRunner string `mapstructure:"buildRunner"`
	}
	applyConfig(a.Name(), &appcfg)

	config.BuildRunner = "build.sh"
	if appcfg.BuildRunner != "" {
		config.BuildRunner = appcfg.BuildRunner
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

	a.m.Lock()
	defer a.m.Unlock()
	config.Integrations = a.integrations

	build := newBuild(a, token, config)
	a.builds[group] = append(a.builds[group], build)

	errChan := make(chan error, 1)
	go func() {
		errChan <- build.Start()
	}()

	if err := <-errChan; err != nil {
		return "", err
	}

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

	return nil, errors.New("Couldn't find build")
}

func (a *app) GetBuildHistory(group string) []Build {
	return a.builds[group]
}

func (a *app) Loginfof(str string, args ...interface{}) {
	args = append([]interface{}{a.Name()}, args...)
	log := loginfof("(%s):"+str, args...)
	a.SendEvent(fmt.Sprintf("/log/app:%s/logtype:crit/%s", a.Name(), log))
}

func (a *app) Logwarnf(str string, args ...interface{}) {
	args = append([]interface{}{a.Name()}, args...)
	log := logwarnf("(%s):"+str, args...)
	a.SendEvent(fmt.Sprintf("/log/app:%s/logtype:warn/%s", a.Name(), log))
}

func (a *app) Logcritf(str string, args ...interface{}) {
	args = append([]interface{}{a.Name()}, args...)
	log := logcritf("(%s):"+str, args...)
	a.SendEvent(fmt.Sprintf("/log/app:%s/logtype:info/%s", a.Name(), log))
}
