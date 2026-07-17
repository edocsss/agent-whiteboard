# CLI commands

Put global flags before the command. Use `--` before an ID because valid capability IDs may begin with `-`.

```text
agent-whiteboard [--server URL] [--timeout DURATION] [--json] serve [flags]
agent-whiteboard [global flags] create markdown [--expires-in SECONDS] FILE
agent-whiteboard [global flags] create html [--expires-in SECONDS] FILE
agent-whiteboard [global flags] update markdown [--expires-in SECONDS] -- ID FILE
agent-whiteboard [global flags] update html [--expires-in SECONDS] -- ID FILE
agent-whiteboard [global flags] delete markdown -- ID
agent-whiteboard [global flags] delete html -- ID
agent-whiteboard [global flags] image upload [--expires-in SECONDS] FILE...
agent-whiteboard [global flags] image update [--expires-in SECONDS] -- ID FILE
agent-whiteboard [global flags] image delete -- ID
```

Use `--server` for a non-default service, `--timeout` for the client deadline, and `--json` for machine-readable output. Create without `--expires-in` uses the server default. Update without it preserves the existing absolute expiration. A positive value resets expiration from the update time; zero makes the resource permanent.

Successful create and update commands print one absolute public URL per resource to stdout. Image upload can print multiple URLs in input order. Successful delete is silent. The CLI never opens a browser.

JSON mode uses schema version 1:

```json
{"schema_version":1,"resource":{"id":"CAPABILITY_ID","url":"https://board.example/whiteboards/markdown/CAPABILITY_ID","expires_at":1780000000,"permanent":false}}
```

Multiple uploads use `resources`; permanent resources have `"expires_at":null` and `"permanent":true`; delete success is `{"schema_version":1}`. Errors go to stderr as `{"schema_version":1,"error":{"code":"...","message":"..."}}`. Human errors also use stderr. Exit codes are 0 success, 1 internal, 2 usage, 3 remote/domain, and 4 timeout/cancellation.

Do not invent `publish`, authentication, asset bundling, or a `--permanent` flag. The supported operations above are the complete publication surface.
