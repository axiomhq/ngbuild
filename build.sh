#!/bin/bash

set -e
# never use system installed gopath
GOPATH="`pwd`"

# never make a directory called src, i will destroy it and your livelyhood
rm -rf src

go get -u github.com/stretchr/testify

mkdir -p src/github.com/watchly/
ln -s "`pwd`" "`pwd`/src/github.com/watchly/ngbuild"
cd src/github.com/watchly/ngbuild

go test -v -race github.com/watchly/ngbuild
go test -v -race github.com/watchly/ngbuild/core
go test -v -race github.com/watchly/ngbuild/integrations/github
go test -v -race github.com/watchly/ngbuild/integrations/slack
go test -v -race github.com/watchly/ngbuild/integrations/web
