// Command http-guardrail-go is a network plugin for Meridian that guards chat
// requests against phone numbers: in `block` mode it rejects the request with
// HTTP 400, in `mask` mode it replaces every number by a token kept in an
// in-memory KV and restores the numbers in the response (non-streaming and
// streaming). It also rejects a forbidden COMBINATION of patterns spread over
// the parts of one request.
//
// Contract: framework/pluginshttp/openapi/openapi.yaml of github.com/neria-cloud/meridian-base.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/fasthttp/router"
	"github.com/fasthttp/websocket"
	"github.com/valyala/fasthttp"
)

const (
	pluginName      = "PhoneGuardrail"
	pluginVersion   = "1.0.0"
	protocolVersion = 1
	basePath        = "/api/meridian/plugin/v1"
	defaultAddr     = "0.0.0.0:18082"
)

var hooks = []string{"PreLLMHook", "PostLLMHook", "HTTPTransportStreamChunkHook"}

// Settings come from the Init config (and the -mode flag as the default).
type settings struct {
	Mode                 string   `json:"mode"`                  // block | mask
	Patterns             []string `json:"patterns"`              // regexes; default: one phone pattern
	ForbiddenCombination []string `json:"forbidden_combination"` // all must match somewhere in one request → 400
}

type (
	contextDetails struct {
		RequestID string `json:"request_id,omitempty"`
	}
	llmRequest struct {
		RequestType string                 `json:"request_type"`
		Request     sonic.NoCopyRawMessage `json:"request,omitempty"`
	}
	llmResponse struct {
		RequestType string                 `json:"request_type,omitempty"`
		Response    sonic.NoCopyRawMessage `json:"response,omitempty"`
	}
	preLLMHookRequest struct {
		Ctx     contextDetails `json:"ctx"`
		Request llmRequest     `json:"request"`
	}
	postLLMHookRequest struct {
		Ctx      contextDetails         `json:"ctx"`
		Response *llmResponse           `json:"response,omitempty"`
		Error    sonic.NoCopyRawMessage `json:"error,omitempty"`
	}
	streamChunk struct {
		Index       int64                  `json:"index"`
		RequestType string                 `json:"request_type,omitempty"`
		Chunk       sonic.NoCopyRawMessage `json:"chunk,omitempty"`
	}
	chunkHookRequest struct {
		Ctx   contextDetails `json:"ctx"`
		Chunk streamChunk    `json:"chunk"`
	}
	errorField struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	bifrostError struct {
		IsBifrostError bool       `json:"is_bifrost_error"`
		StatusCode     int        `json:"status_code"`
		AllowFallbacks bool       `json:"allow_fallbacks"`
		Error          errorField `json:"error"`
	}
	shortCircuit struct {
		Error *bifrostError `json:"error,omitempty"`
	}
	preLLMHookResponse struct {
		Request      *llmRequest   `json:"request,omitempty"`
		ShortCircuit *shortCircuit `json:"short_circuit,omitempty"`
		BodyChanged  bool          `json:"body_changed,omitempty"`
	}
	postLLMHookResponse struct {
		Response    *llmResponse `json:"response,omitempty"`
		BodyChanged bool         `json:"body_changed,omitempty"`
	}
	chunkHookResponse struct {
		Chunk       *streamChunk `json:"chunk,omitempty"`
		BodyChanged bool         `json:"body_changed,omitempty"`
	}
	capabilitiesResponse struct {
		Hooks           []string `json:"hooks"`
		ProtocolVersion int      `json:"protocol_version"`
		StreamSupported bool     `json:"stream_supported"`
	}
	initRequest struct {
		Config settings `json:"config"`
	}
	healthResponse struct {
		Status     string            `json:"status"`
		Components map[string]string `json:"components,omitempty"`
	}
	statusStats struct {
		CallsTotal  int64 `json:"calls_total"`
		ErrorsTotal int64 `json:"errors_total"`
		OpenStreams int32 `json:"open_streams"`
	}
	statusResponse struct {
		Name            string      `json:"name"`
		Version         string      `json:"version"`
		ProtocolVersion int         `json:"protocol_version"`
		Hooks           []string    `json:"hooks"`
		StreamSupported bool        `json:"stream_supported"`
		StartedAt       string      `json:"started_at"`
		UptimeSeconds   int64       `json:"uptime_seconds"`
		Stats           statusStats `json:"stats"`
	}
	transportError struct {
		Error string `json:"error"`
	}
)

