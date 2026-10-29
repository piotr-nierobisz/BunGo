// Package bungo is a full-stack web framework that pairs Go request handlers
// with React (JSX/TSX) views. Views are compiled by an embedded esbuild when
// the server starts, so no Node.js toolchain is required, and `bungo build`
// embeds the web directory into a single portable binary.
//
// A Server is created from an Engine (see the engine package) and a web
// directory containing layouts/ and views/. Pages, API routes, WebSocket routes
// and static aliases are registered on the Server, and named SecurityLayer
// handlers can be attached to any of them:
//
//	srv := bungo.NewServer(engine.NewHTTPEngine(), "./web")
//	srv.SetDefaultLayout("base.gohtml")
//	srv.Page(bungo.PageRoute{
//		Path:     "/",
//		Template: "landing.gohtml",
//		View:     "landing.jsx",
//		Handler: func(req *bungo.Request) (map[string]any, error) {
//			return map[string]any{"Title": "Hello"}, nil
//		},
//	})
//	log.Fatal(srv.Serve(8080))
//
// Guides are available at https://github.com/piotr-nierobisz/BunGo/tree/main/docs.
package bungo
