package github

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	githubO2 "golang.org/x/oauth2/github"

	"github.com/google/go-github/github"
	"github.com/watchly/ngbuild/core"
)

var oauth2State = fmt.Sprintf("%d%d%d", os.Getuid(), os.Getpid(), time.Now().Unix())

type pullRequestStatus struct {
	pull         *github.PullRequest
	currentBuild string // build token
	mergeOnPass  bool
}

type githubConfig struct {
	ClientID     string `mapstructure:"clientID"`
	ClientSecret string `mapstructure:"clientSecret"`

	Owner           string   `mapstructure:"owner"`
	Repo            string   `mapstructure:"repo"`
	IgnoredBranches []string `mapstructure:"ignoredBranches"`
	PublicKey       string   `mapstructure:"publicKey"`

	CancelOnNewCommit    bool     `mapstructure:"cancelOnNewCommit"`
	MergeOnPass          bool     `mapstructure:"mergeOnPass"`
	MergeOnPassAuthWords []string `mapstructure:"mergeOnPassAuthWords"`
}

type githubApp struct {
	app    core.App
	config githubConfig
}

// Github ...
type Github struct {
	m            sync.RWMutex
	globalConfig githubConfig
	apps         map[string]*githubApp

	client                 *github.Client
	clientID, clientSecret string
	clientHasSet           *sync.Cond

	trackedPullRequests map[string]pullRequestStatus
}

// New ...
func New() *Github {
	g := &Github{
		clientHasSet:        sync.NewCond(&sync.Mutex{}),
		apps:                make(map[string]*githubApp),
		trackedPullRequests: make(map[string]pullRequestStatus),
	}

	http.HandleFunc("/cb/auth/github", g.handleGithubAuth)
	http.HandleFunc("/cb/github/hook/", g.handleGithubEvent)
	return g
}

// Identifier ...
func (g *Github) Identifier() string { return "github" }

// IsProvider ...
func (g *Github) IsProvider(source string) bool {
	loginfof("Asked to provide for %s", source)
	return strings.HasPrefix(source, "git@github.com:")
}

