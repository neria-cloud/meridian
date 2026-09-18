# tests/functional

Meridian's functional tests for the patchset-provided APIs (worst-case cost
estimation and the reservation policy). They run against the working tree
through the committed `go.work` (`use . ../../core ../../framework`), never
against published versions, so a release pull request is gated on the code it
carries. The module is brought in by the `estimate-cost` patchset of
bifrost-prep; edit it there.

```
make test-functional        # every test, no Docker, < 1 min
make test-functional-pg     # adds the Postgres-backed override test (Docker)
```

Every test calls `t.Parallel()` and builds its own catalog through
`internal/fixtures`; there is no shared mutable state. The pricing "cache
server" is an `httptest.Server` serving a datasheet JSON.
