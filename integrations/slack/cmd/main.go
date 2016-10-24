package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/watchly/ngbuild/core"
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

	app := core.NewApp("ngbuild-slack")
	build := core.NewBuild("pulls/24", nil)

	s := slack.New(*hostname, *clientID, *clientSecret)
	s.BuildSucceeded(app, build)
	//s.BuildFailed(nil, nil)

	if err := http.ListenAndServe(":http", nil); err != nil {
		fmt.Println(err.Error())
	}
}
