package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/watchly/ngbuild/integrations/slack"
)

var (
	hostname     = flag.String("hostname", "", "domain name of the build server")
	clientID     = flag.String("clientID", "", "slack oauth client ID")
	clientSecret = flag.String("clientSecret", "", "slack oauth client secret")
)

func main() {
	flag.Parse()

	if *hostname == "" || *clientID == "" || *clientSecret == "" {
		fmt.Println("Slack OAuth credentials and hostname required")
	}

	// FIXME: Uncomment when fixed
	// app := core.NewApp("ngbuild-slack")
	// token, _ := app.NewBuild("pulls/24", nil)
	// build, _ := app.GetBuild(token)

	slack.New()

	// s.BuildFailed(app, build)

	if err := http.ListenAndServe(":http", nil); err != nil {
		fmt.Println(err.Error())
	}
}
