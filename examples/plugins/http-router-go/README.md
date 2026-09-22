# http-router-go

A network plugin for Meridian (architecture `http`) that re-routes chat requests by content: when a
user message contains one of the configured words, the request goes to another provider and model.

| Hook | What it does |
|---|---|
| `PreRequestHook` | joins the text of the user messages (plain strings and `text` content blocks), matches it against the rules (whole words, case-insensitive, first rule wins) and echoes `{"provider", "model"}` with `body_changed: true`; system and assistant messages are ignored |

`PreRequestHook` is the phase the core reserves for provider, model and fallback decisions: it runs once
per request before governance resolves the virtual key's provider config and selects a key, so the
allowlists, budgets and key selection apply to the routed target. The echo carries only the two
fields; the host merges them into the typed request it already holds. `PreRequestHook` cannot
short-circuit, and an error of the plugin is logged and skipped.

Init config (`config` of the plugin row); without one the default rule below applies:

```json
{"rules": [{"words": ["distributed", "brainstorm", "planning"], "provider": "openrouter", "model": "deepseek/deepseek-v4-flash"}]}
```

Routes (`/api/meridian/plugin/v1`): `POST /capabilities`, `POST /init`, `POST /getname`, `POST /cleanup`,
`POST /prerequesthook`, `GET /hookstream` (Meridian calls every hook over the stream), `GET /health`,
`GET /status`, plus the plugin-specific `GET /router/stats` (rules, routed count, last route).

```sh
make run ADDR=127.0.0.1:18083          # or: ROUTER_ADDR=0.0.0.0:18083 ./build/http-router-go
./build/http-router-go -log-calls=false   # ROUTER_LOG_CALLS=false: no per-call input/output lines
make docker                               # meridian-http-router:latest, listens on 18083
```

Register it in Meridian, before the built-in plugins so governance sees the routed target:

```sh
curl -X POST http://localhost:8080/api/plugins -H 'Content-Type: application/json' -d '{
  "plugin_architecture": "http",
  "name": "router",
  "enabled": true,
  "url": "http://localhost:18083",
  "capabilities": ["PreRequestHook"],
  "cel": "request_type.startsWith(\"chat_completion\")",
  "placement": "pre_builtin",
  "on_error": "fail_open"
}'
```

The virtual key of the caller must allow the target provider and model, and the target provider needs a
key; otherwise governance rejects the routed request. `on_error: fail_open` keeps requests flowing to
the original model when the router is down. The request log shows the routed provider and model, and
the plugin's line `PreRequestHook: request changed` under Plugin Logs.
