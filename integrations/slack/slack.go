package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	oslack "golang.org/x/oauth2/slack"

	_ "github.com/mitchellh/mapstructure"

	"github.com/nlopes/slack"
	"github.com/watchly/ngbuild/core"
)

const (
	actionValueRebuild = "rebuild"
	colorSucceeded     = "#36a64f"
	colorFailed        = "#bb2c32"
)

var (
	errNoClient  = errors.New("Slack client is not authenticated")
	oauth2Scopes = []string{"incoming-webhook"}
	oauth2State  = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())
	silent       = false
)

type (
	//Slack ...
	Slack struct {
		m            sync.RWMutex
		client       *slack.Client
		clientID     string
		clientSecret string
		hostname     string
		apps         []core.App
	}

	tokenCache struct {
		Token   string `mapstructure:"token"`
		Webhook string `mapstructure:"webhook"`
	}

	messageParams struct {
		Attachments []slack.Attachment `mapstructure:"attachments"`
	}

	config struct {
		Hostname     string `mapstructure:"hostname"`
		ClientID     string `mapstructure:"clientId"`
		ClientSecret string `mapstructure:"clientSecret"`
		Channel      string `mapstructure:"channel"`
		OnlyFixed    bool   `mapstructure:"onlyFixed"`
	}
)

// NewSlack ...
func NewSlack() *Slack {
	s := &Slack{}
	http.HandleFunc("/cb/auth/slack", s.handleSlackAuth())
	http.HandleFunc("/cb/slack", s.handleSlackAction())

	core.RegisterIntegration(s)

	return s
}

// Identifier ...
func (s *Slack) Identifier() string {
	return "slack"
}

// IsProvider ...
func (s *Slack) IsProvider(string) bool {
	return false
}

// ProvideFor ...
func (s *Slack) ProvideFor(*core.BuildConfig, string) error {
	return errors.New("Slack can't provide, man")
}

// AttachToApp ...
func (s *Slack) AttachToApp(app core.App) error {
	s.m.Lock()
	defer s.m.Unlock()

	// Don't have to do this everytime
	if s.clientID == "" {
		cfg := config{}
		app.Config("slack", &cfg)
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			printWarning("Configuration for app `%s` does not have Slack OAuth credentials", app.Name())
			return nil
		} else {
			s.clientID = cfg.ClientID
			s.clientSecret = cfg.ClientSecret
			go s.loadToken()
		}

		var gcfg struct {
			Hostname string `mapstructure:"hostname"`
		}
		app.GlobalConfig(&gcfg)
		if gcfg.Hostname == "" {
			printWarning("Global configuration for app `%s` does not have a hostname specified", app.Name())
			return nil
		} else {
			s.hostname = gcfg.Hostname
		}
	}

	app.Listen(core.SignalBuildComplete, s.onBuildComplete(app))
	s.apps = append(s.apps, app)
	return nil
}

