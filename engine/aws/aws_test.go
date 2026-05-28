package engine_aws

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	bungo "github.com/piotr-nierobisz/BunGo"
)

func TestNormalizePath(t *testing.T) {
	t.Parallel()
	if got := normalizePath("", "/foo"); got != "/foo" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePath("bar", ""); got != "/bar" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePath("", ""); got != "/" {
		t.Fatalf("got %q", got)
	}
}

func TestLambdaEngine_translateRequest(t *testing.T) {
	t.Parallel()
	e := NewLambdaEngine()
	req := events.APIGatewayV2HTTPRequest{
		Headers:               map[string]string{"X-A": "b"},
		QueryStringParameters: map[string]string{"q": "1"},
		Body:                  `{"a":1}`,
	}
	b := e.translateRequest(context.Background(), req)
	if b.Headers["x-a"] != "b" || b.Params["q"] != "1" || string(b.Body) != `{"a":1}` {
		t.Fatalf("%#v", b)
	}
}

func TestLambdaEngine_dispatch_API(t *testing.T) {
	e := NewLambdaEngine()
	srv := bungo.NewServer(nil, "")
	srv.APIs["v1:GET:ping"] = bungo.ApiRoute{
		Path:    "ping",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(*bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{StatusCode: 200, Body: map[string]string{"ok": "1"}}, nil
		},
	}

	ev := events.APIGatewayV2HTTPRequest{
		RawPath: "/api/v1/ping",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
				Path:   "/api/v1/ping",
			},
		},
	}
	resp, err := e.dispatch(context.Background(), ev, srv)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, resp.Body)
	}
}

func TestLambdaEngine_dispatch_notFound(t *testing.T) {
	e := NewLambdaEngine()
	srv := bungo.NewServer(nil, "")
	resp, err := e.dispatch(context.Background(), events.APIGatewayV2HTTPRequest{
		RawPath: "/nope",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
				Path:   "/nope",
			},
		},
	}, srv)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal(resp.StatusCode)
	}
}

func TestLambdaEngine_dispatch_APICookies(t *testing.T) {
	e := NewLambdaEngine()
	srv := bungo.NewServer(nil, "")
	srv.APIs["v1:GET:set"] = bungo.ApiRoute{
		Path:    "set",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(*bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]string{"ok": "1"},
				Cookies: []bungo.Cookie{
					{Name: "session", Value: "abc", Path: "/", HttpOnly: true, Secure: true, SameSite: bungo.SameSiteStrict},
					{Name: "", Value: "skip"},
				},
			}, nil
		},
	}

	ev := events.APIGatewayV2HTTPRequest{
		RawPath: "/api/v1/set",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
				Path:   "/api/v1/set",
			},
		},
	}
	resp, err := e.dispatch(context.Background(), ev, srv)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d: %v", len(resp.Cookies), resp.Cookies)
	}
	want := []string{"session=abc", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"}
	for _, fragment := range want {
		if !strings.Contains(resp.Cookies[0], fragment) {
			t.Fatalf("cookie %q missing %q", resp.Cookies[0], fragment)
		}
	}
}

func TestLambdaEngine_SetCookieConverter(t *testing.T) {
	e := NewLambdaEngine()
	e.SetCookieConverter(func(c bungo.Cookie) string { return "OVERRIDE=" + c.Name })
	srv := bungo.NewServer(nil, "")
	srv.APIs["v1:GET:c"] = bungo.ApiRoute{
		Path:    "c",
		Version: "v1",
		Method:  http.MethodGet,
		Handler: func(*bungo.Request) (bungo.APIResponse, error) {
			return bungo.APIResponse{
				StatusCode: 200,
				Body:       map[string]string{"ok": "1"},
				Cookies:    []bungo.Cookie{{Name: "session"}},
			}, nil
		},
	}
	ev := events.APIGatewayV2HTTPRequest{
		RawPath: "/api/v1/c",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: http.MethodGet, Path: "/api/v1/c"},
		},
	}
	resp, err := e.dispatch(context.Background(), ev, srv)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Cookies) != 1 || resp.Cookies[0] != "OVERRIDE=session" {
		t.Fatalf("custom converter not applied: %#v", resp.Cookies)
	}
	e.SetCookieConverter(nil)
	if e.cookieConverter == nil {
		t.Fatal("nil converter should restore default")
	}
}

func TestLambdaEngine_initHandler_emptyWebDir(t *testing.T) {
	e := NewLambdaEngine()
	srv := bungo.NewServer(nil, "")
	h, err := e.initHandler(srv)
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("nil handler")
	}
	_, err = h(context.Background(), events.APIGatewayV2HTTPRequest{})
	if err != nil {
		t.Fatal(err)
	}
}
