# awst Containers (Firefox extension)

Opens each AWS profile's console in its own isolated [Firefox Multi-Account
Container](https://support.mozilla.org/kb/containers), so multiple accounts stay
logged in at once. `awst console` emits `ext+awst-containers:` URLs that this
extension intercepts.

It's a clean-room, build-free reimplementation of the same approach as the
MIT-licensed [`common-fate/granted-containers`](https://github.com/common-fate/granted-containers).
awst previously relied on that published extension; this is awst's own
replacement so we no longer depend on it.

## Files

- `manifest.json` — MV2 manifest; declares the `ext+awst-containers` protocol
  handler, the stable add-on id `awst-containers@kedwards.github.io`, and the
  four container permissions (`contextualIdentities`, `cookies`, `tabs`,
  `storage`).
- `open.html` / `open.js` — the handler page: parses the protocol URL,
  finds-or-creates the named container, opens the target URL in it, self-closes.

No build step, no toolchain — plain JS.

## Develop / load unsigned

Firefox Developer Edition / Nightly (or stable with
`xpinstall.signatures.required=false`):

1. `about:debugging` ▸ **This Firefox** ▸ **Load Temporary Add-on…**
2. Pick `extension/manifest.json`.

Temporary add-ons are removed on restart — fine for development.

## Build & sign (release)

Stable/ESR/Developer Firefox require a Mozilla-signed XPI. Signing needs a
[Mozilla add-on developer account](https://addons.mozilla.org/developers/) and
AMO API credentials.

```sh
# build an unsigned zip (lint included)
npx --yes web-ext build --source-dir=extension --artifacts-dir=dist

# sign as an unlisted (self-hosted) XPI — requires AMO credentials
WEB_EXT_API_KEY=…  WEB_EXT_API_SECRET=…  \
  npx --yes web-ext sign --source-dir=extension --channel=unlisted --artifacts-dir=dist
```

`task ext:sign` signs into `dist/`, then copies the result to the stable path
**`extension/awst-containers.xpi`** (web-ext's own filename is
`<amo-slug>-<version>.xpi`). That committed artifact is what releases ship:
`goreleaser` attaches `extension/awst-containers.xpi` to each tag (see
`release.extra_files` in `.goreleaser.yml`) — no AMO signing runs in CI.

So the release workflow is:

1. Change the extension → bump `version` in `manifest.json`.
2. `op run … -- task ext:sign` (or set `WEB_EXT_API_KEY`/`WEB_EXT_API_SECRET`).
3. Commit the updated `extension/awst-containers.xpi`.
4. Tag/release as usual — the XPI rides along as `awst-containers.xpi`.

Users install via `awst console --install-extension` (point
`AWST_EXTENSION_XPI` at the downloaded file) or by opening the XPI in Firefox.
