package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/terminal"
	"github.com/watchly/ngbuild/core"
)

// Web ...
type Web struct {
	m sync.RWMutex

	apps   map[string]core.App
	builds map[string]core.Build

	logs  []string
	stats map[string]int
}

// NewWeb ...
func NewWeb() *Web {
	w := &Web{
		apps:   make(map[string]core.App),
		builds: make(map[string]core.Build),
		stats:  make(map[string]int),
	}

	http.HandleFunc("/web/", w.routeHTTP)

	fmt.Printf("Visit the webUI on %s/web/status\n", core.GetHTTPServerURL())
	return w
}

var (
	reBuildStatus = regexp.MustCompile(`\/web\/(?P<appname>[a-zA-Z0-9_-]+)\/(?P<buildtoken>[a-zA-Z0-9_-]+)(?:\/(?P<action>[a-zA-Z0-9_-]+))?`)
)

func (w *Web) routeHTTP(resp http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	switch {
	case path == "/web":
		w.status(resp, req)
	case path == "/web/status":
		w.status(resp, req)
	case strings.HasSuffix(path, ".json") && reBuildStatus.MatchString(strings.TrimSuffix(path, ".json")):
		w.asciinemaFormat(resp, req)
	case reBuildStatus.MatchString(path):
		w.buildStatus(resp, req)
	default:
		fmt.Println("no match: ", path)
		resp.WriteHeader(404)
	}

}

func (w *Web) status(resp http.ResponseWriter, req *http.Request) {
	w.m.RLock()
	defer w.m.RUnlock()

	output := `<html><head><title>NGBuild stats</title></head><body>`
	output += `<pre>`

	output += "Stats:\n"
	for key, value := range w.stats {
		output += fmt.Sprintf("\t%s: %d\n", key, value)
	}

	output += "\nLogs:\n"
	for i := len(w.logs) - 1; i > 0; i-- {
		log := w.logs[i]
		output += html.EscapeString(log) + "\n"
	}

	output += "\nNeil didn't make this look nicer yet"
	output += `</pre>`
	output += `</body></html>`

	resp.Write([]byte(output))
}

func (w *Web) cacheDir(appName, buildToken string) string {
	dir := filepath.Join(core.CacheDirectory(), "web", appName, buildToken)
	os.MkdirAll(dir, 0755)
	return dir
}

func (w *Web) rebuild(resp http.ResponseWriter, req *http.Request) {
	w.m.RLock()
	defer w.m.RUnlock()

	data, err := core.RegexpNamedGroupsMatch(reBuildStatus, req.URL.Path)
	if err != nil {
		return
	}

	appName := data["appname"]
	buildToken := data["buildtoken"]
	cacheDir := w.cacheDir(appName, buildToken)

	if app, ok := w.apps[appName]; ok {
		buildConfig, err := core.UnmarshalBuildConfig(filepath.Join(cacheDir, "buildconfig.json"))
		if err != nil {
			logwarnf("error deserializing build config: %s", err)
			resp.WriteHeader(502)
			return
		}

		token, err := app.NewBuild(buildConfig.Group, buildConfig)
		if err != nil {
			logcritf("error creating new build: %s", err)
			resp.WriteHeader(502)
			return
		}
		baseURL := fmt.Sprintf("/web/%s/%s/", appName, token)

		// I don't know how to do a redirect in this go api, all i have is http status and response writing
		output := fmt.Sprintf(`<html><head></head><body><a href="%s">click here</a></body></html>`, baseURL)
		resp.Write([]byte(output))
	} else {
		logwarnf("no app '%s' found", appName)
		resp.WriteHeader(404)
		return
	}

}

