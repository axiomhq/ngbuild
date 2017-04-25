package core

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var reProcZombied = regexp.MustCompile(`State:\s*Z\s\(zombie\)`)

// hasPIDExited will return true if the pid has zombied/exited
func hasPIDExited(pid int) bool {
	pidDir := filepath.Join("/proc", fmt.Sprintf("%d", pid))
	if exists, _ := Exists(pidDir); exists == false {
		return true
	}

	status, err := ioutil.ReadFile(filepath.Join(pidDir, "status"))
	if err != nil {
		println("Error reading", pidDir+"/status", err)
		return true
	}

	return reProcZombied.Match(status)
}

type buildState uint32

func (state *buildState) HasStarted() bool {
	return atomic.LoadUint32((*uint32)(state))&(^uint32(0)) != 0
}
func (state *buildState) HasStopped() bool {
	return atomic.LoadUint32((*uint32)(state))&(uint32)(buildStateFinished) != 0
}
func (state *buildState) SetBuildState(newState buildState) {
	atomic.StoreUint32((*uint32)(state), (uint32)(newState))
}
func (state *buildState) String() string {
	switch (buildState)(atomic.LoadUint32((*uint32)(state))) {
	case buildStateNull:
		return "Null"
	case buildStateStarted:
		return "Started"
	case buildStateWaitingForProvisioning:
		return "Waiting for provisioning"
	case buildStateFinished:
		return "Finished"
	default:
		return "unknown"
	}
}

// buildStates
const (
	buildStateNull                   buildState = iota
	buildStateWaitingForProvisioning            = 1 << iota
	buildStateStarted
	buildStateFinished
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
	m sync.RWMutex

	config *BuildConfig
	token  string

	parentApp App

	ref refcount

	cmd            *exec.Cmd
	stdpipes       *stdpipes
	buildStartTime time.Time
	buildEndTime   time.Time

	buildDirectory string
	state          buildState
	exitCode       int

	artifacts map[string][]string
}

func newBuild(app App, token string, config *BuildConfig) *build {
	return &build{
		parentApp: app,
		token:     token,
		config:    config,
		artifacts: make(map[string][]string),
	}
}

func (b *build) HasStarted() bool {
	if b == nil {
		return false
	}
	return b.state.HasStarted()
}

func (b *build) HasStopped() bool {
	if b == nil {
		return true
	}
	return b.state.HasStopped()
}

func (b *build) Config() *BuildConfig {
	if b == nil {
		return &BuildConfig{m: &sync.RWMutex{}}
	}

	return b.config
}

func checkConfig(config *BuildConfig) error {
	if config == nil {
		return errors.New("config is nil")
	}

	if config.Title == "" {
		return errors.New("Title is required")
	}

	if config.URL == "" {
		return errors.New("URL is required")
	}

	if config.HeadRepo == "" {
		return errors.New("URL is required")
	}

	if config.HeadHash == "" {
		return errors.New("URL is required")
	}

	if config.BaseRepo == "" {
		return errors.New("MergeRepo is required")
	}

	if config.BaseHash == "" {
		return errors.New("MergeHash is required")
	}

	if config.Group == "" {
		return errors.New("Group is required")
	}

	if config.BuildRunner == "" {
		return errors.New("BuildRunner is required")
	}
	return nil
}

func (b *build) loginfof(str string, args ...interface{}) {
	b.parentApp.Loginfof(fmt.Sprintf("(%s): %s", b.Token(), str), args...)
}

func (b *build) logwarnf(str string, args ...interface{}) {
	b.parentApp.Logwarnf(fmt.Sprintf("(%s): %s", b.Token(), str), args...)
}

func (b *build) logcritf(str string, args ...interface{}) {
	b.parentApp.Logcritf(fmt.Sprintf("(%s): %s", b.Token(), str), args...)
}

// provisionDirectory will return an empty unique directory to work in
func provisionDirectory(basedir string) (string, error) {
	if basedir == "" {
		basedir = os.TempDir()
	}

	os.MkdirAll(basedir, 0766)
	return ioutil.TempDir(basedir, "ngbuild-workspace-")
}

