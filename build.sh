#!/bin/bash

set -e

# never make a directory called src, i will destroy it and your livelyhood
rm -rf src
mkdir -p src/github.com/watchly/
ln -s "`pwd`" "`pwd`/src/github.com/watchly/ngbuild"
GOPATH=$GOPATH:`pwd`

go get -u github.com/watchly/ngbuild
go get -u github.com/stretchr/testify

go test -v -race ./...
