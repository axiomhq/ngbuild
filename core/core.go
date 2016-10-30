package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Errors
var (
	ErrProcessNotFinished     = errors.New("Error: Process not finished")
	ErrProcessNotStarted      = errors.New("Error: process not started yet")
	ErrProcessAlreadyFinished = errors.New("Error: process already finished")
	ErrProcessAlreadyStarted  = errors.New("Error: process already started")
)

// AppBus signal
const (
	appnameRE = `app:(?P<app>\w+)`
	tokenRE   = `token:(?P<token>\w+)`

	SignalBuildComplete = `/build/` + appnameRE + `/complete/` + tokenRE + `$`
	SignalBuildStarted  = `/build/` + appnameRE + `/started/` + tokenRE + `$`
	EventCoreLog        = `/log/` + appnameRE + `/logtype:(?P<logtype>\w+)/log(?P<logmessage>.*)$`
)

type (
	// EventHandler can be used to cancel an event added with Listen
	EventHandler uint32

	// App is defined by $NGBUILD_WORKSPACE/apps/$appname/config.yaml
	// Apps will define different builds, for different projects or different kinds of builds or whatever
	// Config has $NGBUILD_WORKSPACE/config.yaml as a parent and then applies
	// $NGBUILD_WORKSPACE/apps/$appname/config.json onto it
	// Everything App should be thread safe as it will be called from many goroutines
	App interface {
		Name() string
		Config(namespace string, conf interface{}) error
		GlobalConfig(conf interface{}) error
		Shutdown()

		// AppLocation will return the physical filesystem location of this app
		AppLocation() string

		// SendEvent is a dispatcher, it will send this string across all the apps integrations and also Core
		SendEvent(event string)

		// Listen will provide a channel to select on for a given regular expression
		// returned map is the captured groups and values
		// the returned EventHandler can be used to cancel a listener
		Listen(event string, listener func(map[string]string)) EventHandler

		RemoveEventHandler(EventHandler)

		// NewBuild will be used by github and the like to create new builds for this app whenever they deem so
		NewBuild(group string, config *BuildConfig) (token string, err error)
		GetBuild(token string) (Build, error)
		GetBuildHistory(group string) []Build
	}

	// BuildConfig describes a build, heavily in favour of github/git at the moment
	//
	BuildConfig struct {
		m        sync.RWMutex
		metadata map[string]string

		// Required block
		Title string
		URL   string

		HeadRepo   string
		HeadBranch string
		HeadHash   string

		BaseRepo   string
		BaseBranch string
		BaseHash   string

		Group string

		Integrations []Integration

		// Not required block

		// Should be an executable of some sort, if not set, set by app.NewBuild
		BuildRunner string
		Deadline    time.Duration
	}

	// Build interface
	// when a build finishes it will announce on the app event bus as
	// /build/complete/$token
	Build interface {
		Start() error
		Stop() error

		Ref()
		Unref()

		Token() string
		Group() string

		HasStarted() bool
		HasStopped() bool

		// NewBuild() Will be used by slack and the like, /rebuild <token> or buttons or whatever will just lookup the build
		// and call NewBuild() to run the exact same build again
		NewBuild() (token string, err error)

		// Stdout/Stderr give you what you would expect, io.Reader's that will let you access the entire stdout/err output
		Stdout() (io.Reader, error)
		Stderr() (io.Reader, error)

		// ExitCode returns 0, ErrProcessNotFinished
		ExitCode() (int, error)

		// Artifact will return a series of filepaths, artifacts are part of the app config in a map[string][]string format
		// that is, a given named artifact can have many paths associated with it
		// this should be used by say, code coverage tools to generate coverage reports by grabbing artifacts listed here
		Artifact(name string) []string

		BuildTime() time.Duration

		History() []Build

		Config() *BuildConfig

		WebStatusURL() string
	}

	// Integration is an interface that integrations should provide
	Integration interface {
		// Identifier should return what integration this is, "github", "slack", that kind of thing
		Identifier() string

		// IsProvider will when given a string, indicate whether this integration can provide for it
		// strings would be something like
		// http://github.com/foo/bar, or gitlab or git@github.com:foo/bar.git
		IsProvider(string) bool

		// ProvideFor will be called on the integration when it is expected to provide for a build
		// generally this means checkout git repositories into the given directory
		ProvideFor(c *BuildConfig, directory string) error

		// AttachToApp will order the ingeration to do whatever it does, with the given app.
		AttachToApp(App) error

		// Shutdown will be called whenever we are closing, anything the integration needs to do, it has to do syncronously
		Shutdown()
	}
)

var integrations = []Integration{}

// RegisterIntegration will register your integration with core
func RegisterIntegration(integration Integration) {
	integrations = append(integrations, integration)
}

