package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/watchly/ngbuild/slack"
)

var (
	hostname     = flag.String("hostname", "", "Domain name of the build server")
	clientID     = flag.String("clientID", "", "Slack OAuth Client ID")
	clientSecret = flag.String("clientSecret", "", "Slack OAuth Client Secret")
)

func main() {
	flag.Parse()

	if *hostname == "" || *clientID == "" || *clientSecret == "" {
		fmt.Println("Slack OAuth credentials and hostname required")
	}

	slack.New(*hostname, *clientID, *clientSecret)

	if err := http.ListenAndServe(":http", nil); err != nil {
		fmt.Println(err.Error())
	}
}
