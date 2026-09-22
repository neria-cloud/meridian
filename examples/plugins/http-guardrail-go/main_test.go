package main

import (
	"net"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
)

func serve(t *testing.T, mode string) (*fasthttp.Client, *plugin) {
	t.Helper()
	ln := fasthttputil.NewInmemoryListener()
	p := newPlugin(mode)
	go func() { _ = fasthttp.Serve(ln, p.handler()) }()
	t.Cleanup(func() { ln.Close() })
	return &fasthttp.Client{Dial: func(string) (net.Conn, error) { return ln.Dial() }}, p
}

func post(t *testing.T, c *fasthttp.Client, path, body string) (int, string) {
	t.Helper()
	req, resp := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)
	req.SetRequestURI("http://plugin" + basePath + path)
	req.Header.SetMethod(fasthttp.MethodPost)
	req.SetBodyString(body)
	if err := c.Do(req, resp); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode(), strings.TrimSpace(string(resp.Body()))
}

const chatWithPhone = `{"ctx":{"request_id":"r1"},"request":{"request_type":"chat_completion",` +
	`"request":{"provider":"openai","model":"gpt","input":[{"role":"system","content":"be nice"},` +
	`{"role":"user","content":"call me at +1 555 010 0100 please"}]}}}`

func TestBlockMode(t *testing.T) {
	c, _ := serve(t, "block")
	code, body := post(t, c, "/prellmhook", chatWithPhone)
	if code != 200 || !strings.Contains(body, `"status_code":400`) || !strings.Contains(body, "phone number") {
		t.Fatalf("block: %d %s", code, body)
	}
	code, body = post(t, c, "/prellmhook", `{"ctx":{},"request":{"request_type":"chat_completion","request":{"input":[{"role":"user","content":"hello"}]}}}`)
	if code != 200 || body != "{}" {
		t.Fatalf("clean request: %d %s", code, body)
	}
}

func TestMaskAndUnmask(t *testing.T) {
	c, p := serve(t, "mask")
	code, body := post(t, c, "/prellmhook", chatWithPhone)
	if code != 200 || !strings.Contains(body, `"body_changed":true`) || strings.Contains(body, "555 010") || !strings.Contains(body, "<ph:") {
		t.Fatalf("mask: %d %s", code, body)
	}
	var reply preLLMHookResponse
	if err := sonic.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := sonic.Unmarshal(reply.Request.Request, &req); err != nil {
		t.Fatal(err)
	}
	if req["model"] != "gpt" || len(req["input"].([]any)) != 2 {
		t.Fatalf("masked request lost fields: %v", req)
	}
	token := tokenRe.FindString(body)
	if p.vault.size() != 1 || token == "" {
		t.Fatalf("vault size %d token %q", p.vault.size(), token)
	}
	resp := `{"ctx":{"request_id":"r1"},"response":{"request_type":"chat_completion","response":{"choices":[{"message":{"role":"assistant","content":"sure, ` + token + ` it is"}}]}}}`
	code, body = post(t, c, "/postllmhook", resp)
	if code != 200 || !strings.Contains(body, "+1 555 010 0100") || !strings.Contains(body, `"body_changed":true`) {
		t.Fatalf("unmask: %d %s", code, body)
	}
	code, body = post(t, c, "/postllmhook", `{"ctx":{},"response":{"response":{"choices":[{"message":{"content":"no tokens"}}]}}}`)
	if code != 200 || body != "{}" {
		t.Fatalf("untouched response must not echo: %d %s", code, body)
	}
}

func TestForbiddenCombinationAcrossParts(t *testing.T) {
	c, _ := serve(t, "mask")
	if code, body := post(t, c, "/init", `{"config":{"mode":"mask","forbidden_combination":["(?i)wire transfer","(?i)urgent"]}}`); code != 200 || body != "{}" {
		t.Fatalf("init: %d %s", code, body)
	}
	both := `{"ctx":{},"request":{"request_type":"chat_completion","request":{"input":[` +
		`{"role":"system","content":"this is URGENT"},{"role":"user","content":"please do a wire transfer"}]}}}`
	code, body := post(t, c, "/prellmhook", both)
	if code != 200 || !strings.Contains(body, "forbidden combination") {
		t.Fatalf("combination: %d %s", code, body)
	}
	one := `{"ctx":{},"request":{"request_type":"chat_completion","request":{"input":[{"role":"user","content":"please do a wire transfer"}]}}}`
	if code, body := post(t, c, "/prellmhook", one); code != 200 || body != "{}" {
		t.Fatalf("single pattern must pass: %d %s", code, body)
	}
}

func TestStreamUnmaskAcrossChunks(t *testing.T) {
	c, p := serve(t, "mask")
	token := p.vault.put("+1 555 010 0100")
	chunk := func(id int, content string, final bool) string {
		fr := "null"
		if final {
			fr = `"stop"`
		}
		return `{"ctx":{"request_id":"s1"},"chunk":{"index":` + itoa(id) + `,"request_type":"chat_completion_stream",` +
			`"chunk":{"choices":[{"delta":{"content":` + quote(content) + `},"finish_reason":` + fr + `}]}}}`
	}
	// The token is split over three chunks: "call <ph:12" | "34…>" | " now".
	first, second := token[:8], token[8:]
	code, body := post(t, c, "/httptransportstreamchunkhook", chunk(0, "call "+first, false))
	if code != 200 || !strings.Contains(body, `"content":"call "`) {
		t.Fatalf("chunk 0 must hold the partial token: %d %s", code, body)
	}
	code, body = post(t, c, "/httptransportstreamchunkhook", chunk(1, second+" now", false))
	if code != 200 || !strings.Contains(body, "+1 555 010 0100 now") {
		t.Fatalf("chunk 1 must restore the joined token: %d %s", code, body)
	}
	code, body = post(t, c, "/httptransportstreamchunkhook", chunk(2, "", true))
	if code != 200 {
		t.Fatalf("final chunk: %d %s", code, body)
	}
	if _, ok := p.streams.Load("s1"); ok {
		t.Fatal("stream state must be released on the final chunk")
	}
}

func TestHealthAndStatus(t *testing.T) {
	c, _ := serve(t, "block")
	code, body, err := c.Get(nil, "http://plugin"+basePath+"/health")
	if err != nil || code != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("health: %d %s %v", code, body, err)
	}
	code, body, err = c.Get(nil, "http://plugin"+basePath+"/status")
	if err != nil || code != 200 || !strings.Contains(string(body), `"name":"PhoneGuardrail"`) {
		t.Fatalf("status: %d %s %v", code, body, err)
	}
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i))) }

func quote(s string) string {
	b, _ := sonic.Marshal(s)
	return string(b)
}
