---
name: frontend-asset-update
description: Regenerate or update self-hosted frontend assets (Tailwind CSS, htmx, Alpine.js) in appointment-manager. Use when editing Tailwind utility classes in any .templ file, or when bumping the pinned htmx/Alpine versions. Do not use CDN tags; all assets are vendored and committed.
tools: Bash, Read, Edit
---

# Frontend Asset Update

`appointment-manager` self-hosts Tailwind CSS, htmx, and Alpine.js under
`internal/ui/static/{css,vendor}/`. Never reintroduce CDN `<script src="https://...">`
or `<link href="https://...">` tags in `.templ` files.

Pinned versions: Tailwind CSS v4.3.2, htmx v2.0.10, Alpine.js v3.15.12.

## When you changed Tailwind classes in a `.templ` file

Tailwind source lives at `internal/ui/css/input.css`; the generated, **committed**
output is `internal/ui/static/css/app.css` (same convention as `_templ.go`).

1. Regenerate the CSS:
   ```bash
   make css
   ```
2. Stage the regenerated file:
   ```bash
   git add internal/ui/static/css/app.css
   ```
3. Before committing, `lefthook` pre-commit runs `make check-css` automatically
   whenever a commit touches `.templ` files, and fails the commit if `app.css`
   is stale relative to the templ sources. If that happens: rerun `make css`,
   re-stage, and re-attempt the commit — don't bypass with `--no-verify`.

## When bumping the pinned htmx or Alpine.js version

1. Update `HTMX_VERSION` / `ALPINE_VERSION` in the `Makefile` first.
2. Re-download the pinned builds:
   ```bash
   make vendor-js HTMX_VERSION=<new> ALPINE_VERSION=<new>
   ```
3. **Review the diff** of the downloaded files (`internal/ui/static/vendor/htmx.min.js`,
   `internal/ui/static/vendor/alpine.min.js`) before committing — these are
   minified third-party files fetched over the network at build time, so eyeball
   the diff size/shape looks like a legitimate version bump, not something else.
4. Update the "Pinned versions" note in `AGENTS.md` §0 to match.

## Notes

- A fresh clone needs no extra setup step: `app.css` and the vendored JS files
  are already committed, so `go run ./cmd/server` serves working CSS/JS
  immediately. `make css` is only needed when you actively change Tailwind
  classes; `make vendor-js` is only needed when bumping pinned versions.
