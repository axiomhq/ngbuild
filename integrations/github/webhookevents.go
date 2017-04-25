package github

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
	"github.com/watchly/ngbuild/core"
)

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

func (g *Github) handleGithubCommitComment(app *githubApp, body []byte) {}
func (g *Github) handleGithubDelete(app *githubApp, body []byte)        {}

func (g *Github) handleGithubIssueComment(app *githubApp, body []byte) {}

func (g *Github) handleGithubPullRequest(app *githubApp, body []byte) {
	event := github.PullRequestEvent{}
	if err := json.Unmarshal(body, &event); err != nil {
		logwarnf("Could not handle webhook: %s", err)
		return
	}
	switch *event.Action {
	case "opened":
		loginfof("opened pull request")
		g.trackPullRequest(app, &event)
	case "synchronize":
		loginfof("sync pull request")
		g.updatePullRequest(app, &event)
	case "closed":
		loginfof("closed pull request")
		g.closedPullRequest(app, &event)
	case "reopened":
		loginfof("reopened pull request")
		g.trackPullRequest(app, &event)
	}

}

func (g *Github) handleGithubPullRequestReviewEvent(app *githubApp, body []byte) {

}

func (g *Github) handleGithubPullRequestReviewComment(app *githubApp, body []byte) {}

func (g *Github) handleGithubPush(app *githubApp, body []byte) {
	event := github.WebHookPayload{} // badly named, is a new commit
	if err := json.Unmarshal(body, &event); err != nil {
		logwarnf("Could not handle webhook: %s", err)
		return
	}

	refs := strings.Split(*event.Ref, "/")
	branch := ""
	if len(refs) > 1 {
		branch = refs[len(refs)-1]
	} else {
		branch = refs[0]
	}

	if branch == "" {
		logcritf("Branch is nil, something went wrong, ref=%s", *event.Ref)
		return
	}

	commitHash := *event.HeadCommit.ID
	owner := *event.Repo.Owner.Name
	repoName := *event.Repo.Name

	g.m.Lock()
	defer g.m.Unlock()

	foundBranch := false
	for _, trackedBranch := range app.config.BuildBranches {
		if trackedBranch == branch {
			foundBranch = true
			break
		}
	}
	if foundBranch == false {
		logwarnf("Branch %s is not buildBranch, add this branch to the buildBranches config to build this branch", branch)
		return
	}

	for _, build := range g.trackedBuilds {
		if build.Config().GetMetadata("github:BranchBuild") == branch &&
			build.Config().GetMetadata("github:BranchBuildRepo") == repoName &&
			build.Config().GetMetadata("github:BranchBuildOwner") == owner &&
			build.Config().GetMetadata("github:BranchBuildCommit") == commitHash {
			// commit already tracked and building
			return
		}
	}

	// if we get here, we should build this commit, fo sho
	buildConfig := core.NewBuildConfig()
	buildConfig.Title = fmt.Sprintf("%s(%s):%s branch build", repoName, branch, commitHash)
	buildConfig.URL = *event.Compare
	buildConfig.BaseRepo = *event.Repo.SSHURL
	buildConfig.BaseBranch = branch
	buildConfig.BaseHash = commitHash
	buildConfig.Group = branch

	buildConfig.SetMetadata("github:BuildType", "commit")
	buildConfig.SetMetadata("github:BranchBuild", branch)
	buildConfig.SetMetadata("github:BranchBuildRepo", repoName)
	buildConfig.SetMetadata("github:BranchBuildOwner", owner)
	buildConfig.SetMetadata("github:BranchBuildCommit", commitHash)

	_, err := app.app.NewBuild(buildConfig.Group, buildConfig)
	if err != nil {
		logcritf("Couldn't start build for %s(%s):%s", repoName, branch, commitHash)
		return
	}
	loginfof("started build: %s(%s):%s", repoName, branch, commitHash)
}
