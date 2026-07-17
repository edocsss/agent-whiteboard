---
name: agent-whiteboard
description: Use when an agent needs to publish Markdown, Mermaid diagrams, trusted standalone HTML, or images as shareable agent-whiteboard URLs, or update and delete previously published resources.
---

# Agent Whiteboard

Publish through the CLI whenever shell execution is available. Use direct HTTP only when the CLI cannot run. Return public URLs to the user; never open a browser yourself.

## Choose the resource

- Choose Markdown for ordinary boards, prose, code, tables, and Mermaid diagrams. The browser renders and sanitizes Markdown.
- Choose standalone HTML only for trusted active documents. Treat it as same-origin active content, not sanitized Markdown.
- Choose images for PNG, JPEG, GIF, or WebP binary visuals. Never upload SVG.

Respect the configured limits; defaults are 10 MiB per whiteboard, 25 MiB per image, and 100 MiB for the complete image request.

When Markdown uses local images, publish every image first, capture each returned absolute URL, and insert those URLs into the Markdown before publishing it. The service does not bundle local dependencies.

## Publish safely

1. Inspect the content and remove credentials, tokens, personal or sensitive data, and private source. Never publish any of them.
2. Select `--server`, `--timeout`, `--json`, and `--expires-in` deliberately. Omit `--expires-in` for the server default; use `--expires-in 0` for a permanent resource. There is no `--permanent` flag.
3. Publish with an approved command:
   - `agent-whiteboard serve`
   - `agent-whiteboard create markdown`
   - `agent-whiteboard create html`
   - `agent-whiteboard update markdown`
   - `agent-whiteboard update html`
   - `agent-whiteboard delete markdown`
   - `agent-whiteboard delete html`
   - `agent-whiteboard image upload`
   - `agent-whiteboard image update`
   - `agent-whiteboard image delete`
4. Read stdout or the JSON result, capture the public URL and capability ID, and return the URL without opening it.
5. Use the capability ID to update or delete. There is no separate edit token.

Public URLs are bearer capabilities: anyone with one can read it and derive the ID used for mutation. `noindex` limits discovery but is not authorization. Do not assume authentication, secrecy, or revocation beyond deletion.

Read [CLI commands](references/cli.md) for exact syntax and output, [Mermaid guidance](references/mermaid.md) when authoring diagrams, and [security guidance](references/security.md) before publishing HTML or non-public material.