// ProvideFor ...
func (g *Github) ProvideFor(config *core.BuildConfig, directory string) error {
	// FIXME, need to git checkout the given config
	return g.cloneAndMerge(directory, config)
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

func (g *Github) handleGithubEvent(resp http.ResponseWriter, req *http.Request) {
	splits := strings.Split(req.URL.Path, "/")
	appIndex := len(splits) - 1

	appName := splits[appIndex]

	app, ok := g.apps[appName]
	if ok == false {
		logwarnf("Got unknown webhook app name: %s", appName)
		return
	}

	eventType := req.Header.Get("X-GitHub-Event")
	if eventType == "" {
		logwarnf("No event type specified in webhook")
		return
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		logcritf("Error decoding webhook %s:%s", req.URL.RawPath, err)
		return
	}
	loginfof("Got webhook event: %s", eventType)

	switch eventType {
	case "commit_comment":
		g.handleGithubCommitComment(app, body)
	case "delete":
		g.handleGithubDelete(app, body)
	case "pull_request":
		g.handleGithubPullRequest(app, body)
	case "issue_comment":
		g.handleGithubIssueComment(app, body)
	case "pull_request_review_comment":
		g.handleGithubPullRequestReviewComment(app, body)
	case "push":
		g.handleGithubPush(app, body)

	default:
		logwarnf("Could not handle event type: %s", eventType)
		return
	}

	return
}

func (g *Github) getOauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.globalConfig.ClientID,
		ClientSecret: g.globalConfig.ClientSecret,
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

func (g *Github) init(app core.App) {
	if g.client == nil {
		app.Config("github", &g.globalConfig)
		if g.globalConfig.ClientID == "" || g.globalConfig.ClientSecret == "" {
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
}

// AttachToApp ...
func (g *Github) AttachToApp(app core.App) error {
	g.m.Lock()
	defer g.m.Unlock()
	g.init(app)

	appConfig := &githubApp{
		app: app,
	}
	app.Config("github", &appConfig.config)
	g.apps[app.Name()] = appConfig

	g.setupDeployKey(appConfig)
	g.setupHooks(appConfig)

	app.Listen(core.SignalBuildStarted, g.onBuildStarted)
	app.Listen(core.SignalBuildComplete, g.onBuildFinished)
	return nil
}

func (g *Github) setupDeployKey(appConfig *githubApp) error {
	cfg := appConfig.config
	// TODO - would be nicer to generate ssh key automatically
	if cfg.PublicKey == "" {
		logcritf("(%s) No public key available, create one and add it to the configuration", appConfig.app.Name())
		return errors.New("No pub key available")
	}

	keyName := fmt.Sprintf("NGBuild ssh deploy key - %s", appConfig.app.Name())
	_, _, err := g.client.Repositories.CreateKey(cfg.Owner, cfg.Repo, &github.Key{
		Title:    &keyName,
		Key:      &cfg.PublicKey,
		ReadOnly: &[]bool{true}[0],
	})

	if err != nil && strings.Contains(err.Error(), "key is already in use") == false {
		logcritf("Couldn't create deploy key for %s: %s", appConfig.app.Name(), err)
		return err
	}

	return nil
}

func (g *Github) setupHooks(appConfig *githubApp) {
	cfg := appConfig.config
	_, _, err := g.client.Repositories.Get(cfg.Owner, cfg.Repo)
	if err != nil {
		logwarnf("(%s) Repository does not exist, owner=%s, repo=%s", appConfig.app.Name(), cfg.Owner, cfg.Repo)
		return
	}

	hookURL := fmt.Sprintf("%s/cb/github/hook/%s", core.GetHTTPServerURL(), appConfig.app.Name())
	_, _, err = g.client.Repositories.CreateHook(cfg.Owner, cfg.Repo, &github.Hook{
		Name:   &[]string{"web"}[0],
		Active: &[]bool{true}[0],
		Config: map[string]interface{}{
			"url":          hookURL,
			"content_type": "json",
		},
		Events: []string{"pull_request",
			"delete",
			"issue_comment",
			"pull_request_review",
			"pull_request_review_event",
			"push",
			"status",
		},
	})
	if err != nil && strings.Contains(err.Error(), "Hook already exists") == false {
		logwarnf("Could not create webhook, owner=%s, repo=%s: %s", cfg.Owner, cfg.Repo, err)
		return
	}

}

// Shutdown ...
func (g *Github) Shutdown() {}

func (g *Github) trackPullRequest(app *githubApp, event *github.PullRequestEvent) {
	if event.PullRequest == nil {
		logcritf("pull request is nil")
		return
	}
	pull := event.PullRequest
	pullID := strconv.Itoa(*pull.ID)

	g.m.Lock()
	defer g.m.Unlock()

	// check for ignored branches
	for _, branchIgnore := range app.config.IgnoredBranches {
		if branchIgnore == *pull.Base.Ref {
			logwarnf("Ignoring pull request, is an ignored branch")
			return
		}
	}

	g.trackedPullRequests[pullID] = pullRequestStatus{
		pull: pull,
	}
	g.buildPullRequest(app, pull)
}

func (g *Github) buildPullRequest(app *githubApp, pull *github.PullRequest) {
	// for reference, head is the proposed branch, base is the branch to merge into
	pullID := strconv.Itoa(*pull.ID)
	loginfof("Building pull request: %s", pullID)
	status, ok := g.trackedPullRequests[pullID]
	if ok == false {
		status = pullRequestStatus{pull, "", false}
		g.trackedPullRequests[pullID] = status
	}

	// we want to check to see if we are already building or already built this commit
	// and we want to cancel the previous build
	if build, _ := app.app.GetBuild(status.currentBuild); build != nil {
		if build.Config().GetMetadata("github:HeadHash") == *pull.Head.SHA {
			logwarnf("Already building/built this commit")
			return
		}

		if app.config.CancelOnNewCommit {
			build.Stop()
		}
	}

	headBranch := *pull.Head.Ref
	headCloneURL := *pull.Head.Repo.SSHURL
	headCommit := *pull.Head.SHA
	headOwner := *pull.Head.Repo.Owner.Login
	headRepo := *pull.Head.Repo.Name

	baseBranch := *pull.Base.Ref
	baseCloneURL := *pull.Base.Repo.SSHURL
	baseOwner := *pull.Base.Repo.Owner.Login
	baseRepo := *pull.Base.Repo.Name
	baseCommit := *pull.Base.SHA

	buildConfig := core.BuildConfig{
		Title: *pull.Title,
		URL:   *pull.URL,

		HeadRepo:   headCloneURL,
		HeadBranch: headBranch,
		HeadHash:   headCommit,

		BaseRepo:   baseCloneURL,
		BaseBranch: baseBranch,
		BaseHash:   "",

		Group: pullID,
	}

	buildConfig.SetMetadata("github:PullRequestID", pullID)
	buildConfig.SetMetadata("github:HeadHash", headCommit)
	buildConfig.SetMetadata("github:HeadOwner", headOwner)
	buildConfig.SetMetadata("github:HeadRepo", headRepo)
	buildConfig.SetMetadata("github:BaseHash", baseCommit)
	buildConfig.SetMetadata("github:BaseOwner", baseOwner)
	buildConfig.SetMetadata("github:BaseRepo", baseRepo)

	buildToken, err := app.app.NewBuild(buildConfig.Group, &buildConfig)
	if err != nil {
		logcritf("Couldn't start build for %d", *pull.ID)
		return
	}

	build, err := app.app.GetBuild(buildToken)
	if err != nil || build == nil {
		logcritf("Couldn't get build for %d", *pull.ID)
		return
	}

	status.currentBuild = buildToken
	g.trackedPullRequests[pullID] = status
	loginfof("started build: %s", buildToken)
}

func (g *Github) updatePullRequest(app *githubApp, event *github.PullRequestEvent) {
	// this is called when there is a new commit on the pull request or something like that
	pullID := strconv.Itoa(*event.PullRequest.ID)

	g.m.RLock()
	_, ok := g.trackedPullRequests[pullID]
	g.m.RUnlock()

	if ok == false {
		logwarnf("event on unknown/ignored pull request: %s", pullID)
		g.trackPullRequest(app, event)
		return
	}

	g.buildPullRequest(app, event.PullRequest)
}

func (g *Github) closedPullRequest(app *githubApp, event *github.PullRequestEvent) {
	g.m.RLock()
	defer g.m.RUnlock()

	pullID := strconv.Itoa(*event.PullRequest.ID)
	status, ok := g.trackedPullRequests[pullID]
	if ok == false {
		return
	}

	if build, _ := app.app.GetBuild(status.currentBuild); build != nil {
		if app.config.CancelOnNewCommit {
			build.Stop()
		}
	}
	delete(g.trackedPullRequests, pullID)
}

func loginfof(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("github-info: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}

func logwarnf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("github-warn: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}

func logcritf(str string, args ...interface{}) (ret string) {
	ret = fmt.Sprintf("github-crit: "+str+"\n", args...)
	fmt.Printf(ret)
	return ret
}
