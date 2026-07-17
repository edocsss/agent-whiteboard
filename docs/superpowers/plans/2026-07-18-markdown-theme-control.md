# Markdown Viewer Theme Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an accessible top-left Theme button with System, Light, and Dark choices to the Markdown viewer.

**Architecture:** Keep theme state and Mermaid re-rendering inside `renderWhiteboard`. Add a small DOM controller in `viewer.js` that delegates every selection to the existing `setTheme` method. Style the control in the existing viewer CSS and verify it through unit and browser tests; no server or API changes.

**Tech Stack:** Vanilla DOM APIs, existing localStorage/media-query theme controller, CSS, Vitest/jsdom, Playwright, deterministic asset bundler.

## Global Constraints

- The control is a compact floating `Theme: <selection>` button in the top-left corner.
- Explicit options are System, Light, and Dark.
- The selected preference persists under `agent-whiteboard-theme`.
- System follows operating-system color-scheme changes.
- No server API, external resource, or new dependency is introduced.
- Mermaid diagrams re-render through the existing `setTheme(value)` path.

---

### Task 1: Add the theme menu controller and styles

**Files:**
- Modify: `internal/assets/src/viewer.js`
- Modify: `internal/assets/src/viewer.css`
- Test: `internal/assets/src/viewer.test.js`

**Interfaces:** The controller consumes the existing theme state and produces a `button[data-theme-control]`, a `[data-theme-menu]`, three `[data-theme-option]` buttons, and a `destroy()` cleanup path.

- [ ] **Step 1: Write the failing unit test**

Render Markdown and assert the trigger reads `Theme: System`, the menu is initially hidden, and the three options are present. Open the trigger, activate Light, then assert storage is `light`, `document.documentElement.dataset.theme` is `light`, and the menu is closed.

- [ ] **Step 2: Run the focused test and confirm the intended failure**

```bash
/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node --no-experimental-webstorage ./node_modules/vitest/vitest.mjs run --environment jsdom internal/assets/src/viewer.test.js
```

Expected: the new test fails because `[data-theme-control]` does not exist.

- [ ] **Step 3: Implement the minimal controller and styles**

Add `createThemeControl({ doc, controller, container })` in `viewer.js`. Create the trigger and menu with `aria-haspopup`, `aria-expanded`, `aria-controls`, and `aria-checked`; delegate selections to `await controller.setTheme(value)`; close on outside pointer events and Escape; restore focus to the trigger; and return `destroy()` that removes listeners. Mount it before Markdown content and destroy it during viewer cleanup. Add opaque themed menu styling, active and hover states, visible focus outlines, top content padding, and narrow-screen-safe positioning in `viewer.css`.

- [ ] **Step 4: Run the focused test and confirm it passes**

Use the command from Step 2. Expected: all viewer tests pass.

### Task 2: Add interaction and browser regression coverage

**Files:**
- Modify: `internal/assets/src/viewer.test.js`
- Modify: `tests/browser/whiteboard.spec.js`

**Interfaces:** Tests consume the `data-theme-control`, `data-theme-menu`, and `data-theme-option` contracts from Task 1.

- [ ] **Step 1: Add unit cases**

Cover a stored Dark preference, Escape dismissal with focus restoration, outside-click dismissal, System persistence, and cleanup of the control and media-query listener.

- [ ] **Step 2: Add browser coverage**

Open the menu, choose `[data-theme-option="dark"]`, assert `html[data-theme="dark"]`, reload and assert `Theme: Dark`, then choose System and assert storage returns to `system`. Preserve the existing Mermaid label checks.

- [ ] **Step 3: Run unit and browser tests**

```bash
/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node --no-experimental-webstorage ./node_modules/vitest/vitest.mjs run --environment jsdom internal/assets/src/viewer.test.js
/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node ./node_modules/@playwright/test/cli.js test
```

Expected: all viewer and browser tests pass.

### Task 3: Rebuild and verify generated assets

**Files:**
- Modify: `internal/assets/dist/viewer.min.js` (generated)
- Modify: `internal/assets/manifest.json` (generated hash)

- [ ] **Step 1: Rebuild and check assets**

```bash
/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node internal/assets/scripts/build.mjs
/Users/edocsss/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node internal/assets/scripts/check.mjs
```

Expected: `browser assets match deterministic build`.

- [ ] **Step 2: Run full verification**

Run `go test ./...` and `git diff --check`; both must exit successfully.

- [ ] **Step 3: Commit and push**

Stage the seven implementation, test, and generated-asset files, commit with `feat: add Markdown viewer theme menu`, and push `codex/agent-whiteboard-core-storage`.
