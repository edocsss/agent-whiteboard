# Security model

Resource IDs are bearer capabilities and are embedded in public URLs. Anyone who learns a URL therefore has the ID needed to read, update, or delete that resource; there is no separate edit token. IDs must not appear in logs in full.

Resources are public but non-indexed. `X-Robots-Tag` and Markdown robots meta ask cooperative crawlers not to index or archive content. This is not authentication, authorization, revocation distribution, or a confidentiality boundary. Never publish credentials, API tokens, cookies, private source, personal data, regulated data, or other sensitive material.

Markdown is rendered client-side by bundled JavaScript. Raw Markdown HTML is disabled, links and generated SVG are sanitized with DOMPurify, Mermaid uses strict security settings, and no CDN is needed. The JSON source envelope escapes script-closing sequences. These controls reduce injection risk but do not make publication of secrets safe.

Standalone HTML is deliberately served byte-for-byte as trusted active content on the same origin. It may run scripts, make requests, and access origin-scoped browser state. Deploy the whiteboard origin without authentication cookies, credentials, privileged service workers, or other sensitive same-origin state. Only publish standalone HTML you trust.

Image uploads accept PNG, JPEG, GIF, and WebP only after signature detection and format-specific configuration validation. SVG is rejected because it can contain active content. Responses use `nosniff`, a detected media type, inline filename, no-store caching, and `noimageindex`.

The service validates multipart fields, exact size limits, capability IDs, filesystem containment, regular files, and symlink safety. Logs avoid request bodies and full capability IDs. Operational logs and metrics should keep the same rule. Use TLS and appropriate network access controls when serving beyond localhost.