func (w *Web) asciinemaFormat(resp http.ResponseWriter, req *http.Request) {
	w.m.RLock()
	defer w.m.RUnlock()

	data, err := core.RegexpNamedGroupsMatch(reBuildStatus, req.URL.Path)
	if err != nil {
		return
	}

	appName := data["appname"]
	buildToken := data["buildtoken"]

	app := w.apps[appName]
	if app == nil {
		resp.WriteHeader(404)
		return
	}
	cacheDir := w.cacheDir(appName, buildToken)

	jsonData, err := ioutil.ReadFile(filepath.Join(cacheDir, "asciinema.json"))
	if err != nil {
		resp.WriteHeader(500)
		logcritf("Error reading %s: %s", filepath.Join(cacheDir, "asciinema.json"), err)
		return
	}

	_, err = resp.Write(jsonData)
	if err != nil {
		logwarnf("Couldn't write all to resp: %s", err)
	}
}

func (w *Web) buildStatus(resp http.ResponseWriter, req *http.Request) {
	w.m.RLock()
	defer w.m.RUnlock()

	data, err := core.RegexpNamedGroupsMatch(reBuildStatus, req.URL.Path)
	if err != nil {
		return
	}

	appName := data["appname"]
	buildToken := data["buildtoken"]

	baseURL := fmt.Sprintf("/web/%s/%s/", appName, buildToken)
	action := data["action"]

	if action == "rebuild" {
		w.rebuild(resp, req)
		return
	}

	app := w.apps[appName]
	if app == nil {
		resp.WriteHeader(404)
		return
	}

	cacheDir := w.cacheDir(appName, buildToken)

	buildConfig, err := os.Open(filepath.Join(cacheDir, "buildconfig.json"))
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't open buildconfig.json: %s", err)))
		return
	}

	stdout, err := os.Open(filepath.Join(cacheDir, "stdout.log"))
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't open stdout: %s", err)))
		return
	}

	stderr, err := os.Open(filepath.Join(cacheDir, "stderr.log"))
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't open stderr: %s", err)))
		return
	}

	buildConfigRaw, err := ioutil.ReadAll(buildConfig)
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't read buildconfig.json: %s", err)))
	}

	stdoutRaw, err := ioutil.ReadAll(stdout)
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't read stdout: %s", err)))
	}

	stderrRaw, err := ioutil.ReadAll(stderr)
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't read stderr: %s", err)))
	}

	config, err := core.UnmarshalBuildConfig(filepath.Join(cacheDir, "buildconfig.json"))
	if err != nil {
		resp.Write([]byte(fmt.Sprintf("Couldn't open buildconfig.json: %s", err)))
		return
	}

	stdoutHTML := terminal.Render(stdoutRaw)
	stderrHTML := terminal.Render(stderrRaw)

	output := `<html><head>
	<title>NGBuild build output</title>
	<link href="https://fonts.googleapis.com/css?family=Ubuntu|Ubuntu+Mono" rel="stylesheet">
	<link rel="stylesheet" type="text/css" href="http://axiom.sh/axiom.css" />
	<link rel="stylesheet" type="text/css" href="https://storage.googleapis.com/ngbuild/asciinema-player.css" />
	<link rel="stylesheet" type="text/css" href="https://storage.googleapis.com/ngbuild/terminal.css" />
	<style>
	@keyframes flicker {
	  0% {
		opacity: 0.53796;
	  }
	  5% {
		opacity: 0.13547;
	  }
	  10% {
		opacity: 0.63579;
	  }
	  15% {
		opacity: 0.24247;
	  }
	  20% {
		opacity: 0.99758;
	  }
	  25% {
		opacity: 0.73973;
	  }
	  30% {
		opacity: 0.87653;
	  }
	  35% {
		opacity: 0.2604;
	  }
	  40% {
		opacity: 0.10599;
	  }
	  45% {
		opacity: 0.92037;
	  }
	  50% {
		opacity: 0.52826;
	  }
	  55% {
		opacity: 0.5802;
	  }
	  60% {
		opacity: 0.171;
	  }
	  65% {
		opacity: 0.39806;
	  }
	  70% {
		opacity: 0.27816;
	  }
	  75% {
		opacity: 0.33932;
	  }
	  80% {
		opacity: 0.79819;
	  }
	  85% {
		opacity: 0.74343;
	  }
	  90% {
		opacity: 0.8599;
	  }
	  95% {
		opacity: 0.03005;
	  }
	  100% {
		opacity: 0.50583;
	  }
	}
	.crt {
		position: relative;
		display: inline-block;
		overflow: hidden;
		border: 1px solid #393938
	}
	.crt::after {
	  animation: flicker 0.15s infinite;
	  content: " ";
	  display: block;
	  position: absolute;
	  top: 0;
	  left: 0;
	  bottom: 0;
	  right: 0;
	  background: rgba(18, 16, 16, 0.1);
	  opacity: 0;
	  z-index: 2;
	  pointer-events: none;
	}
	.crt::before {
	  content: " ";
	  display: block;
	  position: absolute;
	  top: 0;
	  left: 0;
	  bottom: 0;
	  right: 0;
	  background: linear-gradient(rgba(18, 16, 16, 0) 50%, rgba(0, 0, 0, 0.25) 50%), linear-gradient(90deg, rgba(255, 0, 0, 0.06), rgba(0, 255, 0, 0.02), rgba(0, 0, 255, 0.06));
	  z-index: 2;
	  background-size: 100% 2px, 3px 100%;
	  pointer-events: none;
	}

	.asciinema-theme-axiom .asciinema-terminal {
	  color: #6EDB77;                    /* default text color */
	  background-color: #202224;
	  text-shadow: 0 0 3px #6EDB76;
	  font-family: "Ubuntu Mono";
	  font-size: 14px;
	  font-weight: 300;
	  border-color: #272822;
	  border-width: 0px;
	}
	.asciinema-player-wrapper {
		text-align: left !important;
	}
	.asciinema-theme-axiom .fg-bg {    /* inverse for default text color */
	  color: #2d2d2d;
	}
	.asciinema-theme-axiom .bg-fg {    /* inverse for terminal background color */
		background-color: #6EDB77;
		box-shadow: 0px 0px 3px #6EDB77;
		margin-left: 2px !important;
		margin-right: 2px !important;
	}

	.asciinema-theme-axiom .fg-0 {
		color: #2d2d2d;
	}
	.asciinema-theme-axiom .bg-0 {
	  	background-color: #2d2d2d;
	}
	.asciinema-theme-axiom .fg-1 {
		color: #f2777a;
	}
	.asciinema-theme-axiom .bg-1 {
		background-color: #f2777a;
	}
	.asciinema-theme-axiom .fg-2 {
		color: #99cc99;
	}
	.asciinema-theme-axiom .bg-2 {
		background-color: #99cc99;
	}
	.asciinema-theme-axiom .fg-3 {
		color: #ffcc66;
	}
	.asciinema-theme-axiom .bg-3 {
		background-color: #ffcc66;
	}
	.asciinema-theme-axiom .fg-4 {
		color: #6699cc;
	}
	.asciinema-theme-axiom .bg-4 {
		background-color: #6699cc;
	}
	.asciinema-theme-axiom .fg-5 {
		color: #cc99cc;
	}
	.asciinema-theme-axiom .bg-5 {
		background-color: #cc99cc;
	}
	.asciinema-theme-axiom .fg-6 {
		color: #66cccc;
	}
	.asciinema-theme-axiom .bg-6 {
		background-color: #66cccc;
	}
	.asciinema-theme-axiom .fg-7 {
		color: #d3d0c8;
	}
	.asciinema-theme-axiom .bg-7 {
		background-color: #d3d0c8;
	}
	.asciinema-theme-axiom .fg-8 {
		color: #747369;
	}
	.asciinema-theme-axiom .bg-8 {
		background-color: #747369;
	}
	.asciinema-theme-axiom .fg-9 {
		color: #f2777a;
	}
	.asciinema-theme-axiom .bg-9 {
		background-color: #f2777a;
	}
	.asciinema-theme-axiom .fg-10 {
		color: #99cc99;
	}
	.asciinema-theme-axiom .bg-10 {
		background-color: #99cc99;
	}
	.asciinema-theme-axiom .fg-11 {
		color: #ffcc66;
	}
	.asciinema-theme-axiom .bg-11 {
		background-color: #ffcc66;
	}
	.asciinema-theme-axiom .fg-12 {
		color: #6699cc;
	}
	.asciinema-theme-axiom .bg-12 {
		background-color: #6699cc;
	}
	.asciinema-theme-axiom .fg-13 {
		color: #cc99cc;
	}
	.asciinema-theme-axiom .bg-13 {
		background-color: #cc99cc;
	}
	.asciinema-theme-axiom .fg-14 {
		color: #66cccc;
	}
	.asciinema-theme-axiom .bg-14 {
		background-color: #66cccc;
	}
	.asciinema-theme-axiom .fg-15 {
		color: #f2f0ec;
	}
	.asciinema-theme-axiom .bg-15 {
		background-color: #f2f0ec;
	}
	.asciinema-theme-axiom .fg-8,
	.asciinema-theme-axiom .fg-9,
	.asciinema-theme-axiom .fg-10,
	.asciinema-theme-axiom .fg-11,
	.asciinema-theme-axiom .fg-12,
	.asciinema-theme-axiom .fg-13,
	.asciinema-theme-axiom .fg-14,
	.asciinema-theme-axiom .fg-15 {
		font-weight: bold;
	}
	h1 {
		font-family: Ubuntu;
		font-size: 23px;
		font-style: normal;
		font-variant: normal;
		font-weight: 400;
		line-height: 23px;
	}
	h3 {
		font-family: Ubuntu;
		font-size: 17px;
		font-style: normal;
		font-variant: normal;
		font-weight: 400;
		line-height: 23px;
	}
	p {
		font-family: Ubuntu;
		font-size: 14px;
		font-style: normal;
		font-variant: normal;
		font-weight: 400;
		line-height: 23px;
	}
	blockquote {
		font-family: Ubuntu;
		font-size: 17px;
		font-style: normal;
		font-variant: normal;
		font-weight: 400;
		line-height: 23px;
	}
	pre {
		font-family: Ubuntu;
		font-size: 11px;
		font-style: normal;
		font-variant: normal;
		font-weight: 400;
		line-height: 15.7143px;
		background: #2d2d2d;
		color: #cccccc;
		padding: 0.5em;
		width: 100%;
	}
	</style>
	<link rel="stylesheet" href="//cdnjs.cloudflare.com/ajax/libs/highlight.js/9.7.0/styles/tomorrow-night-eighties.min.css">
	<script src="//cdnjs.cloudflare.com/ajax/libs/highlight.js/9.7.0/highlight.min.js"></script>
	<script>hljs.initHighlightingOnLoad();</script>
	</head><body>`

	output += `<h1>`
	output += fmt.Sprintf(`<a href="%s">%s</a>`, config.URL, config.Title)
	output += fmt.Sprintf(`<small> [<a href="%s/rebuild">rebuild</a>]</small>`, baseURL)
	output += `</h1>`

	output += "<H3>Replay:</H3>"
	output += fmt.Sprintf(`<div class="crt"><asciinema-player src="%s.json" theme="axiom" autoplay="yes please" speed=1></asciinema-player></div>`, baseURL)

	output += "<h3>Stdout:</h3>"
	output += (string)(stdoutHTML)

	output += "<h3>Stderr:</h3>"
	output += (string)(stderrHTML)

	output += "<h3>BuildConfig:</h3>"
	output += `<pre><code class="json">`
	output += string(buildConfigRaw) + "\n"
	output += `</code></pre>`

	output += "\nNeil didn't make this look nicer yet"
	output += `<script src="https://storage.googleapis.com/ngbuild/asciinema-player.js"></script></body></html>`
	resp.Write([]byte(output))
}

