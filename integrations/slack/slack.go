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

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	oslack "golang.org/x/oauth2/slack"

	"github.com/nlopes/slack"
)

// TODO:
//  - Save failed build info so it can be looked up
//  - Handle actions webhook
//  - Better code re-use, message builders func with the basics done already
//  - Add support for fixed builds
//  - Use a build-config to get all the info, not just hard-coded

var (
	errNoClient  = errors.New("Slack client is not authenticated")
	oauth2Scopes = []string{"bot", "chat:write:bot", "files:write:user"}
	oauth2State  = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())
)

type Config struct {
	Token string `json:"token"`
}

type Slack struct {
	clientLock   *sync.RWMutex
	client       *slack.Client
	clientID     string
	clientSecret string
	hostname     string
}

func New(hostname, clientID, clientSecret string) *Slack {
	s := &Slack{
		clientLock:   &sync.RWMutex{},
		clientID:     clientID,
		clientSecret: clientSecret,
		hostname:     hostname,
	}

	s.loadToken()

	http.HandleFunc("/cb/auth/slack", s.handleSlackAuth())
	http.HandleFunc("/cb/slack", s.handleSlackAction())

	return s
}

// Try and load an existing token, otherwise print out instructions
// for the user to log-in the app
func (s *Slack) loadToken() {
	name := getConfigFilePath()
	cfg := Config{}

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

	printInfo("This app must be authenticated, please visit the following URL to authenticate this app:")
	fmt.Println(cfg.AuthCodeURL(oauth2State))
}

func (s *Slack) saveConfig(cfg *Config) {
	name := getConfigFilePath()

	if data, err := json.Marshal(cfg); err != nil {
		printWarning("Unable to save config file - error marshalling data: %s", err.Error())
	} else if err := ioutil.WriteFile(name, data, 0644); err != nil {
		printWarning("Unable to save config file - %s", err.Error())
	}
}

//
// HTTP Callbacks
//
func (s *Slack) handleSlackAuth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		state := q.Get("state")
		if state != oauth2State {
			printWarning("OAuth2 `state` was incorrect, something bad happened between Slack and us")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		code := q.Get("code")
		cfg := s.getOAuth2Config()

		token, err := cfg.Exchange(context.TODO(), code)
		if err != nil {
			printWarning("Unable to authenticate with Slack: %s", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		s.saveConfig(&Config{Token: token.AccessToken})

		s.setClient(token.AccessToken)

		w.Write([]byte("Thanks! You can close this tab now."))
	}
}

func (s *Slack) handleSlackAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, _ := ioutil.ReadAll(r.Body)
		fmt.Println(string(data))
		w.WriteHeader(http.StatusOK)
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

//
// Hooks for various actions
//

func (s *Slack) BuildSucceeded() {
	client, err := s.getClient()
	if err != nil {
		return
	}

	params := slack.PostMessageParameters{
		Attachments: []slack.Attachment{
			slack.Attachment{
				Color:      "#36a64f",
				Fallback:   "Pull Request #24 (Bootstrap the repo) *passed*",
				AuthorName: "gordallott/ngbuild:bootstrap",
				AuthorLink: "https://github.com/gordallott/ngbuild/tree/bootstrap",
				Title:      "#24: Boostrap the repo: PASSED",
				TitleLink:  "https://github.com/watchly/ngbuild/pull/24",
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

	id, timestamp, err := client.PostMessage("testing", "", params)
	if err != nil {
		printWarning("%s(%d): %+v", id, timestamp, err)
	}
}

func (s *Slack) BuildFailed() {
	client, err := s.getClient()
	if err != nil {
		return
	}

	params := slack.PostMessageParameters{
		Attachments: []slack.Attachment{
			slack.Attachment{
				Color:      "#bb2c32",
				CallbackID: "<build token>",
				Fallback:   "Pull Request #24 (Bootstrap the repo) *failed*",
				AuthorName: "gordallott/ngbuild:bootstrap",
				AuthorLink: "https://github.com/gordallott/ngbuild/tree/bootstrap",
				Title:      "#24: Boostrap the repo",
				TitleLink:  "https://github.com/watchly/ngbuild/pull/24",
				Fields: []slack.AttachmentField{
					slack.AttachmentField{
						Title: "Tests Failed",
						Value: "2",
						Short: true,
					},
					slack.AttachmentField{
						Title: "Time Taken",
						Value: "13m52s",
						Short: true,
					},
				},
				Actions: []slack.AttachmentAction{
					slack.AttachmentAction{
						Name:  "log",
						Text:  "View Build Log",
						Type:  "button",
						Value: "log",
					},
					slack.AttachmentAction{
						Name:  "rebuild",
						Text:  "Rebuild",
						Type:  "button",
						Style: "danger",
						Value: "rebuild",
					},
				},
			},
		},
	}

	id, timestamp, err := client.PostMessage("testing", "", params)
	if err != nil {
		printWarning("Error sending message: %+v", id, timestamp, err)
	}
}