// Shutdown ...
func (s *Slack) Shutdown() {

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

// BuildSucceeded ...
func (s *Slack) BuildSucceeded(app core.App, build core.Build) {
	s.PostBuildMessage(app, build, true)
}

// BuildFailed ...
func (s *Slack) BuildFailed(app core.App, build core.Build) {
	s.PostBuildMessage(app, build, false)
}

// PostBuildMessage ...
func (s *Slack) PostBuildMessage(app core.App, build core.Build, succeeded bool) {
	// Remove in prod
	channel := "testing"

	cfg := config{}
	if err := app.Config("slack", &cfg); err != nil {
		printWarning("Unable to load channel")
	} else {
		if cfg.Channel != "" {
			channel = cfg.Channel
		}

		if cfg.OnlyFixed && succeeded {
			history := build.History()
			hl := len(history)
			if hl > 0 {
				if lastBuild := history[hl-1]; lastBuild != nil {
					if code, err := lastBuild.ExitCode(); code == 0 && err == nil {
						// Last build wasn't broken so don't do anything
						return
					}
				}
			}
		}
	}

	params := s.getBaseMessageParams(app, build, succeeded)

	client, err := s.getClient()
	if err != nil {
		printWarning(err.Error())
		return
	}

	_, _, err = client.PostMessage(channel, "", *params)
	if err != nil {
		printWarning("Error sending message: %s", err.Error())
	}
}

func (s *Slack) getBaseMessageParams(app core.App, build core.Build, succeeded bool) *slack.PostMessageParameters {
	color := colorSucceeded
	suffix := "passed"

	if !succeeded {
		color = colorFailed
		suffix = "failed"
	}

	cfg := build.Config()
	pull := cfg.GetMetadata("github:PullNumber")

	params := slack.PostMessageParameters{
		Attachments: []slack.Attachment{
			slack.Attachment{
				AuthorName: app.Name(),
				Color:      color,
				CallbackID: build.Token(),
				Fallback:   fmt.Sprintf("#%s - %s: %s", pull, cfg.Title, suffix),
				Title:      fmt.Sprintf("#%s - %s", pull, cfg.Title),
				TitleLink:  cfg.URL,
				Text:       fmt.Sprintf("Build time: %dm%ds\n<%s|View build>", int64(build.BuildTime().Minutes()), int64(build.BuildTime().Seconds())%60, fmt.Sprintf("https://%s/web/%s/%s", s.hostname, app.Name(), build.Token())),
				MarkdownIn: []string{"title", "text"},
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

		s.saveConfig(&tokenCache{Token: res.AccessToken, Webhook: res.IncomingWebhook.URL})

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
		token := actionData.CallbackID

		switch action.Value {
		case actionValueRebuild:
			text := fmt.Sprintf(":arrows_counterclockwise: _*%s* requested a rebuild_", actionData.User.Name)

			if app, build := s.buildForToken(token); app != nil && build != nil {
				if _, err := build.NewBuild(); err != nil {
					text = fmt.Sprintf(":cry: Unable to start build: %s", err.Error())
				}
			} else {
				text = fmt.Sprintf(":confused: No matching builds for token %s", token)
			}

			// Update the existing message so people don't keep requesting rebuilds
			params := messageParams{}
			params.Attachments = actionData.OriginalMessage.Attachments
			params.Attachments = append(params.Attachments, slack.Attachment{
				Text:       text,
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
func (s *Slack) buildForToken(token string) (core.App, core.Build) {
	s.m.RLock()
	defer s.m.RUnlock()

	for _, a := range s.apps {
		if build, _ := a.GetBuild(token); build != nil {
			return a, build
		}
	}

	return nil, nil
}

func (s *Slack) loadToken() {
	s.m.Lock()
	defer s.m.Unlock()

	// Try and load an existing token, otherwise print out instructions
	// for the user to log-in the app
	cfg := tokenCache{}
	cfg.Token = core.GetCache("slack:token")
	cfg.Webhook = core.GetCache("slack:webhook")
	if cfg.Token == "" {
		s.printAuthHelp()
	} else {
		s.client = slack.New(cfg.Token)
	}
}

func (s *Slack) setClient(token string) {
	s.m.Lock()
	defer s.m.Unlock()

	s.client = slack.New(token)
}

func (s *Slack) getClient() (*slack.Client, error) {
	s.m.RLock()
	defer s.m.RUnlock()

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
		RedirectURL:  fmt.Sprintf("%s/cb/auth/slack", strings.Replace(core.GetHTTPServerURL(), "http://", "https://", 1)),
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

func (s *Slack) saveConfig(cfg *tokenCache) {
	core.StoreCache("slack:token", cfg.Token)
	core.StoreCache("slack:webhook", cfg.Webhook)
}

//
// Util funcs, should be plugged into main app
//
func printInfo(message string, args ...interface{}) {
	if silent {
		return
	}
	fmt.Printf("INFO Slack - "+message+"\n", args...)
}

func printWarning(message string, args ...interface{}) {
	if silent {
		return
	}
	fmt.Printf("WARN Slack - "+message+"\n", args...)
}
