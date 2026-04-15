.PHONY: build test coverage clean cross install tidy lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	docker compose run --rm dev go build -ldflags="$(LDFLAGS)" -o bin/probe .

test:
	docker compose run --rm dev go test ./... -v -race -count=1

test-integration:
	docker compose run --rm dev go test ./... -v -race -count=1 -tags=integration

coverage:
	docker compose run --rm dev go test -coverprofile=coverage.out ./...
	docker compose run --rm dev go tool cover -func=coverage.out

tidy:
	docker compose run --rm dev go mod tidy

lint:
	docker compose run --rm dev go vet ./...

clean:
	rm -rf bin/

UNAME_S := $(shell uname -s)
ifeq ($(findstring MINGW,$(UNAME_S)),MINGW)
  GOOS ?= windows
else ifeq ($(findstring MSYS,$(UNAME_S)),MSYS)
  GOOS ?= windows
else ifeq ($(findstring Darwin,$(UNAME_S)),Darwin)
  GOOS ?= darwin
else
  GOOS ?= linux
endif
GOARCH ?= $(if $(filter arm64 aarch64,$(shell uname -m)),arm64,amd64)
EXT := $(if $(filter windows,$(GOOS)),.exe,)

ifeq ($(GOOS),windows)
  INSTALL_DIR ?= $(LOCALAPPDATA)/Programs/probe
else
  INSTALL_DIR ?= $(HOME)/.local/bin
endif

install:
	docker compose run --rm dev sh -c "CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags='$(LDFLAGS)' -o bin/probe$(EXT) ."
	@mkdir -p "$(INSTALL_DIR)"
	cp bin/probe$(EXT) "$(INSTALL_DIR)/probe$(EXT)"
	@echo "installed probe $(VERSION) ($(GOOS)/$(GOARCH)) to $(INSTALL_DIR)/probe$(EXT)"

cross:
	docker compose run --rm dev sh -c "\
		CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags='$(LDFLAGS)' -o bin/probe-linux-amd64   . && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags='$(LDFLAGS)' -o bin/probe-linux-arm64   . && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags='$(LDFLAGS)' -o bin/probe-darwin-amd64  . && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags='$(LDFLAGS)' -o bin/probe-darwin-arm64  . && \
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags='$(LDFLAGS)' -o bin/probe-windows-amd64.exe ."

CURRENT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)
MAJOR := $(shell echo $(CURRENT_TAG) | sed 's/^v//' | cut -d. -f1)
MINOR := $(shell echo $(CURRENT_TAG) | sed 's/^v//' | cut -d. -f2)
PATCH := $(shell echo $(CURRENT_TAG) | sed 's/^v//' | cut -d. -f3)

release-patch:
	@NEXT=v$(MAJOR).$(MINOR).$(shell echo $$(($(PATCH)+1))); \
	git tag $$NEXT && git push origin HEAD $$NEXT && echo "released $$NEXT"

release-minor:
	@NEXT=v$(MAJOR).$(shell echo $$(($(MINOR)+1))).0; \
	git tag $$NEXT && git push origin HEAD $$NEXT && echo "released $$NEXT"
