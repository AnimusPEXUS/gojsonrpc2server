package gojsonrpc2server

import (
	"context"

	"github.com/sourcegraph/jsonrpc2"
)

type Destructable interface {
	Destroy()
}

type AppContext interface {
	CreateAppContextSession(Destructable) (AppContextSession, error)
}

type AppContextSession interface {
	RPCHandle(*RPCHandleContext)
	Destroy()
}

type RPCHandleContext struct {
	Ctx               context.Context
	Server            *Server
	Session           *Session
	AppContext        AppContext
	AppContextSession AppContextSession
	Conn              *jsonrpc2.Conn
	Req               *jsonrpc2.Request
}
