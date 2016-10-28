package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	githubO2 "golang.org/x/oauth2/github"

	"github.com/google/go-github/github"
	"github.com/watchly/ngbuild/core"
)

var oauth2State = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())

type githubConfig struct {
	ClientID     string `mapstructure:"clientID"` //`mapstructure:"clientID"`
	ClientSecret string `mapstructure:"clientSecret"`
}

// Github ...
type Github struct {
	m         sync.Mutex
	appConfig githubConfig

	client                 *github.Client
	clientID, clientSecret string
	clientHasSet           *sync.Cond
}

// New ...
func New() *Github {
	g := &Github{
		clientHasSet: sync.NewCond(&sync.Mutex{}),
	}

	http.HandleFunc("/cb/auth/github", g.handleGithubAuth)
	return g
}

// Identifier ...
func (g *Github) Identifier() string { return "github" }

// IsProvider ...
func (g *Github) IsProvider(source string) bool {
	return strings.HasPrefix(source, "git@github.com:")
}

// ProvideFor ...
func (g *Github) ProvideFor(config *core.BuildConfig, directory string) error {
	// FIXME, need to git checkout the given config
	return errors.New("Not Implimented")
}

func (g *Github) handleGithubAuth(resp http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	state := q.Get("state")
	if state != oauth2State {
		resp.Write([]byte("OAuth2 state was incorrect, something bad happened between Github and us"))
		return
	}

	code := q.Get("code")
	cfg := g.getOauthConfig()

	token, err := cfg.Exchange(context.Background(), code)
	if err != nil {
		resp.Write([]byte("Error exchanging OAuth code, something bad happened between Github and us: " + err.Error()))
		return
	}

	core.StoreCache("github:token", token.AccessToken)
	g.setClient(token)

	resp.Write([]byte("Thanks! you can close this tab now."))
}

func (g *Github) getOauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.appConfig.ClientID,
		ClientSecret: g.appConfig.ClientSecret,
		Endpoint:     githubO2.Endpoint,
		Scopes:       []string{"repo"},
	}
}

func (g *Github) setClient(token *oauth2.Token) {
	ts := g.getOauthConfig().TokenSource(oauth2.NoContext, token)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	g.client = github.NewClient(tc)
	g.clientHasSet.Broadcast()
}

func (g *Github) acquireOauthToken() {
	token := core.GetCache("github:token")

	if token != "" {
		oauth2Token := oauth2.Token{AccessToken: token}
		g.setClient(&oauth2Token)
		return
	}

	fmt.Println("")
	fmt.Println("This app must be authenticated with github, please visit the following URL to authenticate this app")
	fmt.Println(g.getOauthConfig().AuthCodeURL(oauth2State, oauth2.AccessTypeOffline))
	fmt.Println("")
}

// AttachToApp ...
func (g *Github) AttachToApp(app core.App) error {
	g.m.Lock()
	defer g.m.Unlock()

	if g.client == nil {
		app.Config("github", &g.appConfig)
		if g.appConfig.ClientID == "" || g.appConfig.ClientSecret == "" {
			fmt.Println("Invalid github configuration, missing ClientID/ClientSecret")
		} else {
			g.clientHasSet.L.Lock()
			g.acquireOauthToken()
			for g.client == nil {
				fmt.Println("Waiting for github authentication response...")
				g.clientHasSet.Wait()
			}
			g.clientHasSet.L.Unlock()
		}
	}

	return nil
}

// Shutdown ...
func (g *Github) Shutdown() {}
