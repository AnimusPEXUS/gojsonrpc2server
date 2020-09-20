package gojsonrpc2server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/sourcegraph/jsonrpc2"
)

type SessionOptions struct {
	Server    *Server
	SessionID string
}

type Session struct {
	options *SessionOptions

	session_id string

	client_connection                net.Conn
	client_connection_close_manually bool

	jsonrpc2_conn *jsonrpc2.Conn

	app_context_session AppContextSession

	destroy_guard *sync.Once
}

func NewSession(options *SessionOptions) (*Session, error) {

	self := &Session{
		options:       options,
		session_id:    options.SessionID,
		destroy_guard: &sync.Once{},
	}

	app_session, err := self.options.Server.options.AppContext.CreateAppContextSession(self)
	if err != nil {
		return nil, err
	}

	self.app_context_session = app_session

	return self, nil
}

func (self *Session) GetSessionId() string {
	return self.session_id
}

func (self *Session) Log(txt ...interface{}) {
	t := []interface{}{fmt.Sprintf("[session %s]", self.session_id)}
	t = append(t, txt...)
	self.options.Server.Log(t...)
}

func (self *Session) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {

	session_context := &RPCHandleContext{
		Ctx:               ctx,
		Server:            self.options.Server,
		Session:           self,
		AppContext:        self.options.Server.options.AppContext,
		AppContextSession: self.app_context_session,
		Conn:              conn,
		Req:               req,
	}

	self.app_context_session.RPCHandle(session_context)

	// TODO: cleanups?

}

func (self *Session) HandleConnection(conn net.Conn) {

	self.client_connection = conn
	self.client_connection_close_manually = true

	self.Log("creating buffered streamer")
	buffered_object_stream := jsonrpc2.NewBufferedStream(
		self.client_connection,
		jsonrpc2.VarintObjectCodec{},
	)

	self.HandleBS(buffered_object_stream)

	// TODO: destroy session if connection to DB is lost
}

func (self *Session) HandleBS(bs jsonrpc2.ObjectStream) {

	defer func() {
		self.Log("session handler exiting")
		self.Destroy()
	}()

	self.client_connection_close_manually = false

	ctx := context.Background()

	self.Log("creating RPC connection")

	handler := jsonrpc2.Handler(self)

	if self.options.Server.options.AsyncRequestHandling {
		self.Log("using Async handling")
		handler = jsonrpc2.AsyncHandler(handler)
	}

	jsonrpc2_conn := jsonrpc2.NewConn(
		ctx,
		bs,
		handler,
	)
	self.jsonrpc2_conn = jsonrpc2_conn

	self.Log("RPC is working. now waiting for quit signals")
	select {
	case <-ctx.Done():
		self.Log("got session termination signal using context")
	case <-jsonrpc2_conn.DisconnectNotify():
		self.Log("got session termination signal: incomming connection closed from client side")
	}

	// TODO: destroy session if connection to DB is lost
}

func (self *Session) Destroy() {
	self.destroy_guard.Do(
		func() {

			self.Log("destroy called")

			if self.jsonrpc2_conn != nil {
				self.Log("stopping RPC connection")
				self.jsonrpc2_conn.Close()
				// self.jsonrpc2_conn = nil
			}

			if self.client_connection_close_manually {
				if self.client_connection != nil {
					// TODO: maybe additionnaly signalling to client would be not bad
					self.Log("closing socket connection manually")
					self.client_connection.Close()
					// self.client_connection = nil
				}
			}

			if self.app_context_session != nil {
				self.Log("asking context session to kill self")
				self.app_context_session.Destroy()
			}
			self.Log("session destroyed")
		},
	)
}