type call struct {
	name string
	prop string
	fn   func(payload []byte) (any, error)
}

type plugin struct {
	calls   []call
	started time.Time

	mu        sync.RWMutex
	mode      string
	patterns  []*regexp.Regexp
	forbidden []*regexp.Regexp

	vault   *vault
	streams sync.Map // request_id → *streamState

	// Call logging (-log-calls): input and output of every hook call.
	logCalls    bool
	logMaxBytes int

	callsTotal  atomic.Int64
	errorsTotal atomic.Int64
	openStreams atomic.Int32
	blocked     atomic.Int64
	masked      atomic.Int64
	unmasked    atomic.Int64
	lastMasked  atomic.Pointer[string]
}

// guardStats is the plugin-specific GET /guardrail/stats payload used by tests.
type guardStats struct {
	Mode       string `json:"mode"`
	Blocked    int64  `json:"blocked"`
	Masked     int64  `json:"masked"`
	Unmasked   int64  `json:"unmasked"`
	VaultSize  int    `json:"vault_size"`
	LastMasked string `json:"last_masked,omitempty"`
}

// streamState is the per-stream unmask buffer (a token can be split by tokenisation).
type streamState struct {
	hold string
}

func newPlugin(mode string) *plugin {
	p := &plugin{started: time.Now(), vault: newVault()}
	p.apply(settings{Mode: mode})
	p.calls = []call{
		{"Capabilities", "capabilities", func([]byte) (any, error) {
			return capabilitiesResponse{Hooks: hooks, ProtocolVersion: protocolVersion, StreamSupported: true}, nil
		}},
		{"Init", "init", p.init},
		{"GetName", "get_name", func([]byte) (any, error) { return map[string]string{"name": pluginName}, nil }},
		{"Cleanup", "cleanup", func([]byte) (any, error) { return struct{}{}, nil }},
		{"PreLLMHook", "pre_llm_hook", p.preLLMHook},
		{"PostLLMHook", "post_llm_hook", p.postLLMHook},
		{"HTTPTransportStreamChunkHook", "http_transport_stream_chunk_hook", p.chunkHook},
	}
	return p
}

func (p *plugin) apply(s settings) error {
	if s.Mode == "" {
		s.Mode = "block"
	}
	if s.Mode != "block" && s.Mode != "mask" {
		return fmt.Errorf("mode must be block or mask, got %q", s.Mode)
	}
	if len(s.Patterns) == 0 {
		s.Patterns = []string{defaultPhonePattern}
	}
	pats := make([]*regexp.Regexp, 0, len(s.Patterns))
	for _, raw := range s.Patterns {
		re, err := regexp.Compile(raw)
		if err != nil {
			return fmt.Errorf("patterns: %w", err)
		}
		pats = append(pats, re)
	}
	forb := make([]*regexp.Regexp, 0, len(s.ForbiddenCombination))
	for _, raw := range s.ForbiddenCombination {
		re, err := regexp.Compile(raw)
		if err != nil {
			return fmt.Errorf("forbidden_combination: %w", err)
		}
		forb = append(forb, re)
	}
	p.mu.Lock()
	p.mode, p.patterns, p.forbidden = s.Mode, pats, forb
	p.mu.Unlock()
	return nil
}

func (p *plugin) init(payload []byte) (any, error) {
	var req initRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	if err := p.apply(req.Config); err != nil {
		return map[string]any{"error": map[string]string{"message": err.Error()}}, nil
	}
	log.Printf("init: mode=%s patterns=%d forbidden_combination=%d", req.Config.Mode, len(req.Config.Patterns), len(req.Config.ForbiddenCombination))
	return struct{}{}, nil
}

// guardrailError is a policy decision on the client's input, not a gateway
// failure: is_bifrost_error stays false so Meridian treats it like any 400.
func guardrailError(msg string) *shortCircuit {
	return &shortCircuit{Error: &bifrostError{StatusCode: 400, AllowFallbacks: false, Error: errorField{Type: "guardrail_violation", Message: msg}}}
}

