#!/bin/bash
go build
docker build . -t docker-dev-local.art.local/library/govcsim:latest
