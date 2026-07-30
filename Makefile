BINARY    := wallfacer
MODULE    := github.com/pradipta/wallfacer
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X $(MODULE)/cmd.Version=$(VERSION)
PLATFORMS := darwin-arm64 darwin-amd64 linux-arm64 linux-amd64

.PHONY: build install test vet fmt clean release brew-formula

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) .

install:
	go install -ldflags '$(LDFLAGS)' .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf $(BINARY) dist/

# Cross-compiled release archives (pure Go, no CGO needed). One tarball per
# platform, each holding the binary under its plain name plus the README and
# LICENSE, and a checksums.txt over the lot: Homebrew installs from an archive
# and pins it by sha256, so both are part of the release, not extras.
release: clean
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		echo "  $(BINARY)-$$p"; \
		GOOS=$${p%-*} GOARCH=$${p#*-} go build -ldflags '$(LDFLAGS)' -o dist/$(BINARY) . || exit 1; \
		COPYFILE_DISABLE=1 tar -czf dist/$(BINARY)-$$p.tar.gz -C dist $(BINARY) -C .. README.md LICENSE || exit 1; \
		rm -f dist/$(BINARY); \
	done
	@cd dist && { command -v sha256sum >/dev/null && sha256sum *.tar.gz || shasum -a 256 *.tar.gz; } > checksums.txt
	@ls -lh dist/

# Rewrite the Homebrew formula to point at the archives in dist/. Needs
# `make release` first — the formula carries their checksums. Written via a temp
# file so a failed generation can't leave a half-written tap behind.
brew-formula:
	@mkdir -p HomebrewFormula
	@scripts/brew-formula.sh $(VERSION) > HomebrewFormula/.$(BINARY).rb.tmp
	@mv HomebrewFormula/.$(BINARY).rb.tmp HomebrewFormula/$(BINARY).rb
	@echo "HomebrewFormula/$(BINARY).rb -> $(VERSION)"
