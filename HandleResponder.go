package gojsonrpc2server

import (
	"context"
	"fmt"
	"strings"

	"github.com/sourcegraph/jsonrpc2"
)

type HandleResponder struct {
	ctx  context.Context
	conn *jsonrpc2.Conn
	req  *jsonrpc2.Request

	uuid string

	log       func(txt ...interface{})
	responded bool
}

func NewHandleResponder(
	ctx context.Context,
	conn *jsonrpc2.Conn,
	req *jsonrpc2.Request,
	uuid string,
	log func(txt ...interface{}),
) *HandleResponder {
	self := &HandleResponder{
		ctx:  ctx,
		conn: conn,
		req:  req,
		uuid: uuid,
		log:  log,
	}

	return self
}

// call this with defer
func (self *HandleResponder) Defer() error {
	if !self.responded {
		self.Log("error: this is default error if handler didn't responded")
		return self.RespError(500, "internal error")
	}
	return nil
}

// Format message and send it using log()
func (self *HandleResponder) Log(txt ...interface{}) {
	t := []interface{}{fmt.Sprintf("[call %s]", self.uuid)}
	t = append(t, txt...)
	self.log(t...)
}

// Format text, make error message and use ReplyWithError() function
func (self *HandleResponder) RespError(code int64, txt ...interface{}) error {
	ret := self.ReplyWithError(
		&jsonrpc2.Error{
			Code:    code,
			Message: strings.TrimRight(fmt.Sprintln(txt...), "\n"),
		},
	)
	return ret
}

// Format message, log it with log() and send copy as error reply to rpc
func (self *HandleResponder) LogRespError(code int64, txt ...interface{}) error {
	e := &jsonrpc2.Error{
		Code:    code,
		Message: strings.TrimRight(fmt.Sprintln(txt...), "\n"),
	}
	self.Log(e.Error())
	return self.RespError(code, e.Error())
}

// Send success reply to rpc
func (self *HandleResponder) Reply(result interface{}) error {
	ret := self.conn.Reply(
		self.ctx,
		self.req.ID,
		result,
	)
	self.responded = true
	return ret
}

// Send error reply to rpc
func (self *HandleResponder) ReplyWithError(respErr *jsonrpc2.Error) error {
	ret := self.conn.ReplyWithError(
		self.ctx,
		self.req.ID,
		respErr,
	)
	self.responded = true
	return ret
}
