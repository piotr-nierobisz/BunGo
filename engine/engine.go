package engine

import "github.com/piotr-nierobisz/BunGo"

// Invoker represents the execution environment starting the server
type Invoker interface {
	Start(address string, srv *bungo.Server) error
}
