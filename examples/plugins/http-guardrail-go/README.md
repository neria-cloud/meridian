# http-guardrail-go

A network plugin for Meridian (architecture `http`) that guards chat requests against phone numbers.

| Mode | PreLLMHook | PostLLMHook / stream chunks |
|---|---|---|
| `block` (default) | a phone number anywhere in the typed request → short-circuit error, HTTP 400 `guardrail_violation`, no fallback | — |
| `mask` | every phone number is replaced by a token `<ph:<16 hex>>` kept in an in-memory KV; the request is echoed with `body_changed: true` | tokens in the response are restored from the KV; streamed chunks are buffered so a token split by tokenisation is restored once it completes (the chunk carrying `finish_reason` flushes) |

In both modes a `forbidden_combination` of patterns is rejected with HTTP 400 when EVERY pattern matches somewhere in the request (system prompt, any user message, tool output): one evaluation over the whole typed request the host sends in a single frame.

Init config (`config` of the plugin row):

```json
{"mode": "mask", "patterns": ["\\+?\\d[\\d\\s\\-().]{6,16}\\d"], "forbidden_combination": ["(?i)wire transfer", "(?i)urgent"]}
```

Routes (`/api/meridian/plugin/v1`): `POST /capabilities`, `POST /init`, `POST /getname`, `POST /cleanup`, `POST /prellmhook`, `POST /postllmhook`, `POST /httptransportstreamchunkhook`, `GET /hookstream` (Meridian calls every hook over the stream), `GET /health`, `GET /status`, plus the plugin-specific `GET /guardrail/stats` (counters and the last masked request, used by the tests).

```sh
make run MODE=mask ADDR=127.0.0.1:18082      # or: GUARDRAIL_MODE=mask GUARDRAIL_ADDR=0.0.0.0:18082 ./build/http-guardrail-go
./build/http-guardrail-go -log-calls=false   # GUARDRAIL_LOG_CALLS=false: no per-call input/output lines
./build/http-guardrail-go -log-max-bytes 0   # GUARDRAIL_LOG_MAX_BYTES: whole payloads (default 4096, then …(+N bytes))
make docker                                   # meridian-http-guardrail:latest, listens on 18082
```

Register it in Meridian:

```sh
curl -X POST localhost:8080/api/plugins -H 'content-type: application/json' -d '{
  "plugin_architecture": "http", "name": "guardrail", "enabled": true,
  "url": "http://127.0.0.1:18082",
  "capabilities": ["PreLLMHook", "PostLLMHook", "HTTPTransportStreamChunkHook"],
  "cel": "request_type.startsWith(\"chat_completion\")",
  "on_error": "fail_closed",
  "config": {"mode": "mask"}
}'
```

The KV lives in process memory and never expires; a production plugin keeps it in a store with a TTL.

## Call log

With call logging on (the default) every hook call prints its input and its output:

```
-> stream PreLLMHook id=7 in={"ctx":{"request_id":"…"},"request":{"request_type":"chat_completion",…}}
<- stream PreLLMHook id=7 out={"request":{…},"body_changed":true} dur=412µs
<- post PostLLMHook id=0 error="guardrail: forbidden combination" dur=88µs
```

`stream` is the WebSocket hook stream (`id` is the frame id), `post` a plain `POST /api/meridian/plugin/v1/{method}`
call (`id=0`). Payloads are cut at `-log-max-bytes`.
