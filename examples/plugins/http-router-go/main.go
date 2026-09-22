// Command http-router-go is a network plugin for Meridian that re-routes chat
// requests: when a user message contains one of the configured words, the
// request's provider and model are replaced in PreRequestHook, the phase the
// core reserves for provider/model decisions, before governance selects a key.
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
	pluginName      = "WordRouter"
	pluginVersion   = "1.0.0"
	protocolVersion = 1
	basePath        = "/api/meridian/plugin/v1"
	defaultAddr     = "0.0.0.0:18083"
)

var hooks = []string{"PreRequestHook"}

// rule routes a request whose user messages contain one of Words (whole words,
// case-insensitive) to Provider/Model. The first matching rule wins.
type rule struct {
	Words    []string `json:"words"`
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
}

// Settings come from the Init config; without one the default rule applies.
type settings struct {
	Rules []rule `json:"rules"`
}

var defaultRules = []rule{{
	Words:    []string{"distributed", "brainstorm", "planning"},
	Provider: "openrouter",
	Model:    "deepseek/deepseek-v4-flash",
}}

type (
	contextDetails struct {
		RequestID string `json:"request_id,omitempty"`
	}
	llmRequest struct {
		RequestType string                 `json:"request_type"`
		Request     sonic.NoCopyRawMessage `json:"request,omitempty"`
	}
	preRequestHookRequest struct {
		Ctx     contextDetails `json:"ctx"`
		Request llmRequest     `json:"request"`
	}
	// routeTarget is the echoed request: only the two fields the host merges
	// into its typed request; everything else stays as the host holds it.
	routeTarget struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	preRequestHookResponse struct {
		Request     *llmRequest `json:"request,omitempty"`
		BodyChanged bool        `json:"body_changed,omitempty"`
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

// chatRequest is the part of the typed chat request the router reads.
type chatRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Input    []struct {
		Role    string                 `json:"role"`
		Content sonic.NoCopyRawMessage `json:"content"`
	} `json:"input"`
}

type compiledRule struct {
	re       *regexp.Regexp
	provider string
	model    string
}

type call struct {
	name string
	prop string
	fn   func(payload []byte) (any, error)
}

type plugin struct {
	calls   []call
	started time.Time

	mu    sync.RWMutex
	rules []compiledRule

	logCalls    bool
	logMaxBytes int

	callsTotal  atomic.Int64
	errorsTotal atomic.Int64
	openStreams atomic.Int32
	routed      atomic.Int64
	lastRoute   atomic.Pointer[string]
}

// routerStats is the plugin-specific GET /router/stats payload.
type routerStats struct {
	Rules     int    `json:"rules"`
	Routed    int64  `json:"routed"`
	LastRoute string `json:"last_route,omitempty"`
}

func newPlugin() *plugin {
	p := &plugin{started: time.Now()}
	_ = p.apply(settings{})
	p.calls = []call{
		{"Capabilities", "capabilities", func([]byte) (any, error) {
			return capabilitiesResponse{Hooks: hooks, ProtocolVersion: protocolVersion, StreamSupported: true}, nil
		}},
		{"Init", "init", p.init},
		{"GetName", "get_name", func([]byte) (any, error) { return map[string]string{"name": pluginName}, nil }},
		{"Cleanup", "cleanup", func([]byte) (any, error) { return struct{}{}, nil }},
		{"PreRequestHook", "pre_request_hook", p.preRequestHook},
	}
	return p
}

func (p *plugin) apply(s settings) error {
	rules := s.Rules
	if len(rules) == 0 {
		rules = defaultRules
	}
	compiled := make([]compiledRule, 0, len(rules))
	for i, r := range rules {
		if len(r.Words) == 0 || r.Provider == "" || r.Model == "" {
			return fmt.Errorf("rules[%d]: words, provider and model are required", i)
		}
		quoted := make([]string, len(r.Words))
		for j, w := range r.Words {
			quoted[j] = regexp.QuoteMeta(strings.TrimSpace(w))
		}
		re, err := regexp.Compile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
		if err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		compiled = append(compiled, compiledRule{re: re, provider: r.Provider, model: r.Model})
	}
	p.mu.Lock()
	p.rules = compiled
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
	log.Printf("init: rules=%d", len(p.rules))
	return struct{}{}, nil
}

// preRequestHook: the text of every user message is matched against the rules;
// on a hit the request is echoed with the new provider and model only.
func (p *plugin) preRequestHook(payload []byte) (any, error) {
	var req preRequestHookRequest
	if err := sonic.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(req.Request.RequestType, "chat_completion") || len(req.Request.Request) == 0 {
		return preRequestHookResponse{}, nil
	}
	var chat chatRequest
	if err := sonic.Unmarshal(req.Request.Request, &chat); err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	text := userText(chat)
	p.mu.RLock()
	rules := p.rules
	p.mu.RUnlock()
	for _, r := range rules {
		if !r.re.MatchString(text) {
			continue
		}
		if chat.Provider == r.provider && chat.Model == r.model {
			return preRequestHookResponse{}, nil
		}
		target, err := sonic.Marshal(routeTarget{Provider: r.provider, Model: r.model})
		if err != nil {
			return nil, err
		}
		p.routed.Add(1)
		last := fmt.Sprintf("%s/%s -> %s/%s", chat.Provider, chat.Model, r.provider, r.model)
		p.lastRoute.Store(&last)
		return preRequestHookResponse{
			Request:     &llmRequest{RequestType: req.Request.RequestType, Request: target},
			BodyChanged: true,
		}, nil
	}
	return preRequestHookResponse{}, nil
}

// userText joins the text of the user messages: a plain string content, or the
// text parts of a content block list.
func userText(chat chatRequest) string {
	var b strings.Builder
	for _, m := range chat.Input {
		if m.Role != "user" || len(m.Content) == 0 {
			continue
		}
		var s string
		if err := sonic.Unmarshal(m.Content, &s); err == nil {
			b.WriteString(s)
			b.WriteByte('\n')
			continue
		}
		var blocks []struct {
			Text string `json:"text"`
		}
		if err := sonic.Unmarshal(m.Content, &blocks); err == nil {
			for _, bl := range blocks {
				b.WriteString(bl.Text)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
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
	r.GET(basePath+"/router/stats", p.serveRouterStats)
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

// run dispatches one call and, with call logging on, prints its input and output.
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

func clip(b []byte, limit int) string {
	if limit <= 0 || len(b) <= limit {
		return string(b)
	}
	return fmt.Sprintf("%s…(+%d bytes)", b[:limit], len(b)-limit)
}

func (p *plugin) serveHealth(ctx *fasthttp.RequestCtx) {
	write(ctx, fasthttp.StatusOK, healthResponse{Status: "ok", Components: map[string]string{"stream": "ok"}})
}

func (p *plugin) serveRouterStats(ctx *fasthttp.RequestCtx) {
	p.mu.RLock()
	n := len(p.rules)
	p.mu.RUnlock()
	st := routerStats{Rules: n, Routed: p.routed.Load()}
	if last := p.lastRoute.Load(); last != nil {
		st.LastRoute = *last
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
		reply["transport_error"] = "unknown call " + name
		return reply
	}
	payload, ok := env[c.prop]
	if !ok {
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
	addr := flag.String("addr", envOr("ROUTER_ADDR", defaultAddr), "listen address")
	logCalls := flag.Bool("log-calls", envOr("ROUTER_LOG_CALLS", "true") == "true", "log the input and output of every hook call")
	logMaxBytes := flag.Int("log-max-bytes", envInt("ROUTER_LOG_MAX_BYTES", 4096), "bytes of a logged payload before it is cut; 0 = whole payload")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("MERIDIAN|1|tcp|%s\n", ln.Addr())
	log.SetOutput(os.Stdout)
	p := newPlugin()
	p.logCalls, p.logMaxBytes = *logCalls, *logMaxBytes
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
