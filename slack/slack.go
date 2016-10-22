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

var (
	errNoClient  = errors.New("Slack client is not authenticated")
	oauth2Scopes = []string{"incoming-webhook"}
	oauth2State  = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())
)

func New(hostname, clientID, clientSecret string) *Slack {
	s := &Slack{
		clientLock:   &sync.RWMutex{},
		clientID:     clientID,
		clientSecret: clientSecret,
		hostname:     hostname,
	}

	s.loadToken()

	http.HandleFunc("/cb/auth/slack", s.handleSlackAuth())

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
