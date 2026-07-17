# Security model

Treat each ID and public URL as a bearer capability. Anyone who has the URL can read the resource and recover the ID used to update or delete it. There is no login, authorization layer, private mode, or separate edit token.

Public responses ask crawlers not to index them, but `noindex` is not access control. Links can leak through chat, logs, browser history, screenshots, referrers, and forwarding. Delete a resource to revoke its URL; expiration limits its lifetime but does not make it private while live.

Never publish credentials, tokens, secrets, personal or sensitive data, or private source. Avoid putting full capability IDs into logs or unrelated documents.

Markdown is rendered in the browser and sanitized. Standalone HTML is different: it is trusted, same-origin active content and may execute inline JavaScript. Publish HTML only when its entire source is trusted. Do not use the origin for authentication cookies or sensitive application state. Never upload SVG; use PNG, JPEG, GIF, or WebP.

Return the generated URL without opening a browser or fetching the published active document. Opening it can execute trusted HTML in the agent's browser context.