// preLLMHook scans every string of the typed request. block: 400 on a match;
// mask: replace and echo the request with body_changed=true. A forbidden
// combination (every pattern matching somewhere in the request) is 400 in both modes.
func (p *plugin) preLLMHook(payload []byte) (any, error) {
	var req preLLMHookRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	if len(req.Request.Request) == 0 {
		return preLLMHookResponse{}, nil
	}
	var tree any
	if err := sonic.Unmarshal(req.Request.Request, &tree); err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	p.mu.RLock()
	mode, patterns, forbidden := p.mode, p.patterns, p.forbidden
	p.mu.RUnlock()

	if len(forbidden) > 0 {
		var strs []string
		collectStrings(tree, &strs)
		all := true
		for _, re := range forbidden {
			hit := false
			for _, s := range strs {
				if re.MatchString(s) {
					hit = true
					break
				}
			}
			if !hit {
				all = false
				break
			}
		}
		if all {
			p.blocked.Add(1)
			return preLLMHookResponse{ShortCircuit: guardrailError("request contains a forbidden combination of patterns")}, nil
		}
	}

	found := false
	tree = walkStrings(tree, func(s string) string {
		for _, re := range patterns {
			if mode == "block" {
				if re.MatchString(s) {
					found = true
				}
				continue
			}
			out, hit := p.vault.mask(re, s)
			if hit {
				found = true
				s = out
			}
		}
		return s
	})
	if !found {
		return preLLMHookResponse{}, nil
	}
	if mode == "block" {
		p.blocked.Add(1)
		return preLLMHookResponse{ShortCircuit: guardrailError("request contains a phone number")}, nil
	}
	p.masked.Add(1)
	b, err := sonic.Marshal(tree)
	if err != nil {
		return nil, err
	}
	last := string(b)
	p.lastMasked.Store(&last)
	return preLLMHookResponse{Request: &llmRequest{RequestType: req.Request.RequestType, Request: b}, BodyChanged: true}, nil
}

// postLLMHook restores tokens in every string of the typed response.
func (p *plugin) postLLMHook(payload []byte) (any, error) {
	var req postLLMHookRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	// No raw-bytes shortcut: JSON encoders may escape "<" as \u003c, so the
	// token is only visible after decoding.
	if req.Response == nil || len(req.Response.Response) == 0 || p.vault.size() == 0 {
		return postLLMHookResponse{}, nil
	}
	var tree any
	if err := sonic.Unmarshal(req.Response.Response, &tree); err != nil {
		return nil, fmt.Errorf("response: %w", err)
	}
	changed := false
	tree = walkStrings(tree, func(s string) string {
		out, hit := p.vault.unmask(s)
		if hit {
			changed = true
		}
		return out
	})
	if !changed {
		return postLLMHookResponse{}, nil
	}
	p.unmasked.Add(1)
	b, err := sonic.Marshal(tree)
	if err != nil {
		return nil, err
	}
	return postLLMHookResponse{Response: &llmResponse{RequestType: req.Response.RequestType, Response: b}, BodyChanged: true}, nil
}

// chunkHook restores tokens in streamed chunks. Text that could be the start
// of a token is held back until the next chunk; the chunk carrying a
// finish_reason flushes it.
func (p *plugin) chunkHook(payload []byte) (any, error) {
	var req chunkHookRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	if len(req.Chunk.Chunk) == 0 || req.Ctx.RequestID == "" {
		return chunkHookResponse{}, nil
	}
	if p.vault.size() == 0 {
		return chunkHookResponse{}, nil
	}
	var tree any
	if err := sonic.Unmarshal(req.Chunk.Chunk, &tree); err != nil {
		return nil, fmt.Errorf("chunk: %w", err)
	}
	stv, _ := p.streams.LoadOrStore(req.Ctx.RequestID, &streamState{})
	st := stv.(*streamState)
	changed := false
	final := chunkIsFinal(tree)
	tree = walkStrings(tree, func(s string) string {
		if !isDeltaContent(tree, s) {
			return s
		}
		joined := st.hold + s
		out, hit := p.vault.unmask(joined)
		emit, hold := out, ""
		if !final {
			emit, hold = splitTail(out)
		}
		if hit || joined != s || hold != st.hold {
			changed = true
		}
		st.hold = hold
		return emit
	})
	if final {
		p.streams.Delete(req.Ctx.RequestID)
	}
	if !changed {
		return chunkHookResponse{}, nil
	}
	p.unmasked.Add(1)
	b, err := sonic.Marshal(tree)
	if err != nil {
		return nil, err
	}
	return chunkHookResponse{Chunk: &streamChunk{Index: req.Chunk.Index, RequestType: req.Chunk.RequestType, Chunk: b}, BodyChanged: true}, nil
}

