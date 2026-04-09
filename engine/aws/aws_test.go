package engine_aws

import (
	"context"
	"net/http"
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
