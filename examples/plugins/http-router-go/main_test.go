package main

import (
	"net"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
)

func serve(t *testing.T) (*fasthttp.Client, *plugin) {
	t.Helper()
	ln := fasthttputil.NewInmemoryListener()
	p := newPlugin()
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

func chat(content string) string {
	return `{"ctx":{"request_id":"r1"},"request":{"request_type":"chat_completion",` +
		`"request":{"provider":"LMStudio","model":"qwen/qwen3.8-27b","input":[{"role":"system","content":"planning is forbidden here"},` +
		`{"role":"user","content":` + content + `}]}}}`
}

func TestRoutesOnWordInUserMessage(t *testing.T) {
	c, p := serve(t)
	code, body := post(t, c, "/prerequesthook", chat(`"Let us BRAINSTORM the launch"`))
	if code != 200 || !strings.Contains(body, `"body_changed":true`) {
		t.Fatalf("route: %d %s", code, body)
	}
	var reply preRequestHookResponse
	if err := sonic.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	var target routeTarget
	if err := sonic.Unmarshal(reply.Request.Request, &target); err != nil {
		t.Fatal(err)
	}
	if target.Provider != "openrouter" || target.Model != "deepseek/deepseek-v4-flash" || reply.Request.RequestType != "chat_completion" {
		t.Fatalf("target: %+v", reply.Request)
	}
	if p.routed.Load() != 1 {
		t.Fatalf("routed = %d", p.routed.Load())
	}
}

func TestContentBlocksAndWholeWords(t *testing.T) {
	c, _ := serve(t)
	code, body := post(t, c, "/prerequesthook", chat(`[{"type":"text","text":"a distributed cache"}]`))
	if code != 200 || !strings.Contains(body, `"body_changed":true`) {
		t.Fatalf("content blocks: %d %s", code, body)
	}
	code, body = post(t, c, "/prerequesthook", chat(`"replanning the undistributed"`))
	if code != 200 || body != "{}" {
		t.Fatalf("substrings must not match: %d %s", code, body)
	}
}

func TestIgnoresSystemMessagesAndOtherTypes(t *testing.T) {
	c, _ := serve(t)
	// "planning" is in the system prompt of every chat(); a plain user message must not route.
	code, body := post(t, c, "/prerequesthook", chat(`"hello"`))
	if code != 200 || body != "{}" {
		t.Fatalf("system prompt: %d %s", code, body)
	}
	code, body = post(t, c, "/prerequesthook", `{"ctx":{},"request":{"request_type":"embedding","request":{"input":"brainstorm"}}}`)
	if code != 200 || body != "{}" {
		t.Fatalf("embedding: %d %s", code, body)
	}
}

func TestInitRules(t *testing.T) {
	c, _ := serve(t)
	code, body := post(t, c, "/init", `{"config":{"rules":[{"words":["cheap"],"provider":"LMStudio","model":"qwen/qwen3.8-27b"}]}}`)
	if code != 200 || body != "{}" {
		t.Fatalf("init: %d %s", code, body)
	}
	code, body = post(t, c, "/prerequesthook", chat(`"brainstorm"`))
	if code != 200 || body != "{}" {
		t.Fatalf("default rule must be replaced: %d %s", code, body)
	}
	// Already on the target: nothing to change.
	code, body = post(t, c, "/prerequesthook", chat(`"the cheap one"`))
	if code != 200 || body != "{}" {
		t.Fatalf("same target: %d %s", code, body)
	}
	code, body = post(t, c, "/init", `{"config":{"rules":[{"words":[],"provider":"x","model":"y"}]}}`)
	if code != 200 || !strings.Contains(body, "words, provider and model are required") {
		t.Fatalf("bad rule: %d %s", code, body)
	}
}