// chunkIsFinal: a chat chunk whose first choice carries finish_reason.
func chunkIsFinal(tree any) bool {
	m, ok := tree.(map[string]any)
	if !ok {
		return false
	}
	choices, _ := m["choices"].([]any)
	for _, c := range choices {
		if cm, ok := c.(map[string]any); ok {
			if fr, ok := cm["finish_reason"]; ok && fr != nil {
				return true
			}
		}
	}
	return false
}

// isDeltaContent reports whether s is a choices[].delta.content string of the
// chunk (the only text a stream carries to the client).
func isDeltaContent(tree any, s string) bool {
	m, ok := tree.(map[string]any)
	if !ok {
		return false
	}
	choices, _ := m["choices"].([]any)
	for _, c := range choices {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if d, ok := cm["delta"].(map[string]any); ok {
			if v, ok := d["content"].(string); ok && v == s {
				return true
			}
		}
	}
	return false
}

func (p *plugin) lookup(name string) (call, bool) {
	for _, c := range p.calls {
		if strings.EqualFold(c.name, name) {
			return c, true
		}
	}
	return call{}, false
}

func (p *plugin) handler() fasthttp.RequestHandler {
	r := router.New()
	r.GET(basePath+"/hookstream", p.serveStream)
	r.GET(basePath+"/health", p.serveHealth)
	r.GET(basePath+"/status", p.serveStatus)
	r.GET(basePath+"/guardrail/stats", p.serveGuardStats)
	r.POST(basePath+"/{method}", p.servePost)
	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		fail(ctx, fasthttp.StatusNotFound, "unknown method "+string(ctx.Method())+" "+string(ctx.Path()))
	}
	return r.Handler
}

func (p *plugin) servePost(ctx *fasthttp.RequestCtx) {
	c, ok := p.lookup(ctx.UserValue("method").(string))
	if !ok {
		fail(ctx, fasthttp.StatusNotFound, "unknown method "+string(ctx.Path()))
		return
	}
	p.callsTotal.Add(1)
	resp, err := p.run("post", 0, c, ctx.PostBody())
	if err != nil {
		p.errorsTotal.Add(1)
		fail(ctx, fasthttp.StatusBadRequest, err.Error())
		return
	}
	write(ctx, fasthttp.StatusOK, resp)
}

// run dispatches one call and, when call logging is on, prints its input and
// output (JSON, cut at logMaxBytes) with the transport, the stream id and the
// duration, so a session of the plugin can be read end to end from its log.
func (p *plugin) run(transport string, id uint64, c call, payload []byte) (any, error) {
	if !p.logCalls {
		return c.fn(payload)
	}
	started := time.Now()
	log.Printf("-> %s %s id=%d in=%s", transport, c.name, id, clip(payload, p.logMaxBytes))
	resp, err := c.fn(payload)
	dur := time.Since(started).Round(time.Microsecond)
	if err != nil {
		log.Printf("<- %s %s id=%d error=%q dur=%s", transport, c.name, id, err.Error(), dur)
		return nil, err
	}
	out, mErr := sonic.Marshal(resp)
	if mErr != nil {
		out = []byte(fmt.Sprintf("%q", mErr.Error()))
	}
	log.Printf("<- %s %s id=%d out=%s dur=%s", transport, c.name, id, clip(out, p.logMaxBytes), dur)
	return resp, nil
}

// clip returns b as a string cut at max bytes with a marker of what was dropped.
func clip(b []byte, max int) string {
	if max <= 0 || len(b) <= max {
		return string(b)
	}
	return fmt.Sprintf("%s…(+%d bytes)", b[:max], len(b)-max)
}

func (p *plugin) serveHealth(ctx *fasthttp.RequestCtx) {
	write(ctx, fasthttp.StatusOK, healthResponse{Status: "ok", Components: map[string]string{"stream": "ok", "vault": fmt.Sprintf("%d entries", p.vault.size())}})
}

func (p *plugin) serveGuardStats(ctx *fasthttp.RequestCtx) {
	p.mu.RLock()
	mode := p.mode
	p.mu.RUnlock()
	st := guardStats{Mode: mode, Blocked: p.blocked.Load(), Masked: p.masked.Load(), Unmasked: p.unmasked.Load(), VaultSize: p.vault.size()}
	if last := p.lastMasked.Load(); last != nil {
		st.LastMasked = *last
	}
	write(ctx, fasthttp.StatusOK, st)
}

