VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
BIN      = seedkit

.PHONY: all web go build run test lint release docker clean

all: build

web:                       ## build the Vue frontend into web/dist
	cd web && npm ci && npm run build

go:                        ## build the Go binary (uses whatever is in web/dist)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/seedkit

build: web go              ## full build: frontend then backend

run:                       ## dev run with local data dir
	SEEDKIT_DATA=./data go run ./cmd/seedkit serve

test:
	go test ./...

lint:
	go vet ./...

release:                   ## cross-compile into dist/
	./scripts/release.sh $(VERSION)

docker:
	docker build -f build/Dockerfile --build-arg VERSION=$(VERSION) -t seedkit:$(VERSION) .

clean:
	rm -rf $(BIN) $(BIN).exe dist web/dist/*
