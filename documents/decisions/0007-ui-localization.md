# ADR 0007 — The UI renders in the visitor's language

- **Date:** 2026-08-07
- **Status:** Accepted
- **Scope:** `internal/i18n` (`Locale`, `Load`, `WithLocale`, `FromContext`, `Parse`, `Negotiate`,
  `T`, `N`, `Fallback`, `CookieName`, `locales/{es,en}.yml`), `internal/middleware/locale.go`,
  `internal/ui/language`, the language switcher and `<html lang>` in `internal/ui/layout`,
  `messages.go` in `internal/{appointment,patient,professional,slot,prescription}`,
  `Status.LabelKey` in `internal/appointment/appointment.go`,
  `internal/ui/static/js/timezone.js`, and `DEFAULT_LOCALE` in `cmd/server/config.go`

## Context

The UI was written entirely in English — around 146 strings across the nine `.templ` files and
another hundred messages built in Go handlers. The system is being sold to a kinesiología in CABA
whose staff speaks Spanish, which made this a blocker for the first release.

Translating the strings is the mechanical part and it is visible in the diff. What follows are the
choices that were live options and stop being visible once the code compiles.

## Decision 1 — ctxi18n, contained behind `internal/i18n`

Hand-rolling this is roughly 250 lines plus tests: nested YAML catalogs, `%{name}` interpolation,
`one`/`other` plural selection, and Accept-Language negotiation. `github.com/invopop/ctxi18n` is
the integration templ itself documents and it carries the context plumbing that templ's implicit
`ctx` needs. It is also `v0.9.0`, last released 2024-12-04, pre-1.0 and with no API stability
guarantee.

The containment is what makes that acceptable: **no `.templ` file and no handler imports ctxi18n**.
Every call site goes through `appointment-manager/internal/i18n`, so the roughly 250 copy lookups
depend on one file. Replacing the library is an afternoon of rewriting `i18n.go`, not a sweep.
Naming the facade `i18n` keeps the call sites as short as the library's own would have been.

The facade already earns its keep in one place: **Accept-Language is negotiated with
`golang.org/x/text/language`, not with `ctxi18n.Match`.** `ctxi18n.Match` compares codes verbatim.
It ignores q-values, so `en;q=0.1,es;q=0.9` picks English, and it never falls back from a region to
its base language, so `es-AR` — which is exactly what an Argentine browser sends — matches nothing
at all. For this product that is not an edge case, it is the common case.

## Decision 2 — The choice lives in a cookie, not in Postgres

The alternatives were a column on `session` and a column on `assistant`. Both lose to one fact:
**`/login` has to be translated too, and there is no session and no assistant yet when it renders.**
A cookie is the only store that exists before authentication, and it is also the only one that
survives a logout, which is the behaviour a shared clinic machine wants.

ADR 0006 Decision 3 independently refuses to widen the session row with anything that is not the
assistant's identity, so that option was closed anyway.

Resolution order is **cookie → `Accept-Language` → `DEFAULT_LOCALE`**: an explicit choice outranks
the browser, and a cookie that fails to parse degrades to negotiation rather than to nothing.

Two consequences follow and are deliberate:

- HTML responses carry `Vary: Cookie, Accept-Language`. The output genuinely varies on both, and
  there is a Cloudflare proxy planned in front of this.
- The preference is per-browser, not per-assistant. Clinic staff share a handful of machines and
  nobody expects their language to follow them to a different computer.

## Decision 3 — Two different fallbacks, and a load that never fails

`DEFAULT_LOCALE` is configurable and answers "what does a request that expresses no preference
get" — a deployment policy. `i18n.Fallback` is a constant `es` and answers "what does a lookup with
no locale in its context get" — a bug guard. Collapsing them into one knob would mean a typo in an
environment variable can turn every render that bypassed the middleware into a missing-translation
marker. An unsupported `DEFAULT_LOCALE` is rejected at startup instead.

`Load` is idempotent through `sync.OnceValue`, and `WithLocale` calls it on every use. `run()` also
calls it at startup, but only so a broken catalog fails there rather than on the first page view.

That call inside `WithLocale` is load-bearing. Introducing `i18n.T` into `base.templ` immediately
put ctxi18n's literal `!(MISSING LOCALE)` marker into every page rendered by a test package that
had never called `Load` — including inside an `aria-label`. `TestBaseNeverRendersTheMissingLocaleMarker`
lives in `internal/ui/layout`, a package that deliberately never loads the catalogs, and it fails
if the `_ = Load()` line is removed.

## Decision 4 — The locale middleware is registered through `Guard`, never in the `Chain`

This is the detail most likely to be "simplified" and the one nothing would visibly catch.

