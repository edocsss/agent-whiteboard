# Filesystem storage

The default store implements two domain-owned interfaces, `agentwb.WhiteboardStore` and `agentwb.ImageStore`. Business domains never reach into another domain's persistence layer; composition occurs through services.

The root contains `whiteboards/`, `images/`, and `.readiness/`. Each capability ID has a private `0700` directory. Files are `0600`. A resource directory contains `metadata.json` and an immutable generation: `source-<32 hex>.md`, `source-<32 hex>.html`, or `content-<32 hex>`. Metadata schema 1 records kind, seconds/nanoseconds timestamps, nullable expiration, the referenced generation, extension, and media type.

Create and replace write exclusive random temporary files, sync them, publish an immutable generation without clobbering an existing name, then atomically rename fully written metadata. Directory syncs make the transition durable. Readers follow only the generation named by verified metadata, so they see the old or new complete resource, never a partial write.

Locks are process-local and granular by namespace plus capability ID. Unrelated whiteboards/images proceed concurrently; ordinary reads use the resource's shared read lock, while mutation and cleanup take its exclusive lock. The supported deployment assumption is exactly one server process per filesystem root. Do not point multiple processes at the same root; there is intentionally no inter-process file lock.

Expiration is evaluated during reads and periodic cleanup. Expired resources behave as not found. Cleanup removes expired resources, unreferenced recognized generations, and recognized temporary artifacts, while preserving the live referenced generation and unknown files. Shutdown cancels cleanup, waits for active operations, and closes root handles.

All traversal is rooted through verified `os.Root` handles. Capability IDs and internal filenames are validated; symlink roots/resources, non-directories, traversal, and name collisions are rejected. Metadata and generation files are opened and verified as regular files. Do not manually edit a live root.

Custom stores must honor contexts, support concurrent calls, preserve immutable identity/creation time on replacement, treat expiration consistently, return stable error codes, implement readiness, and make close idempotent. Atomic replacement and crash-safe durability are the custom implementation's responsibility.