type asciinema struct {
	Version  int             `json:"version"`
	Width    int             `json:"width"`
	Height   int             `json:"height"`
	Duration float64         `json:"duration"`
	Title    string          `json:"title"`
	Stdout   [][]interface{} `json:"stdout"`
}

func writeAsciinemaTo(path, title, buildRunner string, stdout io.Reader, stderr io.Reader) {
	currentAsciinema := asciinema{
		Version: 1,
		Width:   120,
		Height:  30,
		Title:   title,
	}

	// first of all we want to pre-fill our stdout with some faked data to say ./build.sh
	currentAsciinema.Stdout = append(currentAsciinema.Stdout, []interface{}{
		0.0,
		fmt.Sprintf("[%s]ngbuild@watchmen $ ", time.Now().UTC().Format("15:04:05")),
	})

	buildRunner = "./" + buildRunner
	for i := range buildRunner {
		text := string(buildRunner[i])
		if i == len(buildRunner)-1 {
			text += "\n"
		}

		currentAsciinema.Stdout = append(currentAsciinema.Stdout, []interface{}{
			(rand.Float64() * 0.1) + 0.1,
			string(text),
		})
	}

	startTime := time.Now().UTC()

	readAll := func(data chan<- []byte, reader io.Reader) {
		basebuf := [1024]byte{}
		for {
			n, err := reader.Read(basebuf[:])
			if n < 1 || err != nil {
				break
			}
			data <- basebuf[:n]
		}
		close(data)
	}

	stdoutC := make(chan []byte, 1)
	stderrC := make(chan []byte, 1)

	go readAll(stdoutC, stdout)
	go readAll(stderrC, stderr)

	stderrClosed := false
	stdoutClosed := false

	lastOutputTime := time.Now().UTC()
	for stderrClosed == false && stdoutClosed == false {
		select {
		case data, ok := <-stdoutC:
			if ok == false {
				stdoutClosed = true
			} else {
				currentAsciinema.Stdout = append(currentAsciinema.Stdout, []interface{}{
					time.Now().UTC().Sub(lastOutputTime).Seconds(),
					string(data),
				})
				lastOutputTime = time.Now().UTC()
			}
		case data, ok := <-stderrC:
			if ok == false {
				stderrClosed = true
			} else {
				currentAsciinema.Stdout = append(currentAsciinema.Stdout, []interface{}{
					time.Now().UTC().Sub(lastOutputTime).Seconds(),
					string(data),
				})
				lastOutputTime = time.Now().UTC()
			}
		}
		currentAsciinema.Duration = time.Now().UTC().
			Add(time.Second * 15).
			Sub(startTime).Seconds()

		// work around a bug in the current player, add an extra line before writing, then remove it
		currentAsciinema.Stdout = append(currentAsciinema.Stdout, []interface{}{
			(time.Now().UTC().Sub(lastOutputTime) + (time.Second * 2)).Seconds(),
			string("[33m[end of message...]"),
		})
		data, err := json.MarshalIndent(currentAsciinema, "", "  ")
		currentAsciinema.Stdout = currentAsciinema.Stdout[:len(currentAsciinema.Stdout)-1]
		if err != nil {
			logcritf("Could not write data to asciinema format: %s", err)
			continue
		}

		err = ioutil.WriteFile(path, data, 0666)
		if err != nil {
			logcritf("Could not write data to %s: %s", path, err)
		}
	}
}

