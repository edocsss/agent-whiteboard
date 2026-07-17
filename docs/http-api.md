# HTTP API

The API is versioned under `/api/v1`. Successful mutation responses contain paths, never server-generated absolute URLs. Build public URLs using the origin you used for the request.

## Routes

| Route | Body and result |
| --- | --- |
| `GET /healthz` | `200 {"status":"ok"}`; `Cache-Control: no-store` |
| `GET /readyz` | `200 {"status":"ready"}` or `503 {"status":"unavailable"}` |
| `POST /api/v1/whiteboards/markdown` | multipart `file`, optional `expires_in_seconds`; `201` resource |
| `PUT /api/v1/whiteboards/markdown/{id}` | same multipart fields; `200` resource |
| `DELETE /api/v1/whiteboards/markdown/{id}` | `204` |
| `POST /api/v1/whiteboards/html` | multipart `file`, optional `expires_in_seconds`; `201` resource |
| `PUT /api/v1/whiteboards/html/{id}` | same multipart fields; `200` resource |
| `DELETE /api/v1/whiteboards/html/{id}` | `204` |
| `GET /whiteboards/markdown/{id}` | browser-rendering HTML shell for Markdown |
| `GET /whiteboards/html/{id}` | trusted HTML bytes unchanged |
| `POST /api/v1/images` | one or more multipart `images`, optional `expires_in_seconds`; `201` images |
| `PUT /api/v1/images/{id}` | exactly one multipart `file`, optional `expires_in_seconds`; `200` resource |
| `DELETE /api/v1/images/{id}` | `204` |
| `GET /images/{id}` | validated raster bytes with detected media type |

`HEAD` is supported by Go's GET routing for public resources. Health endpoints require `GET`. Unsupported methods return `405` with `Allow`.

## Multipart and limits

`expires_in_seconds` is a signed decimal field at the transport layer; valid service values are nonnegative. Omit it to use the create default or preserve update expiration. Zero means permanent. Only documented fields are accepted, and whiteboard create/update requires exactly one `file`.

Defaults are 10 MiB per whiteboard, 25 MiB per image, and 100 MiB for the complete image request. Limits are exact byte limits including a bounded multipart request. Image type is detected from content and verified with format-specific configuration parsing, not trusted from the filename: PNG, JPEG, GIF, and WebP are accepted; SVG and malformed files return `unsupported_media_type`.

## Schemas

A whiteboard or single-image mutation returns:

```json
{"resource":{"id":"CAPABILITY_ID","type":"markdown","path":"/whiteboards/markdown/CAPABILITY_ID","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","expires_at":1767229200,"permanent":false}}
```

Image resources use `filename`, `extension`, and `media_type` instead of `type`. Multi-image create returns `{"images":[...]}` in upload order. `expires_at` is a nullable Unix-seconds integer; when it is `null`, `permanent` is `true`.

Errors are stable JSON:

```json
{"error":{"code":"invalid_request","message":"invalid multipart form"}}
```

| Code | Status |
| --- | ---: |
| `invalid_request` | 400 |
| `not_found` | 404 |
| `content_too_large` | 413 |
| `unsupported_media_type` | 415 |
| `storage_unavailable` | 503 |
| `internal_error` | 500 |

Unknown/internal causes are sanitized. Public GET responses set `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, and `X-Robots-Tag: noindex, nofollow, noarchive`; images append `noimageindex` and set `Content-Disposition: inline` with filename `<id><detected-extension>`. Markdown HTML also contains the corresponding robots meta tag.

## curl

```sh
curl -fsS -F file=@docs/examples/diagram.md -F expires_in_seconds=3600 http://127.0.0.1:8567/api/v1/whiteboards/markdown
curl -fsS -F images=@chart.png -F images=@photo.webp -F expires_in_seconds=0 http://127.0.0.1:8567/api/v1/images
curl -fsS -X DELETE http://127.0.0.1:8567/api/v1/images/CAPABILITY_ID
```