func (p *plugin) serveStatus(ctx *fasthttp.RequestCtx) {
	write(ctx, fasthttp.StatusOK, statusResponse{
		Name: pluginName, Version: pluginVersion, ProtocolVersion: protocolVersion, Hooks: hooks, StreamSupported: true,
		StartedAt: p.started.UTC().Format(time.RFC3339), UptimeSeconds: int64(time.Since(p.started).Seconds()),
		Stats: statusStats{CallsTotal: p.callsTotal.Load(), ErrorsTotal: p.errorsTotal.Load(), OpenStreams: p.openStreams.Load()},
	})
}

var upgrader = websocket.FastHTTPUpgrader{CheckOrigin: func(*fasthttp.RequestCtx) bool { return true }}

func (p *plugin) serveStream(ctx *fasthttp.RequestCtx) {
	err := upgrader.Upgrade(ctx, func(conn *websocket.Conn) {
		defer conn.Close()
		p.openStreams.Add(1)
		defer p.openStreams.Add(-1)
		if p.logCalls {
			log.Printf("hookstream open from %s (%d open)", conn.RemoteAddr(), p.openStreams.Load())
			defer func() { log.Printf("hookstream closed from %s (%d open)", conn.RemoteAddr(), p.openStreams.Load()-1) }()
		}
		for {
			_, frame, err := conn.ReadMessage()
			if err != nil {
				if p.logCalls && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("hookstream read from %s: %v", conn.RemoteAddr(), err)
				}
				return
			}
			reply, err := sonic.Marshal(p.streamReply(frame))
			if err != nil {
				log.Printf("encode: %v", err)
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, reply); err != nil {
				return
			}
		}
	})
	if err != nil {
		log.Printf("hookstream: %v", err)
	}
}

func (p *plugin) streamReply(frame []byte) map[string]any {
	var env map[string]sonic.NoCopyRawMessage
	if err := sonic.Unmarshal(frame, &env); err != nil {
		return map[string]any{"id": 0, "transport_error": "decode: " + err.Error()}
	}
	var id uint64
	var name string
	_ = sonic.Unmarshal(env["id"], &id)
	_ = sonic.Unmarshal(env["call"], &name)
	reply := map[string]any{"id": id, "call": name}
	c, ok := p.lookup(name)
	if !ok {
		if p.logCalls {
			log.Printf("-> stream %s id=%d transport_error=unknown call", name, id)
		}
		reply["transport_error"] = "unknown call " + name
		return reply
	}
	payload, ok := env[c.prop]
	if !ok {
		if p.logCalls {
			log.Printf("-> stream %s id=%d transport_error=missing payload %s", name, id, c.prop)
		}
		reply["transport_error"] = "missing payload " + c.prop
		return reply
	}
	p.callsTotal.Add(1)
	resp, err := p.run("stream", id, c, payload)
	if err != nil {
		p.errorsTotal.Add(1)
		reply["transport_error"] = err.Error()
		return reply
	}
	reply[c.prop] = resp
	return reply
}

func fail(ctx *fasthttp.RequestCtx, status int, msg string) {
	write(ctx, status, transportError{Error: msg})
}

func write(ctx *fasthttp.RequestCtx, status int, v any) {
	b, err := sonic.Marshal(v)
	if err != nil {
		log.Printf("encode: %v", err)
		b, status = []byte(`{"error":"encode failed"}`), fasthttp.StatusInternalServerError
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(status)
	ctx.SetBody(b)
}

func main() {
	addr := flag.String("addr", envOr("GUARDRAIL_ADDR", defaultAddr), "listen address")
	mode := flag.String("mode", envOr("GUARDRAIL_MODE", "block"), "block or mask (the Init config overrides it)")
	logCalls := flag.Bool("log-calls", envOr("GUARDRAIL_LOG_CALLS", "true") == "true", "log the input and output of every hook call")
	logMaxBytes := flag.Int("log-max-bytes", envInt("GUARDRAIL_LOG_MAX_BYTES", 4096), "bytes of a logged payload before it is cut; 0 = whole payload")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("MERIDIAN|1|tcp|%s\n", ln.Addr())
	log.SetOutput(os.Stdout)
	p := newPlugin(*mode)
	p.logCalls, p.logMaxBytes = *logCalls, *logMaxBytes
	if p.logCalls {
		log.Printf("call logging on (max %d bytes per payload)", p.logMaxBytes)
	}
	srv := &fasthttp.Server{Handler: p.handler(), MaxRequestBodySize: 64 << 20}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Fatal(err)
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
