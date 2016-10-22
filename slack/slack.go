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

var (
	clientLock   = sync.RWMutex{}
	client       *slack.Client
	clientID     string
	clientSecret string
	errNoClient  = errors.New("Slack client is not authenticated")
	hostname     string
	oauth2Scopes = []string{"incoming-webhook"}
	oauth2State  = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())
)

func Init(hostname, clientID, clientSecret string) {
	loadToken(hostname, clientID, clientSecret)

	http.HandleFunc("/cb/auth/slack", handleSlackAuth)
}

// Try and load an existing token, otherwise print out instructions
// for the user to log-in the app
func loadToken(host, id, secret string) {
	hostname = host
	clientID = id
	clientSecret = secret

	name := getConfigFilePath()
	cfg := Config{}

	if _, err := os.Stat(name); err != nil {
		printAuthHelp()
	} else if data, err := ioutil.ReadFile(name); err != nil {
		printWarning("Unable to load config - %s", err.Error())
		printAuthHelp()
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		printWarning("Unable to unmarshal config - %s", err.Error())
	} else if cfg.Token == "" {
		printAuthHelp()
	} else {
		setClient(cfg.Token)
	}
}

func setClient(token string) {
	clientLock.Lock()
	defer clientLock.Unlock()

	client = slack.New(token)
}

func getClient() (*slack.Client, error) {
	clientLock.RLock()
	defer clientLock.RUnlock()

	if client != nil {
		return client, nil
	}
	return nil, errNoClient
}

func getOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oslack.Endpoint,
		RedirectURL:  fmt.Sprintf("https://%s/cb/auth/slack", hostname),
		Scopes:       oauth2Scopes,
	}
}

func printAuthHelp() {
	cfg := getOAuth2Config()

	printInfo("This app must be authenticated, please visit the following URL to authenticate this app:")
	fmt.Println(cfg.AuthCodeURL(oauth2State))
}

func saveConfig(cfg *Config) {
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
func handleSlackAuth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	state := q.Get("state")
	if state != oauth2State {
		printWarning("OAuth2 `state` was incorrect, something bad happened between Slack and us")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	code := q.Get("code")
	cfg := getOAuth2Config()

	token, err := cfg.Exchange(context.TODO(), code)
	if err != nil {
		printWarning("Unable to authenticate with Slack: %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	saveConfig(&Config{Token: token.AccessToken})

	setClient(token.AccessToken)

	w.Write([]byte("Thanks! You can close this tab now."))
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