`middleware.Chain` applies its list inside-out, so a middleware added there sits *inside* `Metrics`
and `RequestLogger`. The locale middleware calls `r.WithContext`, which hands the mux a **copy** of
the request. The mux stamps `r.Pattern` on that copy. The observability middlewares are still
holding the original, and see no pattern at all — so the Prometheus `route` label collapses to `/`
and every endpoint in the service lands in one bucket. It is precisely the bug `middleware.Guard`
was written to prevent, documented in its own doc comment, and it has been fixed here once already.

Hence three guards rather than two. `apiProtected` and `uiProtected` existed; `publicUI` is new,
because `/login` and `/logout` register on the raw mux and still need a locale.

`guard_wiring_test.go` asserts `r.Pattern` survives the new guard. No test asserts the Prometheus
label itself, so the reasoning has to live here.

## Decision 5 — Go errors stay English; the UI maps sentinels to catalog keys

Error strings are for logs and for `/api/v1/*`. They are not translated, and translating them would
be the wrong fix. Instead each of the five domain packages carries a `messages.go` with one named
constant per catalog key, and the UI edge maps a sentinel to a key with `errors.Is` — the
`validationErrorKey` functions. Where a message interpolates a limit, that function returns the key
and an `i18n.M` alongside it.

This closed a bug that predated the translation work. `internal/slot/handler.go` and
`internal/patient/handler.go` rendered `err.Error()` straight into the snackbar, so a user saw
`validate slot: end time must be after start time` — a wrapped Go error, wrong in English too, not
merely untranslated.

Adding a new domain error therefore means adding a key and a `case` to the mapping. It does not
mean touching the error's text.

## Decision 6 — `/api/v1/*` is machine-facing and stays English

RFC 9457 Problem Details are consumed by code, so they keep their English text and `web.WriteProblem`
was left alone.

One artefact of that boundary looks like an oversight and is not: **`listProfessionalsQuery` still
wraps the specialty in `INITCAP()` while `listAllProfessionalsQuery`, immediately below it, does
not.** The first backs `GET /api/v1/professionals`, and keeping the cast keeps that response
byte-identical to what it has always returned. The second is read only by the dashboard, which
needs the stored slug so the UI can translate it. Unifying the two queries silently changes an API
payload.

`Repository.List` — the INITCAP one — is also read by the slots dashboard and the slot create form
(`internal/slot/handler.go`), which use only the names. Rendering its specialty anywhere would hand
`SpecialtyLabel` a capitalized value that `specialtyLabelKey` does not match, so it would fall
through to the raw string and never translate. Read specialties through `ListAll`/`GetByID`.

## Decision 7 — Display labels are resolved in Go, not read from lookup tables

The appointments grid used to show `INITCAP(appointment_status.name)`. A value stored in the
database cannot follow the request locale, so `Status.LabelKey()` now returns a catalog key and the
join to `public.appointment_status` was dropped along with the column — the foreign key already
guarantees the status is valid, so the join bought nothing else.

Consequence worth knowing: `appointment_status.name` is now read by nothing. The comment in
`internal/db/migrations/000011_appointment_status_cancelled_by_clinic.up.sql` explaining that the
name is spaced rather than underscored "because the appointments grid renders it through INITCAP()"
is stale. The migration has been applied and is left untouched.

Professional specialty gets the same treatment, with a fallback to the raw stored value: the column
is CHECK-constrained to `kinesiology` today, and a specialty added tomorrow without copy should show
its slug rather than a missing-translation marker.

Health insurance names stay exactly as they are stored. OSDE, Swiss Medical and Galeno are proper
nouns and translating them would be a defect, not a feature.

## Decision 8 — Dates follow the chosen language; the timezone still follows the browser

`timezone.js` formats through `document.documentElement.lang || navigator.language`. Before this,
choosing Spanish in an English browser produced Spanish copy next to `mm/dd/yyyy` dates.

The timezone is deliberately still read from the browser. Where the visitor is standing is a fact
about them, not a preference they expressed, and the server has always refused to assume it.

## Revisit when

- **A third language is added.** `Negotiate` maps the matcher's result by *position* — index 0 is
  Spanish, index 1 is English. The `matcher` tag slice and that switch have to change together and
  in the same order. Nothing enforces the correspondence.
- **A per-assistant preference is actually wanted.** That is a column on `assistant`, read after
  login and written into the same cookie — not a widening of `session`, for the reason in ADR 0006.
- **The catalogs stop fitting one file per locale.** The embed is `locales/*.yml`, so splitting them
  by domain needs no code change; it is only a question of when the single file becomes hard to
  review.
- **A right-to-left or plural-rich language appears.** `i18n.N` already handles `one`/`other`, but
  nothing in the layout is direction-aware, and languages with more than two plural forms would need
  the catalogs restructured.