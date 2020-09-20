package gojsonrpc2server

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/AnimusPEXUS/utils/worker"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
	"github.com/sourcegraph/jsonrpc2"
	jsonrpc2websocket "github.com/sourcegraph/jsonrpc2/websocket"
)

type ServerOptions struct {
	// Verbose bool
	Debug bool

	ListenAtAddresses    string
	ListenAtAddressesWS  string
	AsyncRequestHandling bool

	AppContext AppContext
	EnableTLS  bool
	TLSConfig  *tls.Config
}

type Server struct {
	options *ServerOptions

	mainworker *worker.Worker
	tcpworker  *worker.Worker
	httpworker *worker.Worker
}

func NewServer(opts *ServerOptions) (*Server, error) {

	self := &Server{
		options: opts,
	}

	self.mainworker = worker.New(self.mainThread)
	self.tcpworker = worker.New(self.tcpThread)
	self.httpworker = worker.New(self.httpThread)

	return self, nil
}

func (self *Server) Log(txt ...interface{}) {
	t := []interface{}{"[server]"}
	t = append(t, txt...)
	log.Println(t...)
}

func (self *Server) GetWorker() worker.WorkerI {
	return self.mainworker
}

func (self *Server) Destroy() {

}

func (self *Server) mainThread(
	set_starting func(),
	set_working func(),
	set_stopping func(),
	set_stopped func(),

	is_stop_flag func() bool,
) {

	self.Log("main: thread is starting")
	set_starting()

	defer func() {
		self.Log("main: thread exited")
		set_stopped()
	}()

	set_working()
	for {
		if self.tcpworker.Status().IsStopped() {
			self.Log("tcp worker is stopped: starting")
			self.tcpworker.Start()
		}

		if self.httpworker.Status().IsStopped() {
			self.Log("http worker is stopped: starting")
			self.httpworker.Start()
		}

		if is_stop_flag() {
			break
		}
		time.Sleep(time.Second)
	}
	set_stopping()

}

func (self *Server) tcpThread(
	set_starting func(),
	set_working func(),
	set_stopping func(),
	set_stopped func(),

	is_stop_flag func() bool,
) {
	self.Log("tcp: listening thread is starting")
	set_starting()

	defer func() {
		self.Log("tcp: listening thread exited")
		set_stopped()
	}()

	self.Log("tcp: resolving address:", self.options.ListenAtAddresses)
	tcp_addr, err := net.ResolveTCPAddr("tcp", self.options.ListenAtAddresses)
	if err != nil {
		self.Log("error:", err)
		return
	}

	self.Log("tcp: creating listener")
	listener, err := net.ListenTCP("tcp", tcp_addr)
	if err != nil {
		self.Log("error:", err)
		return
	}
	self.Log("tcp: listening at", listener.Addr().String())

	defer func() {
		listener.Close()
	}()

	go func() {
		for {
			if is_stop_flag() {
				self.Log("tcp: listener got stop signal. exiting..")
				listener.Close()
				break
			}

			time.Sleep(time.Second)
		}
	}()

	set_working()

	self.Log("tcp: worker is working. entering main listening loop")

	for {

		uuid_str := uuid.NewV4()
		// if err != nil {
		// 	self.Log("tcp: can't create new session id on new incomming connection:", err)
		// 	time.Sleep(time.Second)
		// 	continue
		// }

		session_id := uuid_str.String()

		newsession_options := &SessionOptions{
			Server:    self,
			SessionID: session_id,
		}

		newsession, err := NewSession(newsession_options)
		if err != nil {
			self.Log("tcp: error creating session:", err)
			time.Sleep(time.Second)
			continue
		}

		if is_stop_flag() {
			break
		}

		newsession.Log("tcp: waiting for new connection")
		conn, err := listener.Accept()
		if err != nil {
			newsession.Log("tcp: listener connection accepting error:", err)
			newsession.Destroy()
			time.Sleep(time.Second)
			continue
		}

		if self.options.EnableTLS {
			tls_conn := tls.Server(conn, self.options.TLSConfig)
			conn = tls_conn
			newsession.Log("tcp: tls handshake")
			err = tls_conn.Handshake()
			if err != nil {
				newsession.Log("tcp: tls handshake error:", err)
			}
		}

		newsession.Log(
			"tcp: got new connection at",
			conn.LocalAddr().String(),
			"from",
			conn.RemoteAddr().String(),
		)

		go func(s *Session, conn net.Conn) {
			// defer s.Destroy() // connection handler will do it manually
			s.Log("tcp: separated to own routine")
			s.HandleConnection(conn)
		}(newsession, conn)
	}

	set_stopping()
}

type MyHttpHandler struct {
	server *Server
}

func (self *MyHttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	bs := jsonrpc2websocket.NewObjectStream(conn)

	uuid_str := uuid.NewV4()
	// if err != nil {
	// 	self.server.Log("http: can't create new session id on new incomming connection:", err)
	// 	return
	// }

	session_id := uuid_str.String()

	newsession_options := &SessionOptions{
		Server:    self.server,
		SessionID: session_id,
	}

	newsession, err := NewSession(newsession_options)
	if err != nil {
		self.server.Log("http: error creating session:", err)
		return
	}

	newsession.Log(
		"http: got new connection at",
		conn.LocalAddr().String(),
		"from",
		conn.RemoteAddr().String(),
	)

	go func(s *Session, bs jsonrpc2.ObjectStream) {
		// defer s.Destroy() // connection handler will do it manually
		s.Log("http: separated to own routine")
		s.HandleBS(bs)
	}(newsession, bs)
}

func (self *Server) httpThread(
	set_starting func(),
	set_working func(),
	set_stopping func(),
	set_stopped func(),

	is_stop_flag func() bool,
) {
	self.Log("http: main listening thread is starting")
	set_starting()

	defer func() {
		self.Log("http: main listening thread exited")
		set_stopped()
	}()

	set_working()

	var err error

	self.Log("http: worker is working. entering main listening loop")

	for {

		mux_router := mux.NewRouter()
		mux_router.Path("/socket").Handler(&MyHttpHandler{
			server: self,
		})

		s := &http.Server{
			Addr:           self.options.ListenAtAddressesWS,
			Handler:        mux_router,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
			TLSConfig:      self.options.TLSConfig,
		}

		if self.options.EnableTLS {
			self.Log("http: ListenAndServeTLS")
			err = s.ListenAndServeTLS("", "")
			if err != nil {
				self.Log("http: ListenAndServeTLS: ", err)
				time.Sleep(time.Second)
				continue
			}
		} else {
			err = s.ListenAndServe()
			if err != nil {
				self.Log("http: ListenAndServe")
				self.Log("http: ListenAndServe: ", err)
				time.Sleep(time.Second)
				continue
			}
		}

		if is_stop_flag() {
			break
		}

	}

	set_stopping()

}
