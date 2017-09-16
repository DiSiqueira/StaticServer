package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
)

type Server struct {
	Port     uint16
	Handler  Handler
	ErrorLog *log.Logger
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
	server *Server
	rwc    net.Conn
	werr   error
	r      *connReader
	bufr   *bufio.Reader
	bufw   *bufio.Writer
}

func (c *conn) serve() {
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

type checkConnErrorWriter struct {
	c *conn
}

func (w checkConnErrorWriter) Write(p []byte) (n int, err error) {
	n, err = w.c.rwc.Write(p)
	if err != nil && w.c.werr == nil {
		w.c.werr = err
	}
	return
}
