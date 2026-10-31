VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
BIN      = seedkit

.PHONY: all build run test lint icons release docker clean

all: build

build:                     ## build the static binary (frontend is embedded from web/static, no npm)
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/seedkit

run:                       ## dev run with local data dir
	SEEDKIT_DATA=./data go run ./cmd/seedkit serve

icons:                     ## regenerate web/static/vendor/lucide.svg from icons referenced in index.html/app.js
	micromamba run -n base python scripts/icons.py

test:
	go test ./...

lint:
	go vet ./...

release:                   ## cross-compile into dist/
	./scripts/release.sh $(VERSION)

docker:
	docker build -f build/Dockerfile --build-arg VERSION=$(VERSION) -t seedkit:$(VERSION) .

clean:
	rm -rf $(BIN) $(BIN).exe dist
