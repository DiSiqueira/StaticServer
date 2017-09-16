package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// A Server defines parameters for running an HTTP server.
// The zero value for Server is a valid configuration.
type Server struct {
	Port    uint16
	Handler Handler // handler to invoke, http.DefaultServeMux if nil

	ErrorLog *log.Logger

	inShutdown    int32     // accessed atomically (non-zero means we're in Shutdown)
	nextProtoOnce sync.Once // guards setupHTTP2_* init
	nextProtoErr  error     // result of http2.ConfigureServer if used

	mu         sync.Mutex
	listeners  map[net.Listener]struct{}
	activeConn map[*conn]struct{}
	doneChan   chan struct{}
}

func ListenAndServe(port uint16, handler Handler) error {
	server := &Server{Port: port, Handler: handler}
	return server.ListenAndServe()
}

func (srv *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", srv.Port))
	if err != nil {
		return err
	}
	return srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
}

func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()

	for {
		rw, e := l.Accept()
		if e != nil {
			return e
		}
		c := srv.newConn(rw)
		go c.serve()
	}
}

func (srv *Server) newConn(rwc net.Conn) *conn {
	c := &conn{
		server: srv,
		rwc:    rwc,
	}
	return c
}

func (s *Server) logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

type conn struct {
	// server is the server on which the connection arrived.
	// Immutable; never nil.
	server *Server

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc

	// rwc is the underlying network connection.
	// This is never wrapped by other types and is the value given out
	// to CloseNotifier callers. It is usually of type *net.TCPConn or
	// *tls.Conn.
	rwc net.Conn

	// remoteAddr is rwc.RemoteAddr().String(). It is not populated synchronously
	// inside the Listener's Accept goroutine, as some implementations block.
	// It is populated immediately inside the (*conn).serve goroutine.
	// This is the value of a Handler's (*Request).RemoteAddr.
	remoteAddr string

	// tlsState is the TLS connection state when using TLS.
	// nil means not TLS.
	tlsState *tls.ConnectionState

	// werr is set to the first write error to rwc.
	// It is set via checkConnErrorWriter{w}, where bufw writes.
	werr error

	// r is bufr's read source. It's a wrapper around rwc that provides
	// io.LimitedReader-style limiting (while reading request headers)
	// and functionality to support CloseNotifier. See *connReader docs.
	r *connReader

	// bufr reads from r.
	bufr *bufio.Reader

	// bufw writes to checkConnErrorWriter{c}, which populates werr on error.
	bufw *bufio.Writer

	curReq atomic.Value // of *response (which has a Request in it)

	curState atomic.Value // of ConnState
}

func (c *conn) serve() {
	c.remoteAddr = c.rwc.RemoteAddr().String()

	c.r = &connReader{conn: c}
	c.bufr = newBufioReader(c.r)
	c.bufw = newBufioWriterSize(checkConnErrorWriter{c}, 4<<10)

	for {
		w, err := c.readRequest()
		if err != nil {
			return
		}

		c.server.Handler.ServeHTTP(w, w.req)

		w.finishRequest()
		return
	}
}

func (c *conn) readRequest() (w *response, err error) {
	req, err := readRequest(c.bufr)
	if err != nil {
		return nil, err
	}

	w = &response{
		conn:          c,
		req:           req,
		handlerHeader: make(Header),
		contentLength: -1,
	}

	w.cw.res = w
	w.w = newBufioWriterSize(&w.cw, bufferBeforeChunkingSize)
	return w, nil
}

const bufferBeforeChunkingSize = 2048

func readRequest(b *bufio.Reader) (req *Request, err error) {
	tp := newTextprotoReader(b)
	req = new(Request)

	// First line: GET /index.html HTTP/1.0
	s, err := tp.ReadLine()
	if err != nil {
		return nil, err
	}

	var ok bool
	req.RequestURI, ok = parseRequestLine(s)
	if !ok {
		return nil, &badStringError{"malformed HTTP request", s}
	}

	if req.URL, err = url.ParseRequestURI(req.RequestURI); err != nil {
		return nil, err
	}

	return req, nil
}

// parseRequestLine parses "GET /foo HTTP/1.1" into its three parts.
func parseRequestLine(line string) (requestURI string, ok bool) {
	s1 := strings.Index(line, " ")
	s2 := strings.Index(line[s1+1:], " ")
	if s1 < 0 || s2 < 0 {
		return
	}
	s2 += s1 + 1
	return line[s1+1 : s2], true
}

var textprotoReaderPool sync.Pool

func newTextprotoReader(br *bufio.Reader) *textproto.Reader {
	if v := textprotoReaderPool.Get(); v != nil {
		tr := v.(*textproto.Reader)
		tr.R = br
		return tr
	}
	return textproto.NewReader(br)
}

func (w *response) finishRequest() {
	w.w.Flush()
	putBufioWriter(w.w)
	w.cw.close()
}

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	if pool := bufioWriterPool(bw.Available()); pool != nil {
		pool.Put(bw)
	}
}

var (
	bufioReaderPool   sync.Pool
	bufioWriter2kPool sync.Pool
	bufioWriter4kPool sync.Pool
)

func bufioWriterPool(size int) *sync.Pool {
	switch size {
	case 2 << 10:
		return &bufioWriter2kPool
	case 4 << 10:
		return &bufioWriter4kPool
	}
	return nil
}
