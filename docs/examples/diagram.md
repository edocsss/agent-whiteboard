# Deployment flow

This Markdown file is an executable publication example.

| Stage | Owner | State |
| --- | --- | --- |
| Build | CI | complete |
| Publish | Agent | ready |

```mermaid
flowchart LR
  Agent --> CLI
  CLI --> Server
  Server --> Browser
```

```go
ctx, cancel := context.WithTimeout(parent, 30*time.Second)
defer cancel()
```
