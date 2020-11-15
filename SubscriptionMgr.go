package gojsonrpc2server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	uuid "github.com/satori/go.uuid"
	"github.com/sourcegraph/jsonrpc2"
)

type SubscriptionMgrSession struct {
	mgr                       *SubscriptionMgr
	descriptor                string
	unsubscribing_descriptors []string

	jsonrpc2_conn        *jsonrpc2.Conn
	jsonrpc2_conn_cancel context.CancelFunc

	destroy_guard *sync.Once
}

func NewSubscriptionMgrSession(
	mgr *SubscriptionMgr,
	descriptor string,
	parameters interface{},
) (*SubscriptionMgrSession, error) {

	self := &SubscriptionMgrSession{
		mgr:           mgr,
		descriptor:    descriptor,
		destroy_guard: &sync.Once{},
	}

	conn, err := self.mgr.options.GetNewConnection()

	// tcp, err := net.Dial("tcp", self.mgr.options.Context.options.CmdLineOptions.ServerInternal)
	// if err != nil {
	// 	return nil, err
	// }

	// if self.mgr.options.Context.options.ServerOptions.EnableTLS {
	// 	tcp = tls.Client(tcp, self.mgr.options.Context.options.ServerOptions.TLSConfig)
	// }

	bs := jsonrpc2.NewBufferedStream(conn, &jsonrpc2.VarintObjectCodec{})

	ctx, jsonrpc2_conn_cancel := context.WithCancel(context.Background())
	self.jsonrpc2_conn_cancel = jsonrpc2_conn_cancel

	h := jsonrpc2.Handler(self)

	if self.mgr.options.UseAsyncHandler {
		h = jsonrpc2.AsyncHandler(h)
	}

	jsonrpc2_conn := jsonrpc2.NewConn(ctx, bs, h)

	self.jsonrpc2_conn = jsonrpc2_conn

	var res interface{}
	self.mgr.Log("calling serverinternal for subscription")
	err = jsonrpc2_conn.Call(ctx, self.mgr.options.RemoteCommand, parameters, &res)
	if err != nil {
		return nil, err
	}
	self.mgr.Log("calling serverinternal for subscription err:", err)

	go func() {
		select {
		case <-ctx.Done():
		case <-jsonrpc2_conn.DisconnectNotify():
		}
		self.Destroy()
	}()

	return self, nil
}

func (self *SubscriptionMgrSession) Destroy() {
	self.destroy_guard.Do(
		func() {
			if self.jsonrpc2_conn_cancel != nil {
				self.jsonrpc2_conn_cancel()
			}

			if self.jsonrpc2_conn != nil {
				self.jsonrpc2_conn.Close()
			}
		},
	)
}

func (self *SubscriptionMgrSession) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	var uuid_o_s string

	{
		uuid_o := uuid.NewV4()
		// if err != nil {
		// 	// TODO: get better logger
		// 	log.Println("error:", "can't create new uuid")
		// 	return
		// }
		uuid_o_s = uuid_o.String()
	}

	responder := NewHandleResponder(
		ctx,
		conn,
		req,
		uuid_o_s,
		log.Println, // TODO: get better logger
	)

	defer responder.Defer()

	self.mgr.options.RespHandler(req, uuid_o_s)
	responder.Reply("ok")

}

type SubscriptionMgrOptions struct {
	// Context          *Context
	UseAsyncHandler           bool
	GetNewConnection          func() (net.Conn, error)
	RemoteCommand             string
	GetDescriptorForParameter func(parameter interface{}) string
	RespHandler               func(request *jsonrpc2.Request, uuid_str string)
}

type SubscriptionMgr struct {
	options *SubscriptionMgrOptions

	descriptor_subscriptions       map[string]*SubscriptionMgrSession
	descriptor_subscriptions_mutex *sync.RWMutex
}

