# Tern's build and release, the same shape as ../stilday: `make app` builds the
# macOS app for this machine, `make release V=0.1.1` bumps mygo.json, builds the
# disk image, tags it and publishes the tag as a GitHub release. Only the
# machine's own macOS build is produced for now.

APP := Tern
OS := $(shell go env GOOS)
ARCH := $(shell go env GOARCH)
VERSION := $(shell sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' mygo.json)

# The version the release is cut for. `make release V=…` writes it into
# mygo.json first, so the bundle, the tag and the disk image agree.
V ?=

# Empty signs ad hoc, which is what a local build needs. Set SIGN to a
# Developer ID to ship outside this machine:
#   make release V=0.1.1 SIGN="Developer ID Application: Name (TEAMID)"
SIGN ?=
SIGNFLAG := $(if $(filter-out -,$(SIGN)),-sign "$(SIGN)",)

.PHONY: app icons dict release

# MyGo takes the bundle's icon from resources/icon.png, and the development
# build's from resources/icon-dev.png beside it; both come from the generator.
icons:
	go run ./tools/icon
	@mkdir -p resources
	@cp assets/brand/png/icon-1024.png resources/icon.png
	@cp assets/brand/png/icon-dev-1024.png resources/icon-dev.png

# The offline dictionary: ECDICT's SQLite dump, cut down to the words a reader
# meets (about 59k headwords and 56k forms, 12 MB). Its output is committed, so
# this only needs running to refresh the data.
ECDICT ?= .cache/ecdict/stardict.db

dict:
	@mkdir -p $(dir $(ECDICT))
	@test -f "$(ECDICT)" || curl -fL --retry 3 -o .cache/ecdict-sqlite-28.zip \
		https://github.com/skywind3000/ECDICT/releases/download/1.0.28/ecdict-sqlite-28.zip
	@test -f "$(ECDICT)" || unzip -oq .cache/ecdict-sqlite-28.zip -d $(dir $(ECDICT))
	go run ./tools/dict -source "$(ECDICT)" -out resources/dictionary.db

# A local bundle to look at, without the disk image.
app: icons
	go tool mygo build -skip-dmg $(SIGNFLAG)
	@echo "build/$(OS)-$(ARCH)/$(APP).app"

# Bump, build, tag, push, publish. Re-running after a failed build is safe:
# the version bump is only committed when it differs, and the tag check stops
# a second release of the same version.
release: icons
	@test -n "$(V)" || (echo "usage: make release V=0.1.1" >&2; exit 1)
	@test "$$(git rev-parse --abbrev-ref HEAD)" = main || (echo "release from main" >&2; exit 1)
	@test -z "$$(git status --porcelain)" || (echo "the working tree is not clean; commit first" >&2; exit 1)
	@! git rev-parse -q --verify "refs/tags/v$(V)" >/dev/null || (echo "tag v$(V) already exists" >&2; exit 1)
	@sed -i '' 's/"version": *"[^"]*"/"version": "$(V)"/' mygo.json
	@git diff --quiet -- mygo.json || (git add mygo.json && git commit -m "Tern $(V)")
	go test ./...
	go tool mygo build $(SIGNFLAG)
	git tag -a "v$(V)" -m "Tern $(V)"
	git push origin main
	git push origin "v$(V)"
	gh release create "v$(V)" "build/$(OS)-$(ARCH)/$(APP) $(V) $(ARCH).dmg" \
		--title "Tern $(V)" --generate-notes
