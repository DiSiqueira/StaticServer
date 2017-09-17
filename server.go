package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

var (
	crlf = []byte("\r\n")
)

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(int)
}

type chunkWriter struct {
	res         *response
	header      Header
	wroteHeader bool
}

func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	if !cw.wroteHeader {
		cw.writeHeader(p)
	}
	n, err = cw.res.conn.bufw.Write(p)
	if err != nil {
		cw.res.conn.rwc.Close()
	}
	return
}

func (cw *chunkWriter) flush() {
	if !cw.wroteHeader {
		cw.writeHeader(nil)
	}
	cw.res.conn.bufw.Flush()
}

func (cw *chunkWriter) close() {
	if !cw.wroteHeader {
		cw.writeHeader(nil)
	}
}

type response struct {
	conn            *conn
	req             *Request
	w               *bufio.Writer
	cw              chunkWriter
	handlerHeader   Header
	calledHeader    bool
	written         int64
	status          int
	closeAfterReply bool
}

type writerOnly struct {
	io.Writer
}

func srcIsRegularFile(src io.Reader) (isRegular bool, err error) {
	switch v := src.(type) {
	case *os.File:
		fi, err := v.Stat()
		if err != nil {
			return false, err
		}
		return fi.Mode().IsRegular(), nil
	case *io.LimitedReader:
		return srcIsRegularFile(v.R)
	default:
		return
	}
}

func (w *response) ReadFrom(src io.Reader) (n int64, err error) {
	rf, ok := w.conn.rwc.(io.ReaderFrom)
	regFile, err := srcIsRegularFile(src)
	if err != nil {
		return 0, err
	}
	if !ok || !regFile {
		bufp := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(bufp)
		return io.CopyBuffer(writerOnly{w}, src, *bufp)
	}

	w.w.Flush()
	w.cw.flush()

	n0, err := rf.ReadFrom(src)
	n += n0
	w.written += n0
	return n, err
}

type connReader struct {
	conn    *conn
	mu      sync.Mutex
	byteBuf [1]byte
	bgErr   error
	cond    *sync.Cond
	inRead  bool
}

func (cr *connReader) lock() {
	cr.mu.Lock()
	if cr.cond == nil {
		cr.cond = sync.NewCond(&cr.mu)
	}
}

func (cr *connReader) unlock() { cr.mu.Unlock() }

func (cr *connReader) Read(p []byte) (n int, err error) {
	cr.lock()
	if cr.inRead {
		cr.unlock()
		panic("invalid concurrent Body.Read call")
	}
	if cr.bgErr != nil {
		err = cr.bgErr
		cr.unlock()
		return 0, err
	}
	if len(p) == 0 {
		cr.unlock()
		return 0, nil
	}
	cr.inRead = true
	cr.unlock()
	n, err = cr.conn.rwc.Read(p)

	cr.cond.Broadcast()
	return n, err
}

var copyBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func newBufioReader(r io.Reader) *bufio.Reader {
	if v := bufioReaderPool.Get(); v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	return bufio.NewReader(r)
}

func newBufioWriterSize(w io.Writer, size int) *bufio.Writer {
	pool := bufioWriterPool(size)
	if pool != nil {
		if v := pool.Get(); v != nil {
			bw := v.(*bufio.Writer)
			bw.Reset(w)
			return bw
		}
	}
	return bufio.NewWriterSize(w, size)
}

const DefaultMaxHeaderBytes = 1 << 20

func (w *response) Header() Header {
	w.calledHeader = true
	return w.handlerHeader
}

func (w *response) WriteHeader(code int) {
	w.status = code

	if w.calledHeader && w.cw.header == nil {
		w.cw.header = w.handlerHeader.clone()
	}
}

func (cw *chunkWriter) writeHeader(p []byte) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true

	w := cw.res

	header := cw.header
	owned := header != nil
	if !owned {
		header = w.handlerHeader
	}
	var excludeHeader map[string]bool

	w.closeAfterReply = true

	w.conn.bufw.WriteString(statusLine(w.status))
	cw.header.WriteSubset(w.conn.bufw, excludeHeader)
	w.conn.bufw.Write(crlf)
}

var (
	statusMu    sync.RWMutex
	statusLines = make(map[int]string)
)

func statusLine(code int) string {
	key := code
	statusMu.RLock()
	line, ok := statusLines[key]
	statusMu.RUnlock()
	if ok {
		return line
	}

	proto := "HTTP/1.0"
	codestring := fmt.Sprintf("%03d", code)
	text, ok := statusText[code]
	if !ok {
		text = "status code " + codestring
	}
	line = proto + " " + codestring + " " + text + "\r\n"
	if ok {
		statusMu.Lock()
		defer statusMu.Unlock()
		statusLines[key] = line
	}
	return line
}

func (w *response) Write(data []byte) (n int, err error) {
	return w.write(data)
}

func (w *response) write(data []byte) (n int, err error) {
	lenData := len(data)
	if lenData == 0 {
		return 0, nil
	}

	return w.w.Write(data)
}

func (w *response) Flush() {
	w.w.Flush()
	w.cw.flush()
}

type HandlerFunc func(ResponseWriter, *Request)

func Error(w ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}

func NotFound(w ResponseWriter, r *Request) { Error(w, "404 page not found", StatusNotFound) }

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
