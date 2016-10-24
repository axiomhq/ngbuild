package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"golang.org/x/oauth2"
	oslack "golang.org/x/oauth2/slack"

	"github.com/nlopes/slack"
	"github.com/watchly/ngbuild/core"
)

// TODO:
//  - Save failed build info so it can be looked up
//  - Handle actions webhook
//  - Better code re-use, message builders func with the basics done already
//  - Add support for fixed builds
//  - Use a build-config to get all the info, not just hard-coded

const (
	actionValueRebuild = "rebuild"
)

var (
	errNoClient  = errors.New("Slack client is not authenticated")
	oauth2Scopes = []string{"incoming-webhook"}
	oauth2State  = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())
)

type (
	Slack struct {
		clientLock   *sync.RWMutex
		client       *slack.Client
		clientID     string
		clientSecret string
		hostname     string
		channel      string
	}

	config struct {
		Token   string `json:"token"`
		Webhook string `json:"webhook"`
	}

	messageParams struct {
		Attachments []slack.Attachment `json:"attachments"`
	}
)

func New(hostname, clientID, clientSecret string) *Slack {
	s := &Slack{
		clientLock:   &sync.RWMutex{},
		clientID:     clientID,
		clientSecret: clientSecret,
		hostname:     hostname,
		channel:      "testing",
	}

	s.loadToken()

	http.HandleFunc("/cb/auth/slack", s.handleSlackAuth())
	http.HandleFunc("/cb/slack", s.handleSlackAction())

	return s
}

func (s *Slack) Identifier() string {
	return "slack"
}

func (s *Slack) IsProvider(string) bool {
	return false
}

func (s *Slack) ProvideFor(core.Build, string) error {
	return errors.New("Slack can't provide, man")
}

func (s *Slack) AttachToApp(app core.App) error {
	app.Listen(core.SignalBuildComplete, s.onBuildComplete(app))
	return nil
}

func (s *Slack) onBuildComplete(app core.App) func(map[string]string) {
	return func(values map[string]string) {
		token := values["token"]
		if build, err := app.GetBuild(token); err != nil {
			printWarning("Build %s does not exist: %s", token, err.Error())
		} else {
			if code, err := build.ExitCode(); err != nil {
				printWarning("BuildCompleted fired before build was completed: %s", err.Error())
			} else if code == 0 {
				s.BuildSucceeded(app, build)
			} else {
				s.BuildFailed(app, build)
			}
		}
	}
}

//
// Hooks for various actions & message creation
//
func (s *Slack) BuildSucceeded(core.App, core.Build) {
	client, err := s.getClient()
	if err != nil {
		printWarning("Slack client is not authenticated")
		return
	}

	params := slack.PostMessageParameters{
		Attachments: []slack.Attachment{
			slack.Attachment{
				Color:     "#36a64f",
				Fallback:  "Pull Request #24 (Bootstrap the repo) *passed*",
				Title:     "#24: Boostrap the repo: PASSED",
				TitleLink: "https://github.com/watchly/ngbuild/pull/24",
				Fields: []slack.AttachmentField{
					slack.AttachmentField{
						Title: "Tests Passed",
						Value: "249",
						Short: true,
					},
					slack.AttachmentField{
						Title: "Time Taken",
						Value: "12m52s",
						Short: true,
					},
				},
			},
		},
	}

	id, timestamp, err := client.PostMessage(s.channel, "", params)
	if err != nil {
		printWarning("%s(%d): %+v", id, timestamp, err)
	}
}

func (s *Slack) BuildFailed(app core.App, build core.Build) {
	client, err := s.getClient()
	if err != nil {
		printWarning("Slack client is not authenticated")
	}

	params := s.getBaseMessageParams(app, build, false)

	id, timestamp, err := client.PostMessage(s.channel, "", *params)
	if err != nil {
		printWarning("Error sending message: %+v", id, timestamp, err)
	}
}

func (s *Slack) getBaseMessageParams(app core.App, build core.Build, succeeded bool) *slack.PostMessageParameters {
	color := "#36a64f"
	suffix := "*passed*"

	if !succeeded {
		color = "#bb2c32"
		suffix = "*failed*"
	}

	// FIXME: We don't have a way to get the build config of the build, so making
	// a fake one until we do
	cfg := core.BuildConfig{
		Title: "#24: Bootstrap the repo",
		URL:   "https://github.com/ngbuild/pull/24",
	}

	params := slack.PostMessageParameters{
		Attachments: []slack.Attachment{
			slack.Attachment{
				Color:      color,
				CallbackID: build.Token(),
				Fallback:   fmt.Sprintf("%s: %s - %s", app.Name(), cfg.Title, suffix),
				Title:      fmt.Sprintf("%s: %s", app.Name(), cfg.Title),
				TitleLink:  cfg.URL,
				Fields: []slack.AttachmentField{
					slack.AttachmentField{
						Title: "Time Taken",
						Value: fmt.Sprintf("%dm:%ds", int64(build.BuildTime().Minutes()), int64(build.BuildTime().Seconds())%60),
						Short: true,
					},
				},
				MarkdownIn: []string{"title"},
			},
		},
	}

	if !succeeded {
		params.Attachments[0].Actions = []slack.AttachmentAction{
			slack.AttachmentAction{
				Name:  "rebuild",
				Text:  "Rebuild",
				Type:  "button",
				Style: "danger",
				Value: actionValueRebuild,
			},
		}
	}

	return &params
}

