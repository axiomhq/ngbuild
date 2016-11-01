package github

import (
	"encoding/json"

	"github.com/google/go-github/github"
)

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
}
