TAILWIND_VERSION := v4.3.2
HTMX_VERSION     := 2.0.10
ALPINE_VERSION   := 3.15.12

TAILWIND_BIN := bin/tailwindcss

UNAME_OS   := $(shell uname -s)
UNAME_ARCH := $(shell uname -m)

ifeq ($(UNAME_OS),Linux)
  TW_OS := linux
else ifeq ($(UNAME_OS),Darwin)
  TW_OS := macos
else
  $(error unsupported OS $(UNAME_OS) for tailwindcss download)
endif

ifeq ($(UNAME_ARCH),x86_64)
  TW_ARCH := x64
else ifeq ($(UNAME_ARCH),arm64)
  TW_ARCH := arm64
else ifeq ($(UNAME_ARCH),aarch64)
  TW_ARCH := arm64
else
  $(error unsupported architecture $(UNAME_ARCH) for tailwindcss download)
endif

TAILWIND_URL := https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TW_OS)-$(TW_ARCH)

.PHONY: css check-css vendor-js

$(TAILWIND_BIN):
	mkdir -p bin
	curl -fsSL -o $(TAILWIND_BIN) $(TAILWIND_URL)
	chmod +x $(TAILWIND_BIN)

css: $(TAILWIND_BIN) ## Regenerate internal/ui/static/css/app.css from internal/ui/css/input.css
	$(TAILWIND_BIN) -i internal/ui/css/input.css -o internal/ui/static/css/app.css --minify

check-css: css ## Fail if app.css is stale relative to .templ sources (used by lefthook)
	git diff --exit-code -- internal/ui/static/css/app.css

vendor-js: ## One-off: re-download pinned htmx/Alpine builds (only rerun when bumping HTMX_VERSION/ALPINE_VERSION)
	curl -fsSL -o internal/ui/static/vendor/htmx.min.js \
		https://unpkg.com/htmx.org@$(HTMX_VERSION)/dist/htmx.min.js
	curl -fsSL -o internal/ui/static/vendor/alpine.min.js \
		https://unpkg.com/alpinejs@$(ALPINE_VERSION)/dist/cdn.min.js
