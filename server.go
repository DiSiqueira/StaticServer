package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	ErrBodyNotAllowed = errors.New("http: request method or response status code does not allow body")
	ErrContentLength  = errors.New("http: wrote more than the declared Content-Length")
)

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(int)
}

type Flusher interface {
	Flush()
}

type Hijacker interface {
	Hijack() (net.Conn, *bufio.ReadWriter, error)
}

type CloseNotifier interface {
	CloseNotify() <-chan bool
}

type chunkWriter struct {
	res         *response
	header      Header
	wroteHeader bool
	chunking    bool
}

var (
	crlf       = []byte("\r\n")
	colonSpace = []byte(": ")
)

func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	if !cw.wroteHeader {
		cw.writeHeader(p)
	}
	if cw.chunking {
		_, err = fmt.Fprintf(cw.res.conn.bufw, "%x\r\n", len(p))
		if err != nil {
			cw.res.conn.rwc.Close()
			return
		}
	}
	n, err = cw.res.conn.bufw.Write(p)
	if cw.chunking && err == nil {
		_, err = cw.res.conn.bufw.Write(crlf)
	}
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
	if cw.chunking {
		bw := cw.res.conn.bufw

		bw.WriteString("0\r\n")

		bw.WriteString("\r\n")
	}
}

type response struct {
	conn                *conn
	req                 *Request
	cancelCtx           context.CancelFunc
	wroteHeader         bool
	wroteContinue       bool
	wants10KeepAlive    bool
	wantsClose          bool
	w                   *bufio.Writer
	cw                  chunkWriter
	handlerHeader       Header
	calledHeader        bool
	written             int64
	contentLength       int64
	status              int
	closeAfterReply     bool
	requestBodyLimitHit bool
}

type atomicBool int32

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

	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}

	w.w.Flush()
	w.cw.flush()

	if !w.cw.chunking {
		n0, err := rf.ReadFrom(src)
		n += n0
		w.written += n0
		return n, err
	}

	n0, err := io.Copy(writerOnly{w}, src)
	n += n0
	return n, err
}

type readResult struct {
	n   int
	err error
	b   byte
}

type connReader struct {
	conn    *conn
	mu      sync.Mutex
	hasByte bool
	byteBuf [1]byte
	bgErr   error
	cond    *sync.Cond
	inRead  bool
	aborted bool
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
	if cr.hasByte {
		p[0] = cr.byteBuf[0]
		cr.hasByte = false
		cr.unlock()
		return 1, nil
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

type expectContinueReader struct {
	resp       *response
	readCloser io.ReadCloser
	closed     bool
	sawEOF     bool
}

func (ecr *expectContinueReader) Read(p []byte) (n int, err error) {
	if ecr.closed {
		return 0, errors.New("http: invalid Read on closed Body")
	}
	n, err = ecr.readCloser.Read(p)
	if err == io.EOF {
		ecr.sawEOF = true
	}
	return
}

const TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

func (w *response) Header() Header {
	if w.cw.header == nil && w.wroteHeader && !w.cw.wroteHeader {

		w.cw.header = w.handlerHeader.clone()
	}
	w.calledHeader = true
	return w.handlerHeader
}

const maxPostHandlerReadBytes = 256 << 10

func (w *response) WriteHeader(code int) {
	if w.wroteHeader {
		w.conn.server.logf("http: multiple response.WriteHeader calls")
		return
	}
	w.wroteHeader = true
	w.status = code

	if w.calledHeader && w.cw.header == nil {
		w.cw.header = w.handlerHeader.clone()
	}

	if cl := w.handlerHeader.get("Content-Length"); cl != "" {
		v, err := strconv.ParseInt(cl, 10, 64)
		if err == nil && v >= 0 {
			w.contentLength = v
		} else {
			w.conn.server.logf("http: invalid Content-Length of %q", cl)
			w.handlerHeader.Del("Content-Length")
		}
	}
}

type extraHeader struct {
	contentType      string
	connection       string
	transferEncoding string
	date             []byte
	contentLength    []byte
}

var extraHeaderKeys = [][]byte{
	[]byte("Content-Type"),
	[]byte("Connection"),
	[]byte("Transfer-Encoding"),
}

var (
	headerContentLength = []byte("Content-Length: ")
	headerDate          = []byte("Date: ")
)

func (h extraHeader) Write(w *bufio.Writer) {
	if h.date != nil {
		w.Write(headerDate)
		w.Write(h.date)
		w.Write(crlf)
	}
	if h.contentLength != nil {
		w.Write(headerContentLength)
		w.Write(h.contentLength)
		w.Write(crlf)
	}
	for i, v := range []string{h.contentType, h.connection, h.transferEncoding} {
		if v != "" {
			w.Write(extraHeaderKeys[i])
			w.Write(colonSpace)
			w.WriteString(v)
			w.Write(crlf)
		}
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
	var setHeader extraHeader

	w.closeAfterReply = true

	w.conn.bufw.WriteString(statusLine(w.req, w.status))
	cw.header.WriteSubset(w.conn.bufw, excludeHeader)
	setHeader.Write(w.conn.bufw)
	w.conn.bufw.Write(crlf)
}

var (
	statusMu    sync.RWMutex
	statusLines = make(map[int]string)
)

func statusLine(req *Request, code int) string {
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
	return w.write(len(data), data, "")
}

func (w *response) WriteString(data string) (n int, err error) {
	return w.write(len(data), nil, data)
}

func (w *response) write(lenData int, dataB []byte, dataS string) (n int, err error) {
	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}
	if lenData == 0 {
		return 0, nil
	}

	w.written += int64(lenData)
	if w.contentLength != -1 && w.written > w.contentLength {
		return 0, ErrContentLength
	}
	if dataB != nil {
		return w.w.Write(dataB)
	} else {
		return w.w.WriteString(dataS)
	}
}

func (w *response) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}
	w.w.Flush()
	w.cw.flush()
}

type closeWriter interface {
	CloseWrite() error
}

type HandlerFunc func(ResponseWriter, *Request)

func Error(w ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}

func NotFound(w ResponseWriter, r *Request) { Error(w, "404 page not found", StatusNotFound) }

type muxEntry struct {
	explicit bool
	h        Handler
	pattern  string
}

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
