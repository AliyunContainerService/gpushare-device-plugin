# Definitions
# support  x86、arm macos or x86 linux
DockerBuild = docker build
DockerRun = docker run
ifeq ($(shell uname -p),arm)
	DockerBuild = docker buildx build --platform=linux/amd64
	DockerRun = docker run --platform=linux/amd64
endif

# Definitions
IMAGE                   := registry.cn-hangzhou.aliyuncs.com/acs/gpushare-device-plugin
GIT_VERSION             := $(shell git rev-parse --short=7 HEAD)
COMMIT_ID 				:= $(shell git describe --match=NeVeRmAtCh --abbrev=99 --tags --always --dirty)
GOLANG_DOCKER_IMAGE     := golang:1.19

build-inspect-plugin:
	go build -o bin/kubectl-inspect-gpushare-v2 cmd/inspect/*.go

build-image:
	${DockerBuild} -t ${IMAGE}:${GIT_VERSION} -f Dockerfile .
