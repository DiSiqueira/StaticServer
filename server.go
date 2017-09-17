package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
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
	res    *response
	header Header
}

func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	n, err = cw.res.conn.bufw.Write(p)
	if err != nil {
		cw.res.conn.rwc.Close()
	}
	return
}

func (cw *chunkWriter) flush() {
	cw.writeHeader()
	cw.res.conn.bufw.Flush()
}

func (cw *chunkWriter) close() {
}

func (cw *chunkWriter) writeHeader() {
	cw.res.conn.bufw.WriteString(fmt.Sprintf("HTTP/1.0 %d %s\r\n", cw.res.status, statusText[cw.res.status]))
	cw.header.WriteSubset(cw.res.conn.bufw)
	cw.res.conn.bufw.Write([]byte("\r\n"))
}

type response struct {
	conn          *conn
	req           *Request
	w             *bufio.Writer
	cw            chunkWriter
	handlerHeader Header
	status        int
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
	return n, err
}

type connReader struct {
	conn  *conn
	mu    sync.Mutex
	bgErr error
	cond  *sync.Cond
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
	if cr.bgErr != nil {
		err = cr.bgErr
		cr.unlock()
		return 0, err
	}
	if len(p) == 0 {
		cr.unlock()
		return 0, nil
	}
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

func (w *response) Header() Header {
	return w.handlerHeader
}

func (w *response) WriteHeader(code int) {
	w.status = code

	if w.cw.header == nil {
		w.cw.header = w.handlerHeader.clone()
	}
}

func (w *response) Write(data []byte) (n int, err error) {
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

func Error(w ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}

func NotFound(w ResponseWriter) {
	Error(w, statusText[StatusNotFound], StatusNotFound)
}

func Forbidden(w ResponseWriter) {
	Error(w, statusText[StatusForbidden], StatusForbidden)
}

func InternalServerError(w ResponseWriter) {
	Error(w, statusText[StatusInternalServerError], StatusInternalServerError)
}
