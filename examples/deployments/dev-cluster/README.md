# Dev cluster

Three meridian nodes behind Traefik, with PostgreSQL. For hand-driven
experiments and for seeing the real web UI on a clustered deployment.

```sh
make dev-cluster                       # needs MERIDIAN_LICENSE_STR
make dev-cluster MERIDIAN_DEV_AUTH=1   # seed admin/admin and enable auth
make dev-cluster-down                  # V=1 also wipes the volumes
make dev-cluster-logs N=2              # tail one node
```

| address | what |
|---|---|
| http://localhost:8082 | Traefik — the address to use |
| http://localhost:8090 | Traefik dashboard |
| http://localhost:8091/8092/8093 | node1/2/3 directly, bypassing the load balancer |
| localhost:15433 | PostgreSQL |

Built on the **production** image (`make docker-build-ent` → `meridian-ent:latest`), so
the nodes serve the real embedded UI. There is no hot reload: rebuild the image
after code or UI edits. For a fast UI loop use `make dev`, which runs the vite
dev server with HMR against a local backend.

## Routing

`/api`, `/internal`, `/metrics`, `/ws` and `/health` load-balance across the
three nodes; everything else is the SPA, also load-balanced. The UI is not
pinned to one node (as it is in `../meridian`'s dev cluster) because every node
runs the same image — the embedded UI is byte-identical and its assets are
content-hashed, so pinning would add a single point of failure for nothing.

Two Traefik settings are load-bearing:

- `allowEmptyServices` keeps the service registered when every backend fails its
  readiness check, so an all-unready cluster answers 503 — the correct
  load-balancer signal — instead of the route vanishing and falling through to
  404.
- `constraints=Label(meridian-dev-cluster,true)` scopes discovery to this
  stack. The docker provider watches the whole daemon, so without it a
  concurrently running `make e2e` stack is adopted as a backend: its routers
  match the same rule at the same priority but its containers sit on a different
  network, and requests resolve to an unreachable service.

The service-level health check is `/internal/ready`; the container health check
is `/internal/live`. They are deliberately different. Liveness is independent of
quorum, so a node parked awaiting re-admission keeps answering it and Docker
does not restart the container, while Traefik's readiness check drains it. Using
liveness for both would send roughly one request in three to a node that cannot
serve it.

## `MERIDIAN_ENCRYPTION_KEY` must be identical on every node

WebSocket tickets (`POST /api/session/ws-ticket` → `GET /ws?ticket=…`) are
HMAC-signed with a key derived from it. Only a signed ticket survives the trip
through a load balancer: the node that issues the ticket and the node the
upgrade lands on are almost never the same.

Without the key, `encrypt.Key()` returns nil and
`handlers.NewSignedWSTicketStore` **silently falls back** to a node-local
in-memory map, so every `/ws` handshake through Traefik answers 401 while
working fine against a node's own port. Nothing else breaks and the UI retries
forever, which makes this failure easy to miss — `tests/e2e` has a regression
test for it.

The value here is a dev-only default, overridable with
`MERIDIAN_DEV_ENCRYPTION_KEY`. It also encrypts database columns, so once set it
cannot be changed without wiping the volumes (`make dev-cluster-down V=1`).

## NOT CONSIDERED & TODO

- The cluster-wide WS ticket signing key is tied to the operator-supplied
  encryption key. A better shape is a separate ephemeral signing key generated
  by the `_meta` raft leader and distributed through the Olric global DMap:
  tickets live 30 s, so nothing durable depends on it, and working WebSockets
  would stop depending on whether encryption was configured. Needs lazy
  resolution (a follower can reach the ticket store before the leader has
  written the key), an atomic put-if-absent, and a fallback to the node-local
  store when Olric is absent. [potential-improvement]
- No `framework.pricing` block, so nodes fetch the model catalog and MCP library
  from the real internet at boot. `tests/cluster` serves these from a local
  nginx because the internet fetch runs under the cluster-wide startup lock
  before gRPC binds and can stagger nodes past the founding window. Acceptable
  here, where a developer can wait; it is why first start is slower than the
  e2e stack's. [potential-improvement]
- No hot reload. `../meridian` runs air against a bind-mounted source tree with
  a dedicated dev image; that was not ported. [porting:postponned-to-the-next-task]