func writeAll(writer io.Writer, buf []byte) error {
	for len(buf) > 0 {
		n, err := writer.Write(buf)
		buf = buf[n:]
		if err != nil {
			return err
		}
	}

	return nil
}

func writeTo(path string, reader io.Reader) {
	file, err := os.Create(path)
	file.Close()

	for err == nil {
		var readBuf [1024 * 4]byte
		var n int
		n, err = reader.Read(readBuf[:])

		file, oerr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if oerr != nil {
			logcritf("error opening %s: %s", path, err)
			return
		}

		if werr := writeAll(file, readBuf[:n]); werr != nil {
			err = werr
		}

		file.Close()
	}

	if err != nil && err != io.EOF {
		logcritf("error writing %s: %s", path, err)
	}

}

func (w *Web) startMonitorBuild(data map[string]string) {
	w.m.Lock()
	defer w.m.Unlock()
	loginfof("Starting monitoring of %+v", data)

	appName := data["app"]
	token := data["token"]

	app := w.apps[appName]
	if app == nil {
		logcritf("no app for %s", appName)
		return
	}

	build, err := app.GetBuild(token)
	if err != nil {
		logcritf("No build for %s", token)
		return
	}
	w.builds[token] = build
	build.Ref()

	cacheDir := w.cacheDir(appName, token)

	serializedConfig, err := build.Config().Marshal()
	if err != nil {
		logcritf("Couldn't serialize config: %s", err)
		return
	}

	stdout, err := build.Stdout()
	if err != nil {
		logcritf("Couldn't get build stdout: %s", err)
		return
	}

	stderr, err := build.Stderr()
	if err != nil {
		logcritf("Couldn't get build stderr: %s", err)
		return
	}

	ioutil.WriteFile(filepath.Join(cacheDir, "buildconfig.json"), serializedConfig, 0664)

	go writeTo(filepath.Join(cacheDir, "stdout.log"), stdout)
	go writeTo(filepath.Join(cacheDir, "stderr.log"), stderr)

	// get new stdout/errs for asciinema
	stdout, err = build.Stdout()
	if err != nil {
		logcritf("Couldn't get build stdout: %s", err)
		return
	}

	stderr, err = build.Stderr()
	if err != nil {
		logcritf("Couldn't get build stderr: %s", err)
		return
	}
	go writeAsciinemaTo(filepath.Join(cacheDir, "asciinema.json"), fmt.Sprintf("%s::%s", appName, token), build.Config().BuildRunner, stdout, stderr)

	w.stats["tracked builds total"]++
	w.stats[fmt.Sprintf("(%s)current tracked builds", appName)] = len(w.builds)
}

