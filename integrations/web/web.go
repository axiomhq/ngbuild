package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

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
	switch {
	case req.URL.Path == "/web":
		w.status(resp, req)
	case req.URL.Path == "/web/status":
		w.status(resp, req)
	case reBuildStatus.MatchString(req.URL.Path):
		w.buildStatus(resp, req)
	default:
		fmt.Println("no match: ", req.URL.Path)
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

func (w *Web) buildStatus(resp http.ResponseWriter, req *http.Request) {
	w.m.RLock()
	defer w.m.RUnlock()

	data, err := core.RegexpNamedGroupsMatch(reBuildStatus, req.URL.Path)
	if err != nil {
		return
	}

	appName := data["appname"]
	buildToken := data["buildtoken"]
	//action := data["action"]

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

	config := core.BuildConfig{}
	json.Unmarshal(buildConfigRaw, &config)

	output := `<html><head>
	<title>NGBuild build output</title>
	<link rel="stylesheet" type="text/css" href="//fonts.googleapis.com/css?family=Ubuntu" />
	<style>
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
			width: 80%;
		}	
	</style>
	<link rel="stylesheet" href="//cdnjs.cloudflare.com/ajax/libs/highlight.js/9.7.0/styles/tomorrow-night-eighties.min.css">
	<script src="//cdnjs.cloudflare.com/ajax/libs/highlight.js/9.7.0/highlight.min.js"></script>
	<script>hljs.initHighlightingOnLoad();</script>
	</head><body>`
	output += fmt.Sprintf(`<h1><a href="%s">%s</a></h1>`, config.URL, config.Title)
	output += "<h3>Stdout:</h3>"
	output += `<pre><code class="nohighlight">`
	output += html.EscapeString((string)(stdoutRaw))
	output += `</code></pre>`

	output += "<h3>Stderr:</h3>"
	output += `<pre><code class="nohighlight">`
	output += html.EscapeString((string)(stderrRaw))
	output += `</code></pre>`

	output += "<h3>BuildConfig:</h3>"
	output += `<pre><code class="json">`
	output += string(buildConfigRaw) + "\n"
	output += `</code></pre>`

	output += "\nNeil didn't make this look nicer yet"
	output += `</body></html>`
	resp.Write([]byte(output))
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
		loginfof("%s: Read %d data: %s", path, n, string(readBuf[:n]))

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

	serializedConfig, err := json.MarshalIndent(build.Config(), "", "    ")
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
	fmt.Printf(ret)
	return ret
}

func logwarnf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("web-warn: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}

func logcritf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("web-crit: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}
