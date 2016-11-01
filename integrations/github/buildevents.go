package github

import (
	"fmt"

	"github.com/google/go-github/github"
	"github.com/watchly/ngbuild/core"
)

func (g *Github) updateBuildStatus(app core.App, build core.Build) {
	// update github status
	buildToken := build.Token()
	baseRepo := build.Config().GetMetadata("github:BaseRepo")
	baseOwner := build.Config().GetMetadata("github:BaseOwner")
	headCommit := build.Config().GetMetadata("github:HeadHash")

	if baseRepo == "" || baseOwner == "" || headCommit == "" {
		logwarnf("Couldn't extract github info from: %s", buildToken)
		return
	}

	var state string
	var description string
	if build.HasStopped() {
		if code, err := build.ExitCode(); err != nil {
			state = "error"
			description = fmt.Sprintf("I am error")
		} else if code != 0 {
			state = "failure"
			description = fmt.Sprintf("Failed with exit code: %d", code)
		} else {
			state = "success"
			description = fmt.Sprintf("Succeeded, well done you!")
		}
	} else {
		state = "pending"
		description = fmt.Sprintf("Build started")
	}

	webStatusURL := build.WebStatusURL()
	context := fmt.Sprintf("NGBuildService/github/%s", app.Name())
	commitStatus := &github.RepoStatus{
		State:       &state,
		TargetURL:   &webStatusURL,
		Description: &description,
		Context:     &context,
	}

	_, _, err := g.client.Repositories.CreateStatus(baseOwner, baseRepo, headCommit, commitStatus)
	if err != nil {
		logcritf("Couldn't set status for %s/%s:%s, %s", baseOwner, baseRepo, headCommit, err)
	}

}

func (g *Github) onBuildStarted(data map[string]string) {
	g.m.Lock()
	defer g.m.Unlock()
	loginfof("build started")
	buildToken := data["token"]
	appName := data["app"]
	app := g.apps[appName]

	if app == nil {
		logcritf("Couldn't find app `%s`", appName)
		return
	}

	build, err := app.app.GetBuild(buildToken)
	if err != nil {
		logcritf("Couldn't get build `%s`: %s", buildToken, err)
		return
	}
	build.Ref()

	g.updateBuildStatus(app.app, build)
}

func (g *Github) onBuildFinished(data map[string]string) {
	g.m.Lock()
	defer g.m.Unlock()

	buildToken := data["token"]
	appName := data["app"]
	app := g.apps[appName]

	if app == nil {
		logcritf("Couldn't find app `%s`", appName)
		return
	}

	build, err := app.app.GetBuild(buildToken)
	if err != nil {
		logcritf("Couldn't get build `%s`: %s", buildToken, err)
		return
	}
	build.Unref()

	g.updateBuildStatus(app.app, build)
}
