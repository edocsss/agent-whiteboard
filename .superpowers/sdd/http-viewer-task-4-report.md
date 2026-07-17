# HTTP/viewer Task 4 report

## Status

- Status: complete
- Base: `913544900b8fb9ab259bbcbddda56aea8c49d6eb`
- Implementation head: `9177d119fcc5e6b8ce2ef8fb980b19dc4a998920`
- Implementation commit: `feat: bundle browser whiteboard renderer`

## Files

- `.gitignore`
- `package.json`
- `pnpm-lock.yaml`
- `pnpm-workspace.yaml`
- `internal/assets/assets.go`
- `internal/assets/assets_test.go`
- `internal/assets/src/viewer.js`
- `internal/assets/src/viewer.css`
- `internal/assets/src/viewer.test.js`
- `internal/assets/scripts/build.mjs`
- `internal/assets/scripts/check.mjs`
- `internal/assets/dist/viewer.min.js`
- `internal/assets/dist/viewer.min.css`
- `internal/assets/manifest.json`

`pnpm-workspace.yaml` is retained for the pinned single-package workspace because pnpm 11.4 records its esbuild build-script authorization there (`allowBuilds.esbuild: true`). `node_modules`, pnpm store data, and browser-test output directories are ignored and were not committed.

## Toolchain and dependencies

- Node.js: `v24.14.0` from `/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin`
- Corepack dispatcher: `0.34.5`
- pnpm: `11.4.0`
- Runtime libraries: markdown-it `14.2.0`, DOMPurify `3.4.12`, Mermaid `11.15.0`, highlight.js `11.11.1`
- Build/test libraries: esbuild `0.28.1`, Vitest `4.1.10`, jsdom `29.1.1`

The host `/usr/local/bin/corepack` shim has stale signing keys and is incompatible with Node 24 dynamic imports. Every pnpm command was still executed under the required bundled Node 24.14.0, using npm-exec Corepack 0.34.5 solely as the dispatcher to the exact pinned pnpm 11.4.0 release.

## TDD evidence

### RED

1. After the pinned install and before viewer implementation, `pnpm test` failed in import analysis because `internal/assets/src/viewer.js` did not exist.
2. Before Go embedding implementation, `go test ./internal/assets -v` failed to compile with undefined `ViewerJS`, `ViewerCSS`, and `Manifest` accessors.
3. The first embedded-asset GREEN attempt exposed literal `http://` and `https://` markers in full Mermaid library strings. The build now deterministically represents those slashes with equivalent JavaScript escapes and the regression test rejects literal external-reference markers.

### GREEN

- Vitest: 1 file passed, 15 tests passed.
- Go asset tests: version/hash verification, no external runtime references, non-empty assets, and mutation isolation all passed.
- Mermaid is the only mocked runtime module in JavaScript tests; markdown-it and DOMPurify are real.

## Deterministic asset evidence

- `pnpm build` generated the committed JavaScript, CSS, and manifest.
- `pnpm run check:assets` rebuilt all three files in a fresh temporary directory, byte-compared them, printed `browser assets match deterministic build`, and removed the temporary directory in `finally`.
- `node --check internal/assets/dist/viewer.min.js` passed after URL-string escaping and template-literal downleveling.
- The manifest records all four runtime versions, esbuild's version, and SHA-256 hashes for both bundles.
- A direct scan found no literal `http://`, `https://`, `<script src`, or stylesheet-link reference in either committed bundle or the manifest.

## Verification

- Frozen install: PASS (`pnpm install --frozen-lockfile`)
- JavaScript tests: PASS (15/15)
- JavaScript build: PASS
- Generated asset drift check: PASS
- Generated JavaScript syntax check: PASS
- `go test ./internal/assets -v`: PASS
- `go test -race ./internal/assets`: PASS
- `go test ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `gofmt -l internal/assets/assets.go internal/assets/assets_test.go`: no output
- `git diff --check` and `git diff --cached --check`: PASS before commit

## Self-review

- Confirmed the required pipeline order: JSON source, markdown-it with raw HTML disabled, DOMPurify, DOM insertion, highlight.js, strict Mermaid initialization, per-placeholder Mermaid render, SVG-profile DOMPurify, insertion.
- Confirmed generated task-list checkboxes do not enable raw Markdown HTML.
- Confirmed first-H1 title behavior, fallback title, allowed theme normalization, localStorage key restriction, system-mode live subscription, retained Mermaid source, all-diagram theme re-render, isolated errors, and the complete Mermaid `secure` list.
- Confirmed Go accessors clone cached embedded bytes on every call.
- Hardened `check.mjs` so both missing and byte-different committed outputs are reported by filename.
- The first staged diff check caught third-party template-literal whitespace in the generated bundle. The build now downlevels template literals deterministically instead of trimming potentially semantic string content; all build, syntax, drift, browser-unit, and Go checks were rerun afterward.

## Concerns and deferrals

- No product or test blocker remains.
- The host Corepack shim issue is environmental and documented above; pinned Node and pnpm execution was preserved.
- Third-party notices are intentionally deferred to release Plan Task 5 per the task instruction. No `licenses` or notice artifact was added here.
- Whiteboard shell and HTTP handler behavior were intentionally not added in this task.