func cleanupDirectory(directory string) error {
	return os.RemoveAll(directory)
}

func (b *build) provisionBuildIntoDirectory(config *BuildConfig, workdir string) error {
	provisioned := false
	for _, integration := range config.Integrations {
		if integration.IsProvider(config.HeadRepo) && integration.IsProvider(config.BaseRepo) {
			if err := integration.ProvideFor(config, workdir); err != nil {
				b.logcritf("(%s) Error providing for build: %s", integration.Identifier(), err)
				continue
			}

			provisioned = true
			break
		}
	}

	if provisioned == false {
		return errors.New("Could not provision with any loaded integration")
	}

	return nil
}

func (b *build) runBuildSync(config BuildConfig) error {
	defer func() { b.state = buildStateFinished }()

	b.loginfof("provisioning")
	var appConfig struct {
		BuildLocation string `mapstructure:"buildLocation"`
	}
	b.parentApp.GlobalConfig(&appConfig)

	provisionedDirectory, err := provisionDirectory(appConfig.BuildLocation)
	if err != nil {
		return err
	}

	b.m.Lock()

	b.buildStartTime = time.Now().UTC()
	b.buildDirectory = provisionedDirectory

	cmd := exec.Command(filepath.Join(provisionedDirectory, config.BuildRunner))
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Dir = provisionedDirectory

	// gets child processes killed, probably linux only
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	b.cmd = cmd

	b.m.Unlock()

	err = b.provisionBuildIntoDirectory(&config, provisionedDirectory)
	if err != nil {
		b.buildFinished(501)
		return err
	}

	b.loginfof("running build: %s", filepath.Join(provisionedDirectory, config.BuildRunner))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	b.stdpipes = newStdpipes(stdout, stderr)
	err = cmd.Start()
	b.parentApp.SendEvent(fmt.Sprintf("/build/app:%s/started/token:%s", b.parentApp.Name(), b.Token()))

	if err != nil {
		cmd.Process.Kill()
		return err
	}
	b.loginfof("Command started, pid=%d", cmd.Process.Pid)
	b.state = buildStateStarted

runSyncLoop:
	for {
		select {
		case <-b.stdpipes.Done:
			b.loginfof("Build exited, waiting...")
			err = cmd.Wait() // stdout/err have finished, just need to wait for the process to exit
			if err != nil {
				b.logwarnf("Build exited with non zero error code")
				b.buildFinished(1)
				return err
			}
			break runSyncLoop

		case <-time.After(config.Deadline):
			b.logwarnf("Cancelling build as deadline reached")
			err := b.Stop()
			if err != nil {
				b.logcritf("Couldn't stop build: %s", err)
				b.buildFinished(500)
				return err
			}
		case <-time.After(time.Second * 5):
			// every so often we need to check that the pid is still going, to avoid situations where
			// the stderr/out pipes are still open, but the pid has died
			// this is primaraly a problem with nodejs as it allows nodejs programs
			// to not flush their stdout/err before exiting, leaving stdout/err open forever
			if hasPIDExited(cmd.Process.Pid) {
				b.logcritf("Process exited but stdpipes are still open(zombied): %d", cmd.Process.Pid)
				b.stdpipes.Close()
			}
		}
	}

	b.buildFinished(0)

	b.loginfof("Build finished")
	return nil

}

func (b *build) buildFinished(code int) {
	b.m.Lock()
	defer b.m.Unlock()
	b.buildEndTime = time.Now().UTC()
	b.exitCode = code
	b.cmd = nil
}

