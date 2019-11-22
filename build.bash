#!/bin/bash
set -ex
export GO111MODULE=on
go build -tags netgo -o ndt7-client-bin -v .
