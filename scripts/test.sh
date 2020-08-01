#!/bin/bash -e

echo "Building testing binary and running tests..."
#Get into the right directory
cd $(dirname $0)

export GOOS=""
export GOARCH=""

#Add this directory to PATH
export PATH="$PATH:`pwd`"

go build -o "alertmanager_gotify_bridge" ../

echo "Running tests..."
cd ../

go test
