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

	s := slack.New(*hostname, *clientID, *clientSecret)
	// s.BuildSucceeded()
	s.BuildFailed()

	if err := http.ListenAndServe(":http", nil); err != nil {
		fmt.Println(err.Error())
	}
}