//
// HTTP Callbacks
//
func (s *Slack) handleSlackAuth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		state := q.Get("state")
		if state != oauth2State {
			w.Write([]byte("OAuth2 `state` was incorrect, something bad happened between Slack and us"))
			return
		}

		code := q.Get("code")
		cfg := s.getOAuth2Config()

		res, err := slack.GetOAuthResponse(cfg.ClientID, cfg.ClientSecret, code, cfg.RedirectURL, false)
		if err != nil {
			w.Write([]byte(fmt.Sprintf("Unable to authenticate with Slack: %s", err.Error())))
			return
		}

		s.saveConfig(&config{Token: res.AccessToken, Webhook: res.IncomingWebhook.URL})

		s.setClient(res.AccessToken)

		w.Write([]byte("Thanks! You can close this tab now."))
	}
}

func (s *Slack) handleSlackAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := r.FormValue("payload")

		actionData := slack.AttachmentActionCallback{}
		if err := json.Unmarshal([]byte(payload), &actionData); err != nil {
			printWarning("Unable to unmarshal Slack action callback: %s", err.Error())
			return
		}

		if len(actionData.Actions) < 1 {
			printWarning("No action in callback message: %s", payload)
			return
		}

		action := actionData.Actions[0]

		switch action.Value {
		case actionValueRebuild:
			printWarning("Rebuild %s - not implemented", actionData.CallbackID)

			// Respond to the request
			params := messageParams{}
			params.Attachments = actionData.OriginalMessage.Attachments
			params.Attachments = append(params.Attachments, slack.Attachment{
				Text:       fmt.Sprintf(":arrows_counterclockwise: _*%s* requested a rebuild_", actionData.User.Name),
				Color:      params.Attachments[0].Color,
				MarkdownIn: []string{"text"},
			})

			// Remove original actions
			params.Attachments[0].Actions = nil

			if data, err := json.Marshal(params); err != nil {
				printWarning("Unable to marshal JSON payload for action callback: %s", err.Error())
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			}
		default:
			printWarning("Action `%s` not supported", action.Value)
		}
	}
}

//
// Internal
//

func (s *Slack) loadToken() {
	// Try and load an existing token, otherwise print out instructions
	// for the user to log-in the app
	name := getConfigFilePath()
	cfg := config{}

	if _, err := os.Stat(name); err != nil {
		s.printAuthHelp()
	} else if data, err := ioutil.ReadFile(name); err != nil {
		printWarning("Unable to load config - %s", err.Error())
		s.printAuthHelp()
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		printWarning("Unable to unmarshal config - %s", err.Error())
	} else if cfg.Token == "" {
		s.printAuthHelp()
	} else {
		s.setClient(cfg.Token)
	}
}

func (s *Slack) setClient(token string) {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	s.client = slack.New(token)
}

func (s *Slack) getClient() (*slack.Client, error) {
	s.clientLock.RLock()
	defer s.clientLock.RUnlock()

	if s.client != nil {
		return s.client, nil
	}
	return nil, errNoClient
}

func (s *Slack) getOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		Endpoint:     oslack.Endpoint,
		RedirectURL:  fmt.Sprintf("https://%s/cb/auth/slack", s.hostname),
		Scopes:       oauth2Scopes,
	}
}

func (s *Slack) printAuthHelp() {
	cfg := s.getOAuth2Config()

	fmt.Println("")
	printInfo("This app must be authenticated, please visit the following URL to authenticate this app:")
	fmt.Println(cfg.AuthCodeURL(oauth2State))
	fmt.Println("")
}

func (s *Slack) saveConfig(cfg *config) {
	name := getConfigFilePath()

	if data, err := json.Marshal(cfg); err != nil {
		printWarning("Unable to save config file - error marshalling data: %s", err.Error())
	} else if err := ioutil.WriteFile(name, data, 0644); err != nil {
		printWarning("Unable to save config file - %s", err.Error())
	}
}

//
// Util funcs, should be plugged into main app
//
func getDataDir() string {
	return os.TempDir()
}

func getConfigFilePath() string {
	return path.Join(getDataDir(), "slack.config")
}

func printInfo(message string, args ...interface{}) {
	fmt.Printf("INFO Slack - "+message+"\n", args...)
}

func printWarning(message string, args ...interface{}) {
	fmt.Printf("WARN Slack - "+message+"\n", args...)
}