func (w *Web) endMonitorBuild(data map[string]string) {
	w.m.Lock()
	defer w.m.Unlock()

	token := data["token"]
	appName := data["app"]
	if build, ok := w.builds[token]; ok {
		build.Unref()
	}
	delete(w.builds, token)

	w.stats[fmt.Sprintf("(%s)current tracked builds", appName)] = len(w.builds)
}

func (w *Web) logger(data map[string]string) {
	w.m.Lock()
	defer w.m.Unlock()

	logTime := time.Now().Format("15:04:05")
	logType := data["logtype"]
	logMessage := data["logmessage"]

	if logType == "" || logMessage == "" {
		logcritf("got broken log message")
		return
	}

	w.logs = append(w.logs, fmt.Sprintf("[%s]%s: %s", logTime, logType, logMessage))
	if len(w.logs) > 1000 {
		w.logs = w.logs[len(w.logs)-1000:]
	}
}

// Identifier ...
func (w *Web) Identifier() string { return "Web" }

// IsProvider ...
func (w *Web) IsProvider(string) bool { return false }

//ProvideFor ...
func (w *Web) ProvideFor(*core.BuildConfig, string) error { return errors.New("Can not provide") }

//AttachToApp ...
func (w *Web) AttachToApp(app core.App) error {
	w.m.Lock()
	defer w.m.Unlock()

	w.apps[app.Name()] = app
	app.Listen(core.SignalBuildStarted, w.startMonitorBuild)
	app.Listen(core.SignalBuildComplete, w.endMonitorBuild)
	app.Listen(core.EventCoreLog, w.logger)
	return nil
}

//Shutdown ...
func (w *Web) Shutdown() {}

func loginfof(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("web-info: "+str+"\n", args...)
	fmt.Println(ret)
	return ret
}

func logwarnf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("web-warn: "+str+"\n", args...)
	fmt.Println(ret)
	return ret
}

func logcritf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("web-crit: "+str+"\n", args...)
	fmt.Println(ret)
	return ret
}
