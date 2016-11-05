#!/bin/bash

set -e
go get -u github.com/watchly/ngbuild
go get -u github.com/stretchr/testify

go test -v -race ./...
