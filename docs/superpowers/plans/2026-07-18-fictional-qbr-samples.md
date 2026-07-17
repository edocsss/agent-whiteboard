# Fictional QBR Samples Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce and publish realistic Markdown and standalone HTML versions of a fictional Q2 2026 executive operating review.

**Architecture:** Author both artifacts from one internally consistent fictional dataset in a temporary directory, so the repository's active code changes remain untouched. Publish each artifact through the real CLI to the locally running server, then verify the resulting capability URLs in a browser.

**Tech Stack:** Markdown, Mermaid, semantic HTML, self-contained CSS, Go CLI, local agent-whiteboard HTTP server, browser automation.

## Global Constraints

- Use only fictional Northstar Cloud Q2 2026 data.
- Publish to `http://127.0.0.1:8567`.
- Omit `--expires-in` so the server-configured default applies.
- Include no external scripts, stylesheets, fonts, images, or network resources in HTML.
- Keep capability IDs out of unrelated logs and retain them only for this task.
- Do not modify or delete the user's server state after publication.

---

### Task 1: Author the two QBR artifacts

**Files:**
- Create: `/tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.md`
- Create: `/tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.html`

**Interfaces:**
- Consumes: The approved fictional QBR sample design.
- Produces: Two UTF-8 documents accepted by `agent-whiteboard create markdown` and `agent-whiteboard create html`.

- [ ] **Step 1: Author the Markdown report**

Create a renderer-focused executive report containing: an explicit fictional-data notice; executive summary; KPI table with actual, plan, Q1, and status; operating narrative; Mermaid growth flywheel; risk table; Q3 priority checklist with owners and numeric targets; and a fenced JSON forecast excerpt.

- [ ] **Step 2: Author the standalone HTML dashboard**

Create a responsive, semantic document with inline CSS only. Include an executive header, KPI cards, plan-attainment bars, business highlights, risk register, Q3 priorities, fictional-data footer, print styles, visible focus treatment, and reduced-motion handling. Use no JavaScript or external resource URL.

- [ ] **Step 3: Validate both source files**

Run:

```bash
test -s /tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.md
test -s /tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.html
rg -n 'Northstar Cloud|Q2 2026|Fictional' /tmp/agent-whiteboard-qbr
if rg -n 'https?://|<script|TODO|TBD|FIXME' /tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.html; then exit 1; fi
```

Expected: both files are non-empty; both identify the company, period, and fictional nature; the HTML scan exits successfully with no matches.

### Task 2: Publish and verify the samples

**Files:**
- Read: `/tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.md`
- Read: `/tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.html`

**Interfaces:**
- Consumes: The two validated source documents and a server listening at `127.0.0.1:8567`.
- Produces: One Markdown capability URL and one HTML capability URL verified in a browser.

- [ ] **Step 1: Confirm server readiness and build the current CLI**

Run:

```bash
curl --fail --silent --show-error http://127.0.0.1:8567/readyz
go build -o /tmp/agent-whiteboard-qbr/agent-whiteboard ./cmd/agent-whiteboard
```

Expected: readiness returns success and the CLI binary is created.

- [ ] **Step 2: Publish both artifacts**

Run each command with JSON output and parse the returned resource URL without printing unrelated capability fields:

```bash
/tmp/agent-whiteboard-qbr/agent-whiteboard --server http://127.0.0.1:8567 --timeout 30s --json create markdown /tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.md
/tmp/agent-whiteboard-qbr/agent-whiteboard --server http://127.0.0.1:8567 --timeout 30s --json create html /tmp/agent-whiteboard-qbr/northstar-cloud-q2-2026.html
```

Expected: both commands exit zero and return distinct view URLs.

- [ ] **Step 3: Verify both URLs in a browser**

Open each returned URL. Confirm the page title and QBR sections render, Markdown tables and Mermaid appear, the HTML dashboard is readable at desktop and narrow viewport widths, and neither document has a blocking console or external-network error.

- [ ] **Step 4: Return the verified URLs**

Label the Markdown and HTML links separately, state that the data is fictional, and note that the server's configured default expiration applies.