// Start will start the given build, it will error with ErrAlreadyStarted if the build is already running
func (b *build) Start() error {
	if b == nil {
		return errors.New("b is nil")
	}

	b.m.Lock()
	defer b.m.Unlock()
	if b.state.HasStarted() || b.state.HasStopped() {
		return ErrProcessAlreadyStarted
	}

	b.state = buildStateWaitingForProvisioning
	b.parentApp.SendEvent(fmt.Sprintf("/build/app:%s/provisioning/token:%s", b.parentApp.Name(), b.Token()))

	var config BuildConfig
	config = *b.config

	if config.Deadline < time.Duration(time.Millisecond) {
		b.logwarnf("deadline not set in config, defaulting to 30 minutes")
		config.Deadline = time.Minute * 30
	}

	go func() {
		err := b.runBuildSync(config)
		if err != nil {
			b.logwarnf("Build exited with error: %s", err)
		}

		// move artifacts over to perminent storage
		var cfg struct {
			ArtifactsLocation string `mapstructure:"artifactsLocation"`
		}
		perminentStorageDir := "/tmp/ngbuildartifacts/"

		if err := b.parentApp.GlobalConfig(&cfg); err == nil {
			perminentStorageDir = cfg.ArtifactsLocation
		}

		artifactDir := filepath.Join(perminentStorageDir, b.Token())
		if err := os.MkdirAll(artifactDir, 0766); err != nil {
			b.logcritf("Couldn't create artifact directory %s: %s", artifactDir, err)
		} else {
			b.loginfof("this message stops a linter error")
			// TODO, all this stuff, needs things defined in config
			/*
				for _, artifact := range b.parentApp.GetArtifactList() {
					// copy file to artifacts directory
				}
			*/
		}

		b.parentApp.SendEvent(fmt.Sprintf("/build/app:%s/complete/token:%s", b.parentApp.Name(), b.Token()))
	}()

	return nil
}

// Stop will stop the given build, it will error with ErrAlreadyStopped if the build has finished
func (b *build) Stop() error {
	if b == nil {
		return errors.New("b is nil")
	}

	b.m.RLock()
	if b.state.HasStarted() == false || b.state.HasStopped() {
		b.m.RUnlock()
		b.logcritf("Stop called on non started/already stopped build")
		return ErrProcessAlreadyFinished
	}
	b.m.RUnlock()

	b.m.Lock()
	defer b.m.Unlock()
	if b.cmd == nil || b.cmd.Process == nil {
		b.logcritf("unknown process asked to stop")
		b.state.SetBuildState(buildStateFinished)
		b.exitCode = 505
		b.parentApp.SendEvent(fmt.Sprintf("/build/app:%s/complete/token:%s", b.parentApp.Name(), b.Token()))
		if b.stdpipes != nil {
			b.stdpipes.Done <- struct{}{}
		}
	} else {
		pgid, err := syscall.Getpgid(b.cmd.Process.Pid)
		if err != nil {
			return err
		}

		if err := syscall.Kill(-pgid, 15); err != nil {
			return err
		}
	}
	b.loginfof("Stopped build")

	return nil
}

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
		b.m.Lock()
		defer b.m.Unlock()
		if b.buildDirectory != "" {
			os.RemoveAll(b.buildDirectory)
			b.buildDirectory = ""
		}
	}
}

// NewBuild will construct a new Build using this build as a base,
// it is essentally a retry system
func (b *build) NewBuild() (token string, err error) {
	config := *b.config
	return b.parentApp.NewBuild(b.Group(), &config)
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

	if b.HasStopped() {
		return b.exitCode, nil
	}

	return 0, ErrProcessNotFinished
}

// Artifact will return a list of filepaths for the given artifact name
func (b *build) Artifact(name string) []string {
	if b == nil {
		return nil
	}

	return b.artifacts[name]
}

// BuildTime will return how long the build took, will return 0 if build hasn't started yet
func (b *build) BuildTime() time.Duration {
	if b == nil || b.state.HasStopped() == false {
		return time.Duration(0)
	}

	return b.buildEndTime.Sub(b.buildStartTime)
}

// History will return an array of previous Build's in this builds group
func (b *build) History() []Build {
	if b == nil {
		return nil
	}

	history := b.parentApp.GetBuildHistory(b.Group())
	for i, build := range history {
		if build.Token() == b.Token() {
			return history[:i+1]
		}
	}

	return nil
}

// WebStatusURL will return a url that can be used to find the status of this build request
func (b *build) WebStatusURL() string {
	return fmt.Sprintf("%s/web/%s/%s/", GetHTTPServerURL(), b.parentApp.Name(), b.Token())
}
