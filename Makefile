Version := $(shell git describe --tags --dirty)
# Version := "dev"
GitCommit := $(shell git rev-parse HEAD)
LDFLAGS := "-s -w -X github.com/alexellis/k3sup/cmd.Version=$(Version) -X github.com/alexellis/k3sup/cmd.GitCommit=$(GitCommit)"
export GO111MODULE=on
SOURCE_DIRS = cmd pkg main.go

.PHONY: all
all: gofmt test dist hash

.PHONY: test
test:
	CGO_ENABLED=0 go test $(shell go list ./... | grep -v /vendor/|xargs echo) -cover

.PHONY: gofmt
gofmt: 
	gofmt -l -s $(SOURCE_DIRS) ./ 

.PHONY: dist
dist:
	mkdir -p bin/
	rm -rf bin/k3sup*
	CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -ldflags $(LDFLAGS) -installsuffix cgo -o bin/k3sup
	CGO_ENABLED=0 GOOS=darwin go build -mod=vendor -a -ldflags $(LDFLAGS) -installsuffix cgo -o bin/k3sup-darwin
	GOARM=6 GOARCH=arm CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -ldflags $(LDFLAGS) -installsuffix cgo -o bin/k3sup-armhf
	GOARCH=arm64 CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -ldflags $(LDFLAGS) -installsuffix cgo -o bin/k3sup-arm64
	GOOS=windows CGO_ENABLED=0 go build -mod=vendor -a -ldflags $(LDFLAGS) -installsuffix cgo -o bin/k3sup.exe

.PHONY: hash
hash:
	rm -rf bin/*.sha256 && ./hack/hashgen.sh
