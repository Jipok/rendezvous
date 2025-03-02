#!/usr/bin/env bash
CGO_ENABLED=0 go build -ldflags "-s -w" && upx rendezvous-server
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o rendezvous-server-arm64 -ldflags "-s -w" && upx rendezvous-server-arm64
