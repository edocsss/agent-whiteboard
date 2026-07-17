# Fictional QBR Sample Design

## Purpose

Create two realistic, fictional quarterly business review artifacts for testing the local agent-whiteboard server: one Markdown report and one trusted standalone HTML dashboard. The user will open the returned capability URLs personally.

## Content

Both artifacts represent Northstar Cloud's Q2 2026 executive operating review and use the same invented, internally consistent data. They include:

- an executive summary;
- KPI performance versus plan and prior quarter;
- revenue, customer, retention, and margin trends;
- product and go-to-market highlights;
- operating risks and mitigations;
- Q3 priorities with owners and measurable targets;
- a Mermaid growth-flywheel diagram in Markdown.

No real company, customer, employee, credential, private source, or sensitive data is included.

## Presentation

The Markdown report emphasizes readability and renderer coverage through headings, tables, blockquotes, task lists, highlighted code-style data, and Mermaid.

The standalone HTML report is a responsive executive dashboard with self-contained HTML and CSS. It uses no external scripts, stylesheets, fonts, images, or network resources. It presents KPI cards, trend bars, status badges, risks, and next-quarter priorities. It contains no active JavaScript because interaction is unnecessary for this test.

## Publication and verification

Publish both files through the real CLI to the default origin, `http://127.0.0.1:8567`. Omit `--expires-in` deliberately so the server's configured default applies. Capture JSON output and retain the capability IDs only for the duration of the task.

Use available browser automation to verify that both returned URLs load successfully, contain the expected titles and sections, have readable responsive layouts, and make no external requests. Do not modify or delete the user's server state after successful publication. Return both public URLs so the user can open them independently.

## Acceptance criteria

- Both CLI creates succeed against the default server.
- Markdown renders its tables, highlighted block, and Mermaid diagram.
- HTML renders as a polished dashboard without external dependencies.
- Browser verification finds no layout, content, console, or network error that blocks use.
- Final output contains the two URLs and identifies which is Markdown and which is HTML.