func NewSubscriptionMgr(options *SubscriptionMgrOptions) *SubscriptionMgr {
	self := &SubscriptionMgr{
		options:                        options,
		descriptor_subscriptions:       make(map[string]*SubscriptionMgrSession),
		descriptor_subscriptions_mutex: &sync.RWMutex{},
	}
	return self
}

func (self *SubscriptionMgr) Log(txt ...interface{}) {
	t := []interface{}{fmt.Sprintf("[SubscriptionMgr]")}
	t = append(t, txt...)
	log.Println(t...)
}

func (self *SubscriptionMgr) Subscriptions(descriptor string) (unsubscribing_descriptors []string, err error) {

	if _, ok := self.descriptor_subscriptions[descriptor]; !ok {
		return
	}

	x := self.descriptor_subscriptions[descriptor].unsubscribing_descriptors
	unsubscribing_descriptors = make([]string, len(x))
	copy(unsubscribing_descriptors, x)
	return
}

func (self *SubscriptionMgr) Subscribe(
	parameter interface{},
) (descriptor string, unsubscribing_descriptor string, err error) {
	self.descriptor_subscriptions_mutex.Lock()
	defer func() {
		self.descriptor_subscriptions_mutex.Unlock()
	}()

	descriptor = self.options.GetDescriptorForParameter(parameter)

	mgr_sess, ok := self.descriptor_subscriptions[descriptor]
	if !ok {
		mgr_sess, err = NewSubscriptionMgrSession(self, descriptor, parameter)
		if err != nil {
			return
		}
		self.descriptor_subscriptions[descriptor] = mgr_sess
	}

	unsubscribing_descriptor = uuid.NewV4().String()

	mgr_sess.unsubscribing_descriptors = append(mgr_sess.unsubscribing_descriptors, unsubscribing_descriptor)

	self.Log(
		fmt.Sprintf(
			"new subscribtion to %s created. currently subscribed %d",
			descriptor,
			len(mgr_sess.unsubscribing_descriptors),
		),
	)

	return
}

func (self *SubscriptionMgr) UnsubscribeAllDescriptors(unsubscribing_descriptor string) {
	self.descriptor_subscriptions_mutex.Lock()
	defer self.descriptor_subscriptions_mutex.Unlock()

	var descriptors []string

	for k, _ := range self.descriptor_subscriptions {
		descriptors = append(descriptors, k)
	}

	for _, i := range descriptors {
		self.inUnsubscribe(i, unsubscribing_descriptor)
	}

}

func (self *SubscriptionMgr) Unsubscribe(descriptor string, unsubscribing_descriptor string) {
	self.descriptor_subscriptions_mutex.Lock()
	defer self.descriptor_subscriptions_mutex.Unlock()

	self.inUnsubscribe(descriptor, unsubscribing_descriptor)
}

func (self *SubscriptionMgr) inUnsubscribe(descriptor string, unsubscribing_descriptor string) {

	mgr_sess, ok := self.descriptor_subscriptions[descriptor]
	if !ok || mgr_sess == nil {
		return
	}

	for i := len(mgr_sess.unsubscribing_descriptors) - 1; i != -1; i -= 1 {
		session := mgr_sess.unsubscribing_descriptors[i]
		if session == unsubscribing_descriptor {
			mgr_sess.unsubscribing_descriptors = append(mgr_sess.unsubscribing_descriptors[:i], mgr_sess.unsubscribing_descriptors[i+1:]...)
		}
	}

	self.Log(
		fmt.Sprintf(
			"removed subscriber from %s. now them %d",
			descriptor,
			len(mgr_sess.unsubscribing_descriptors),
		),
	)

	if len(mgr_sess.unsubscribing_descriptors) == 0 {
		self.Log(
			fmt.Sprintf("Descriptor `%s` have 0 subscribers. destroying it..", descriptor),
		)
		delete(self.descriptor_subscriptions, descriptor)
		mgr_sess.Destroy()
	}

}