func getNGBuildDirectory() (string, error) {
	probeLocations := []string{}

	if overrideDir, ok := os.LookupEnv("NGBUILD_DIRECTORY"); ok {
		probeLocations = append(probeLocations, overrideDir)
	}

	if cwd, err := os.Getwd(); err == nil {
		probeLocations = append(probeLocations, cwd)
	}

	if user, err := user.Current(); err == nil {
		probeLocations = append(probeLocations, user.HomeDir)
	}

	probeLocations = append(probeLocations, "/etc/ngbuild/")

	for _, probeLocation := range probeLocations {
		if exists, _ := Exists(filepath.Join(probeLocation, "ngbuild.json")); exists == false {
			continue
		} else if exists, _ = Exists(filepath.Join(probeLocation, "apps")); exists == false {
			continue
		}

		// we have a valid location, it has an ngbuild.conf and an apps directory
		return probeLocation, nil
	}
	return "", errors.New("no app location detected")
}

var (
	cacheLock      sync.RWMutex
	cacheSyncLock  sync.Mutex
	cacheSyncCheck uint64
	cache          = make(map[string]string)
	cacheInited    uint64
)

// StoreCache will store the given data perminately on disk, it can be retrieved  with GetCache()
func StoreCache(key, data string) {
	cacheLock.Lock()
	cache[key] = data
	cacheLock.Unlock()

	// sync cache to disk from here out
	if atomic.LoadUint64(&cacheSyncCheck) > 0 {
		return
	}

	cacheSyncLock.Lock()
	atomic.StoreUint64(&cacheSyncCheck, 1)
	defer atomic.StoreUint64(&cacheSyncCheck, 0)
	defer cacheSyncLock.Unlock()

	cfgCache := struct {
		CacheDirectory string `mapstructure:"cacheDirectory"`
	}{}
	applyConfig("", &cfgCache)

	os.MkdirAll(cfgCache.CacheDirectory, 0755)

	cacheLock.RLock()
	defer cacheLock.RUnlock()
	if data, err := json.Marshal(cache); err != nil {
		logcritf("Unable to serialize cache to disk: %s", err)
	} else if err := ioutil.WriteFile(filepath.Join(cfgCache.CacheDirectory, "ngbuild.cache"), data, 0644); err != nil {
		logcritf("Unable to serialize cache to disk: %s", err)
	}

	return
}

func initCache() {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	if atomic.LoadUint64(&cacheInited) > 0 {
		return
	}

	cacheSyncLock.Lock()
	defer atomic.StoreUint64(&cacheInited, 1)
	defer cacheSyncLock.Unlock()

	cfgCache := struct {
		CacheDirectory string `mapstructure:"cacheDirectory"`
	}{}
	applyConfig("", &cfgCache)

	if data, err := ioutil.ReadFile(filepath.Join(cfgCache.CacheDirectory, "ngbuild.cache")); err != nil {
		logcritf("Unable to read cached data: %s", err)
	} else if err := json.Unmarshal(data, &cache); err != nil {
		logcritf("Unable to read cached data: %s", err)
	}
}

// GetCache will retrieve data from the global cache, this may block longer than you expect
func GetCache(key string) string {
	if atomic.LoadUint64(&cacheInited) < 1 {
		initCache()
	}

	cacheLock.RLock()
	defer cacheLock.RUnlock()
	return cache[key]
}

// StartHTTPServer will start the core http server that can be used by integrations
func StartHTTPServer() chan struct{} {
	httpDone := make(chan struct{}, 1)
	go func() {
		cfg := struct {
			HTTPListenPort string `mapstructure:"httpListenPort"`
		}{}
		applyConfig("", &cfg)

		loginfof("Starting http listen server on :%s", cfg.HTTPListenPort)
		if err := http.ListenAndServe(":"+cfg.HTTPListenPort, nil); err != nil {
			fmt.Println(err.Error())
		}
		httpDone <- struct{}{}
	}()
	return httpDone
}

// GetHTTPServerURL will return the base url that the http server is listening on
func GetHTTPServerURL() string {
	cfg := struct {
		HTTPListenPort string `mapstructure:"httpListenPort"`
		Hostname       string `mapstructure:"hostname"`
	}{}
	applyConfig("", &cfg)

	if cfg.HTTPListenPort == "80" {
		return fmt.Sprintf("http://%s", cfg.Hostname)
	} else if cfg.HTTPListenPort == "443" {
		return fmt.Sprintf("https://%s", cfg.Hostname)
	} else {
		return fmt.Sprintf("http://%s:%s", cfg.Hostname, cfg.HTTPListenPort)
	}
}

func loginfof(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("info: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}

func logwarnf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("warn: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}

func logcritf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("crit: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}
