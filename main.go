package main

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/net/http2/hpack"
	"golang.org/x/net/lex/httplex"
	"io"
	"io/ioutil"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"net/http/httptrace"
)

const (
	StatusContinue           = 100 // RFC 7231, 6.2.1
	StatusSwitchingProtocols = 101 // RFC 7231, 6.2.2
	StatusProcessing         = 102 // RFC 2518, 10.1

	StatusOK                   = 200 // RFC 7231, 6.3.1
	StatusCreated              = 201 // RFC 7231, 6.3.2
	StatusAccepted             = 202 // RFC 7231, 6.3.3
	StatusNonAuthoritativeInfo = 203 // RFC 7231, 6.3.4
	StatusNoContent            = 204 // RFC 7231, 6.3.5
	StatusResetContent         = 205 // RFC 7231, 6.3.6
	StatusPartialContent       = 206 // RFC 7233, 4.1
	StatusMultiStatus          = 207 // RFC 4918, 11.1
	StatusAlreadyReported      = 208 // RFC 5842, 7.1
	StatusIMUsed               = 226 // RFC 3229, 10.4.1

	StatusMultipleChoices   = 300 // RFC 7231, 6.4.1
	StatusMovedPermanently  = 301 // RFC 7231, 6.4.2
	StatusFound             = 302 // RFC 7231, 6.4.3
	StatusSeeOther          = 303 // RFC 7231, 6.4.4
	StatusNotModified       = 304 // RFC 7232, 4.1
	StatusUseProxy          = 305 // RFC 7231, 6.4.5
	_                       = 306 // RFC 7231, 6.4.6 (Unused)
	StatusTemporaryRedirect = 307 // RFC 7231, 6.4.7
	StatusPermanentRedirect = 308 // RFC 7538, 3

	StatusBadRequest                   = 400 // RFC 7231, 6.5.1
	StatusUnauthorized                 = 401 // RFC 7235, 3.1
	StatusPaymentRequired              = 402 // RFC 7231, 6.5.2
	StatusForbidden                    = 403 // RFC 7231, 6.5.3
	StatusNotFound                     = 404 // RFC 7231, 6.5.4
	StatusMethodNotAllowed             = 405 // RFC 7231, 6.5.5
	StatusNotAcceptable                = 406 // RFC 7231, 6.5.6
	StatusProxyAuthRequired            = 407 // RFC 7235, 3.2
	StatusRequestTimeout               = 408 // RFC 7231, 6.5.7
	StatusConflict                     = 409 // RFC 7231, 6.5.8
	StatusGone                         = 410 // RFC 7231, 6.5.9
	StatusLengthRequired               = 411 // RFC 7231, 6.5.10
	StatusPreconditionFailed           = 412 // RFC 7232, 4.2
	StatusRequestEntityTooLarge        = 413 // RFC 7231, 6.5.11
	StatusRequestURITooLong            = 414 // RFC 7231, 6.5.12
	StatusUnsupportedMediaType         = 415 // RFC 7231, 6.5.13
	StatusRequestedRangeNotSatisfiable = 416 // RFC 7233, 4.4
	StatusExpectationFailed            = 417 // RFC 7231, 6.5.14
	StatusTeapot                       = 418 // RFC 7168, 2.3.3
	StatusUnprocessableEntity          = 422 // RFC 4918, 11.2
	StatusLocked                       = 423 // RFC 4918, 11.3
	StatusFailedDependency             = 424 // RFC 4918, 11.4
	StatusUpgradeRequired              = 426 // RFC 7231, 6.5.15
	StatusPreconditionRequired         = 428 // RFC 6585, 3
	StatusTooManyRequests              = 429 // RFC 6585, 4
	StatusRequestHeaderFieldsTooLarge  = 431 // RFC 6585, 5
	StatusUnavailableForLegalReasons   = 451 // RFC 7725, 3

	StatusInternalServerError           = 500 // RFC 7231, 6.6.1
	StatusNotImplemented                = 501 // RFC 7231, 6.6.2
	StatusBadGateway                    = 502 // RFC 7231, 6.6.3
	StatusServiceUnavailable            = 503 // RFC 7231, 6.6.4
	StatusGatewayTimeout                = 504 // RFC 7231, 6.6.5
	StatusHTTPVersionNotSupported       = 505 // RFC 7231, 6.6.6
	StatusVariantAlsoNegotiates         = 506 // RFC 2295, 8.1
	StatusInsufficientStorage           = 507 // RFC 4918, 11.5
	StatusLoopDetected                  = 508 // RFC 5842, 7.2
	StatusNotExtended                   = 510 // RFC 2774, 7
	StatusNetworkAuthenticationRequired = 511 // RFC 6585, 6
)

var statusText = map[int]string{
	StatusContinue:           "Continue",
	StatusSwitchingProtocols: "Switching Protocols",
	StatusProcessing:         "Processing",

	StatusOK:                   "OK",
	StatusCreated:              "Created",
	StatusAccepted:             "Accepted",
	StatusNonAuthoritativeInfo: "Non-Authoritative Information",
	StatusNoContent:            "No Content",
	StatusResetContent:         "Reset Content",
	StatusPartialContent:       "Partial Content",
	StatusMultiStatus:          "Multi-Status",
	StatusAlreadyReported:      "Already Reported",
	StatusIMUsed:               "IM Used",

	StatusMultipleChoices:   "Multiple Choices",
	StatusMovedPermanently:  "Moved Permanently",
	StatusFound:             "Found",
	StatusSeeOther:          "See Other",
	StatusNotModified:       "Not Modified",
	StatusUseProxy:          "Use Proxy",
	StatusTemporaryRedirect: "Temporary Redirect",
	StatusPermanentRedirect: "Permanent Redirect",

	StatusBadRequest:                   "Bad Request",
	StatusUnauthorized:                 "Unauthorized",
	StatusPaymentRequired:              "Payment Required",
	StatusForbidden:                    "Forbidden",
	StatusNotFound:                     "Not Found",
	StatusMethodNotAllowed:             "Method Not Allowed",
	StatusNotAcceptable:                "Not Acceptable",
	StatusProxyAuthRequired:            "Proxy Authentication Required",
	StatusRequestTimeout:               "Request Timeout",
	StatusConflict:                     "Conflict",
	StatusGone:                         "Gone",
	StatusLengthRequired:               "Length Required",
	StatusPreconditionFailed:           "Precondition Failed",
	StatusRequestEntityTooLarge:        "Request Entity Too Large",
	StatusRequestURITooLong:            "Request URI Too Long",
	StatusUnsupportedMediaType:         "Unsupported Media Type",
	StatusRequestedRangeNotSatisfiable: "Requested Range Not Satisfiable",
	StatusExpectationFailed:            "Expectation Failed",
	StatusTeapot:                       "I'm a teapot",
	StatusUnprocessableEntity:          "Unprocessable Entity",
	StatusLocked:                       "Locked",
	StatusFailedDependency:             "Failed Dependency",
	StatusUpgradeRequired:              "Upgrade Required",
	StatusPreconditionRequired:         "Precondition Required",
	StatusTooManyRequests:              "Too Many Requests",
	StatusRequestHeaderFieldsTooLarge:  "Request Header Fields Too Large",
	StatusUnavailableForLegalReasons:   "Unavailable For Legal Reasons",

	StatusInternalServerError:           "Internal Server Error",
	StatusNotImplemented:                "Not Implemented",
	StatusBadGateway:                    "Bad Gateway",
	StatusServiceUnavailable:            "Service Unavailable",
	StatusGatewayTimeout:                "Gateway Timeout",
	StatusHTTPVersionNotSupported:       "HTTP Version Not Supported",
	StatusVariantAlsoNegotiates:         "Variant Also Negotiates",
	StatusInsufficientStorage:           "Insufficient Storage",
	StatusLoopDetected:                  "Loop Detected",
	StatusNotExtended:                   "Not Extended",
	StatusNetworkAuthenticationRequired: "Network Authentication Required",
}

func main() {
	fs := FileServer(Dir("files"))
	Handle("/", fs)

	log.Println("Listening...")
	ListenAndServe(":8080", nil)
}

type http2Server struct {
	// MaxHandlers limits the number of http.Handler ServeHTTP goroutines
	// which may run at a time over all connections.
	// Negative or zero no limit.
	// TODO: implement
	MaxHandlers int

	// MaxConcurrentStreams optionally specifies the number of
	// concurrent streams that each client may have open at a
	// time. This is unrelated to the number of http.Handler goroutines
	// which may be active globally, which is MaxHandlers.
	// If zero, MaxConcurrentStreams defaults to at least 100, per
	// the HTTP/2 spec's recommendations.
	MaxConcurrentStreams uint32

	// MaxReadFrameSize optionally specifies the largest frame
	// this server is willing to read. A valid value is between
	// 16k and 16M, inclusive. If zero or otherwise invalid, a
	// default value is used.
	MaxReadFrameSize uint32

	// PermitProhibitedCipherSuites, if true, permits the use of
	// cipher suites prohibited by the HTTP/2 spec.
	PermitProhibitedCipherSuites bool

	// IdleTimeout specifies how long until idle clients should be
	// closed with a GOAWAY frame. PING frames are not considered
	// activity for the purposes of IdleTimeout.
	IdleTimeout time.Duration

	// NewWriteScheduler constructs a write scheduler for a connection.
	// If nil, a default scheduler is chosen.
	NewWriteScheduler func() http2WriteScheduler
}

type http2WriteScheduler interface {
	// OpenStream opens a new stream in the write scheduler.
	// It is illegal to call this with streamID=0 or with a streamID that is
	// already open -- the call may panic.
	OpenStream(streamID uint32, options http2OpenStreamOptions)

	// CloseStream closes a stream in the write scheduler. Any frames queued on
	// this stream should be discarded. It is illegal to call this on a stream
	// that is not open -- the call may panic.
	CloseStream(streamID uint32)

	// AdjustStream adjusts the priority of the given stream. This may be called
	// on a stream that has not yet been opened or has been closed. Note that
	// RFC 7540 allows PRIORITY frames to be sent on streams in any state. See:
	// https://tools.ietf.org/html/rfc7540#section-5.1
	AdjustStream(streamID uint32, priority http2PriorityParam)

	// Push queues a frame in the scheduler. In most cases, this will not be
	// called with wr.StreamID()!=0 unless that stream is currently open. The one
	// exception is RST_STREAM frames, which may be sent on idle or closed streams.
	Push(wr http2FrameWriteRequest)

	// Pop dequeues the next frame to write. Returns false if no frames can
	// be written. Frames with a given wr.StreamID() are Pop'd in the same
	// order they are Push'd.
	Pop() (wr http2FrameWriteRequest, ok bool)
}

type http2FrameWriteRequest struct {
	// write is the interface value that does the writing, once the
	// WriteScheduler has selected this frame to write. The write
	// functions are all defined in write.go.
	write http2writeFramer

	// stream is the stream on which this frame will be written.
	// nil for non-stream frames like PING and SETTINGS.
	stream *http2stream

	// done, if non-nil, must be a buffered channel with space for
	// 1 message and is sent the return value from write (or an
	// earlier error) when the frame has been written.
	done chan error
}

const http2frameHeaderLen = 9

type http2pipe struct {
	mu       sync.Mutex
	c        sync.Cond // c.L lazily initialized to &p.mu
	b        http2pipeBuffer
	err      error         // read error once empty. non-nil means closed.
	breakErr error         // immediate read error (caller doesn't see rest of b)
	donec    chan struct{} // closed on error
	readFn   func()        // optional code to run in Read before error
}

type http2pipeBuffer interface {
	Len() int
	io.Writer
	io.Reader
}

type http2closeWaiter chan struct{}

type http2contextContext interface {
	context.Context
}

type http2flow struct {
	// n is the number of DATA bytes we're allowed to send.
	// A flow is kept both on a conn and a per-stream.
	n int32

	// conn points to the shared connection-level flow that is
	// shared by all streams on that conn. It is nil for the flow
	// that's on the conn directly.
	conn *http2flow
}

type http2streamState int

type http2stream struct {
	// immutable:
	sc        *http2serverConn
	id        uint32
	body      *http2pipe       // non-nil if expecting DATA frames
	cw        http2closeWaiter // closed wait stream transitions to closed state
	ctx       http2contextContext
	cancelCtx func()

	// owned by serverConn's serve loop:
	bodyBytes        int64        // body bytes seen so far
	declBodyBytes    int64        // or -1 if undeclared
	flow             http2flow    // limits writing from Handler to client
	inflow           http2flow    // what the client is allowed to POST/etc to us
	parent           *http2stream // or nil
	numTrailerValues int64
	weight           uint8
	state            http2streamState
	resetQueued      bool   // RST_STREAM queued for write; set by sc.resetStream
	gotTrailerHeader bool   // HEADER frame for trailers was seen
	wroteHeaders     bool   // whether we wrote headers (not status 100)
	reqBuf           []byte // if non-nil, body pipe buffer to return later at EOF

	trailer    Header // accumulated trailers
	reqTrailer Header // handler's Request.Trailer
}

type http2bufferedWriter struct {
	w  io.Writer     // immutable
	bw *bufio.Writer // non-nil when data is buffered
}

type http2Frame interface {
	Header() http2FrameHeader

	// invalidate is called by Framer.ReadFrame to make this
	// frame's buffers as being invalid, since the subsequent
	// frame will reuse them.
	invalidate()
}

type http2FrameType uint8
type http2Flags uint8

type http2FrameHeader struct {
	valid bool // caller can access []byte fields in the Frame

	// Type is the 1 byte frame type. There are ten standard frame
	// types, but extension frame types may be written by WriteRawFrame
	// and will be returned by ReadFrame (as UnknownFrame).
	Type http2FrameType

	// Flags are the 1 byte of 8 potential bit flags per frame.
	// They are specific to the frame type.
	Flags http2Flags

	// Length is the length of the frame, not including the 9 byte header.
	// The maximum size is one byte less than 16MB (uint24), but only
	// frames up to 16KB are allowed without peer agreement.
	Length uint32

	// StreamID is which stream this frame is for. Certain frames
	// are not stream-specific, in which case this field is 0.
	StreamID uint32
}

type http2readFrameResult struct {
	f   http2Frame // valid until readMore is called
	err error

	// readMore should be called once the consumer no longer needs or
	// retains f. After readMore, f is invalid and more frames can be
	// read.
	readMore func()
}
type http2startPushRequest struct {
	parent *http2stream
	method string
	url    *url.URL
	header Header
	done   chan error
}
type http2frameWriteResult struct {
	wr  http2FrameWriteRequest // what was written (or attempted)
	err error                  // result of the writeFrame call
}
type http2bodyReadMsg struct {
	st *http2stream
	n  int
}

type http2goroutineLock uint64

type http2serverConn struct {
	// Immutable:
	srv              *http2Server
	hs               *Server
	conn             net.Conn
	bw               *http2bufferedWriter // writing to conn
	handler          Handler
	baseCtx          http2contextContext
	framer           *http2Framer
	doneServing      chan struct{}               // closed when serverConn.serve ends
	readFrameCh      chan http2readFrameResult   // written by serverConn.readFrames
	wantWriteFrameCh chan http2FrameWriteRequest // from handlers -> serve
	wantStartPushCh  chan http2startPushRequest  // from handlers -> serve
	wroteFrameCh     chan http2frameWriteResult  // from writeFrameAsync -> serve, tickles more frame writes
	bodyReadCh       chan http2bodyReadMsg       // from handlers -> serve
	testHookCh       chan func(int)              // code to run on the serve loop
	flow             http2flow                   // conn-wide (not stream-specific) outbound flow control
	inflow           http2flow                   // conn-wide inbound flow control
	tlsState         *tls.ConnectionState        // shared by all handlers, like net/http
	remoteAddrStr    string
	writeSched       http2WriteScheduler

	// Everything following is owned by the serve loop; use serveG.check():
	serveG                http2goroutineLock // used to verify funcs are on serve()
	pushEnabled           bool
	sawFirstSettings      bool // got the initial SETTINGS frame after the preface
	needToSendSettingsAck bool
	unackedSettings       int    // how many SETTINGS have we sent without ACKs?
	clientMaxStreams      uint32 // SETTINGS_MAX_CONCURRENT_STREAMS from client (our PUSH_PROMISE limit)
	advMaxStreams         uint32 // our SETTINGS_MAX_CONCURRENT_STREAMS advertised the client
	curClientStreams      uint32 // number of open streams initiated by the client
	curPushedStreams      uint32 // number of open streams initiated by server push
	maxClientStreamID     uint32 // max ever seen from client (odd), or 0 if there have been no client requests
	maxPushPromiseID      uint32 // ID of the last push promise (even), or 0 if there have been no pushes
	streams               map[uint32]*http2stream
	initialWindowSize     int32
	maxFrameSize          int32
	headerTableSize       uint32
	peerMaxHeaderListSize uint32            // zero means unknown (default)
	canonHeader           map[string]string // http2-lower-case -> Go-Canonical-Case
	writingFrame          bool              // started writing a frame (on serve goroutine or separate)
	writingFrameAsync     bool              // started a frame on its own goroutine but haven't heard back on wroteFrameCh
	needsFrameFlush       bool              // last frame write wasn't a flush
	inGoAway              bool              // we've started to or sent GOAWAY
	inFrameScheduleLoop   bool              // whether we're in the scheduleFrameWrite loop
	needToSendGoAway      bool              // we need to schedule a GOAWAY frame write
	goAwayCode            http2ErrCode
	shutdownTimerCh       <-chan time.Time // nil until used
	shutdownTimer         *time.Timer      // nil until used
	idleTimer             *time.Timer      // nil if unused
	idleTimerCh           <-chan time.Time // nil if unused

	// Owned by the writeFrameAsync goroutine:
	headerWriteBuf bytes.Buffer
	hpackEncoder   *hpack.Encoder
}

type http2ErrCode uint32

type http2Framer struct {
	r         io.Reader
	lastFrame http2Frame
	errDetail error

	// lastHeaderStream is non-zero if the last frame was an
	// unfinished HEADERS/CONTINUATION.
	lastHeaderStream uint32

	maxReadSize uint32
	headerBuf   [http2frameHeaderLen]byte

	// TODO: let getReadBuf be configurable, and use a less memory-pinning
	// allocator in server.go to minimize memory pinned for many idle conns.
	// Will probably also need to make frame invalidation have a hook too.
	getReadBuf func(size uint32) []byte
	readBuf    []byte // cache for default getReadBuf

	maxWriteSize uint32 // zero means unlimited; TODO: implement

	w    io.Writer
	wbuf []byte

	// AllowIllegalWrites permits the Framer's Write methods to
	// write frames that do not conform to the HTTP/2 spec. This
	// permits using the Framer to test other HTTP/2
	// implementations' conformance to the spec.
	// If false, the Write methods will prefer to return an error
	// rather than comply.
	AllowIllegalWrites bool

	// AllowIllegalReads permits the Framer's ReadFrame method
	// to return non-compliant frames or frame orders.
	// This is for testing and permits using the Framer to test
	// other HTTP/2 implementations' conformance to the spec.
	// It is not compatible with ReadMetaHeaders.
	AllowIllegalReads bool

	// ReadMetaHeaders if non-nil causes ReadFrame to merge
	// HEADERS and CONTINUATION frames together and return
	// MetaHeadersFrame instead.
	ReadMetaHeaders *hpack.Decoder

	// MaxHeaderListSize is the http2 MAX_HEADER_LIST_SIZE.
	// It's used only if ReadMetaHeaders is set; 0 means a sane default
	// (currently 16MB)
	// If the limit is hit, MetaHeadersFrame.Truncated is set true.
	MaxHeaderListSize uint32

	logReads, logWrites bool

	debugFramer       *http2Framer // only use for logging written writes
	debugFramerBuf    *bytes.Buffer
	debugReadLoggerf  func(string, ...interface{})
	debugWriteLoggerf func(string, ...interface{})
}

type http2writeContext interface {
	Framer() *http2Framer
	Flush() error
	CloseConn() error
	// HeaderEncoder returns an HPACK encoder that writes to the
	// returned buffer.
	HeaderEncoder() (*hpack.Encoder, *bytes.Buffer)
}

type http2writeFramer interface {
	writeFrame(http2writeContext) error

	// staysWithinBuffer reports whether this writer promises that
	// it will only write less than or equal to size bytes, and it
	// won't Flush the write context.
	staysWithinBuffer(size int) bool
}
type http2PriorityParam struct {
	// StreamDep is a 31-bit stream identifier for the
	// stream that this stream depends on. Zero means no
	// dependency.
	StreamDep uint32

	// Exclusive is whether the dependency is exclusive.
	Exclusive bool

	// Weight is the stream's zero-indexed weight. It should be
	// set together with StreamDep, or neither should be set.  Per
	// the spec, "Add one to the value to obtain a weight between
	// 1 and 256."
	Weight uint8
}

type http2OpenStreamOptions struct {
	// PusherID is zero if the stream was initiated by the client. Otherwise,
	// PusherID names the stream that pushed the newly opened stream.
	PusherID uint32
}

func http2configureServer18(h1 *Server, h2 *http2Server) error {
	if h2.IdleTimeout == 0 {
		if h1.IdleTimeout != 0 {
			h2.IdleTimeout = h1.IdleTimeout
		} else {
			h2.IdleTimeout = h1.ReadTimeout
		}
	}
	return nil
}

func http2isBadCipher(cipher uint16) bool {
	switch cipher {
	case tls.TLS_RSA_WITH_RC4_128_SHA,
		tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:

		return true
	default:
		return false
	}
}

const http2NextProtoTLS = "h2"

func http2ConfigureServer(s *Server, conf *http2Server) error {
	if s == nil {
		panic("nil *http.Server")
	}
	if conf == nil {
		conf = new(http2Server)
	}
	if err := http2configureServer18(s, conf); err != nil {
		return err
	}

	if s.TLSConfig == nil {
		s.TLSConfig = new(tls.Config)
	} else if s.TLSConfig.CipherSuites != nil {
		// If they already provided a CipherSuite list, return
		// an error if it has a bad order or is missing
		// ECDHE_RSA_WITH_AES_128_GCM_SHA256.
		const requiredCipher = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
		haveRequired := false
		sawBad := false
		for i, cs := range s.TLSConfig.CipherSuites {
			if cs == requiredCipher {
				haveRequired = true
			}
			if http2isBadCipher(cs) {
				sawBad = true
			} else if sawBad {
				return fmt.Errorf("http2: TLSConfig.CipherSuites index %d contains an HTTP/2-approved cipher suite (%#04x), but it comes after unapproved cipher suites. With this configuration, clients that don't support previous, approved cipher suites may be given an unapproved one and reject the connection.", i, cs)
			}
		}
		if !haveRequired {
			return fmt.Errorf("http2: TLSConfig.CipherSuites is missing HTTP/2-required TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")
		}
	}

	s.TLSConfig.PreferServerCipherSuites = true

	haveNPN := false
	for _, p := range s.TLSConfig.NextProtos {
		if p == http2NextProtoTLS {
			haveNPN = true
			break
		}
	}
	if !haveNPN {
		s.TLSConfig.NextProtos = append(s.TLSConfig.NextProtos, http2NextProtoTLS)
	}

	if s.TLSNextProto == nil {
		s.TLSNextProto = map[string]func(*Server, *tls.Conn, Handler){}
	}
	protoHandler := func(hs *Server, c *tls.Conn, h Handler) {
		if http2testHookOnConn != nil {
			http2testHookOnConn()
		}
		conf.ServeConn(c, &http2ServeConnOpts{
			Handler:    h,
			BaseConfig: hs,
		})
	}
	s.TLSNextProto[http2NextProtoTLS] = protoHandler
	return nil
}

type http2ServeConnOpts struct {
	// BaseConfig optionally sets the base configuration
	// for values. If nil, defaults are used.
	BaseConfig *Server

	// Handler specifies which handler to use for processing
	// requests. If nil, BaseConfig.Handler is used. If BaseConfig
	// or BaseConfig.Handler is nil, http.DefaultServeMux is used.
	Handler Handler
}

func (o *http2ServeConnOpts) baseConfig() *Server {
	if o != nil && o.BaseConfig != nil {
		return o.BaseConfig
	}
	return new(Server)
}

type contextKey struct {
	name string
}

func http2serverConnBaseContext(c net.Conn, opts *http2ServeConnOpts) (ctx http2contextContext, cancel func()) {
	ctx, cancel = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, LocalAddrContextKey, c.LocalAddr())
	if hs := opts.baseConfig(); hs != nil {
		ctx = context.WithValue(ctx, ServerContextKey, hs)
	}
	return
}

func http2newBufferedWriter(w io.Writer) *http2bufferedWriter {
	return &http2bufferedWriter{w: w}
}

const http2initialWindowSize = 65535 // 6.9.2 Initial Flow Control Window Size
const http2initialMaxFrameSize = 16384
const http2initialHeaderTableSize = 4096

func (s *http2Server) maxConcurrentStreams() uint32 {
	if v := s.MaxConcurrentStreams; v > 0 {
		return v
	}
	return http2defaultMaxStreams
}

var http2DebugGoroutines = os.Getenv("DEBUG_HTTP2_GOROUTINES") == "1"

func http2newGoroutineLock() http2goroutineLock {
	if !http2DebugGoroutines {
		return 0
	}
	return http2goroutineLock(http2curGoroutineID())
}

var http2littleBuf = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 64)
		return &buf
	},
}
var http2goroutineSpace = []byte("goroutine ")

func http2curGoroutineID() uint64 {
	bp := http2littleBuf.Get().(*[]byte)
	defer http2littleBuf.Put(bp)
	b := *bp
	b = b[:runtime.Stack(b, false)]

	b = bytes.TrimPrefix(b, http2goroutineSpace)
	i := bytes.IndexByte(b, ' ')
	if i < 0 {
		panic(fmt.Sprintf("No space found in %q", b))
	}
	b = b[:i]
	n, err := http2parseUintBytes(b, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse goroutine ID out of %q: %v", b, err))
	}
	return n
}

func http2cutoff64(base int) uint64 {
	if base < 2 {
		return 0
	}
	return (1<<64-1)/uint64(base) + 1
}

func http2parseUintBytes(s []byte, base int, bitSize int) (n uint64, err error) {
	var cutoff, maxVal uint64

	if bitSize == 0 {
		bitSize = int(strconv.IntSize)
	}

	s0 := s
	switch {
	case len(s) < 1:
		err = strconv.ErrSyntax
		goto Error

	case 2 <= base && base <= 36:

	case base == 0:

		switch {
		case s[0] == '0' && len(s) > 1 && (s[1] == 'x' || s[1] == 'X'):
			base = 16
			s = s[2:]
			if len(s) < 1 {
				err = strconv.ErrSyntax
				goto Error
			}
		case s[0] == '0':
			base = 8
		default:
			base = 10
		}

	default:
		err = errors.New("invalid base " + strconv.Itoa(base))
		goto Error
	}

	n = 0
	cutoff = http2cutoff64(base)
	maxVal = 1<<uint(bitSize) - 1

	for i := 0; i < len(s); i++ {
		var v byte
		d := s[i]
		switch {
		case '0' <= d && d <= '9':
			v = d - '0'
		case 'a' <= d && d <= 'z':
			v = d - 'a' + 10
		case 'A' <= d && d <= 'Z':
			v = d - 'A' + 10
		default:
			n = 0
			err = strconv.ErrSyntax
			goto Error
		}
		if int(v) >= base {
			n = 0
			err = strconv.ErrSyntax
			goto Error
		}

		if n >= cutoff {

			n = 1<<64 - 1
			err = strconv.ErrRange
			goto Error
		}
		n *= uint64(base)

		n1 := n + uint64(v)
		if n1 < n || n1 > maxVal {

			n = 1<<64 - 1
			err = strconv.ErrRange
			goto Error
		}
		n = n1
	}

	return n, nil

Error:
	return n, &strconv.NumError{Func: "ParseUint", Num: string(s0), Err: err}
}

func (o *http2ServeConnOpts) handler() Handler {
	if o != nil {
		if o.Handler != nil {
			return o.Handler
		}
		if o.BaseConfig != nil && o.BaseConfig.Handler != nil {
			return o.BaseConfig.Handler
		}
	}
	return DefaultServeMux
}

func (r *Request) ProtoAtLeast(major, minor int) bool {
	return r.ProtoMajor > major ||
		r.ProtoMajor == major && r.ProtoMinor >= minor
}

func (s *http2Server) ServeConn(c net.Conn, opts *http2ServeConnOpts) {
	baseCtx, cancel := http2serverConnBaseContext(c, opts)
	defer cancel()

	sc := &http2serverConn{
		srv:               s,
		hs:                opts.baseConfig(),
		conn:              c,
		baseCtx:           baseCtx,
		remoteAddrStr:     c.RemoteAddr().String(),
		bw:                http2newBufferedWriter(c),
		handler:           opts.handler(),
		streams:           make(map[uint32]*http2stream),
		readFrameCh:       make(chan http2readFrameResult),
		wantWriteFrameCh:  make(chan http2FrameWriteRequest, 8),
		wantStartPushCh:   make(chan http2startPushRequest, 8),
		wroteFrameCh:      make(chan http2frameWriteResult, 1),
		bodyReadCh:        make(chan http2bodyReadMsg),
		doneServing:       make(chan struct{}),
		clientMaxStreams:  math.MaxUint32,
		advMaxStreams:     s.maxConcurrentStreams(),
		initialWindowSize: http2initialWindowSize,
		maxFrameSize:      http2initialMaxFrameSize,
		headerTableSize:   http2initialHeaderTableSize,
		serveG:            http2newGoroutineLock(),
		pushEnabled:       true,
	}

	if sc.hs.WriteTimeout != 0 {
		sc.conn.SetWriteDeadline(time.Time{})
	}

	if s.NewWriteScheduler != nil {
		sc.writeSched = s.NewWriteScheduler()
	} else {
		sc.writeSched = http2NewRandomWriteScheduler()
	}

	sc.flow.add(http2initialWindowSize)
	sc.inflow.add(http2initialWindowSize)
	sc.hpackEncoder = hpack.NewEncoder(&sc.headerWriteBuf)

	fr := http2NewFramer(sc.bw, c)
	fr.ReadMetaHeaders = hpack.NewDecoder(http2initialHeaderTableSize, nil)
	fr.MaxHeaderListSize = sc.maxHeaderListSize()
	fr.SetMaxReadFrameSize(s.maxReadFrameSize())
	sc.framer = fr

	if tc, ok := c.(http2connectionStater); ok {
		sc.tlsState = new(tls.ConnectionState)
		*sc.tlsState = tc.ConnectionState()

		if sc.tlsState.Version < tls.VersionTLS12 {
			sc.rejectConn(http2ErrCodeInadequateSecurity, "TLS version too low")
			return
		}

		if sc.tlsState.ServerName == "" {

		}

		if !s.PermitProhibitedCipherSuites && http2isBadCipher(sc.tlsState.CipherSuite) {

			sc.rejectConn(http2ErrCodeInadequateSecurity, fmt.Sprintf("Prohibited TLS 1.2 Cipher Suite: %x", sc.tlsState.CipherSuite))
			return
		}
	}

	if hook := http2testHookGetServerConn; hook != nil {
		hook(sc)
	}
	sc.serve()
}

func (g http2goroutineLock) check() {
	if !http2DebugGoroutines {
		return
	}
	if http2curGoroutineID() != uint64(g) {
		panic("running on the wrong goroutine")
	}
}

var http2testHookOnPanicMu *sync.Mutex // nil except in tests

func (sc *http2serverConn) notePanic() {

	if http2testHookOnPanicMu != nil {
		http2testHookOnPanicMu.Lock()
		defer http2testHookOnPanicMu.Unlock()
	}
	if http2testHookOnPanic != nil {
		if e := recover(); e != nil {
			if http2testHookOnPanic(sc, e) {
				panic(e)
			}
		}
	}
}

var http2testHookOnPanic func(sc *http2serverConn, panicVal interface{}) (rePanic bool)

func (sc *http2serverConn) closeAllStreamsOnConnClose() {
	sc.serveG.check()
	for _, st := range sc.streams {
		sc.closeStream(st, http2errClientDisconnected)
	}
}

func (st *http2stream) isPushed() bool {
	return st.id%2 == 0
}

func (sc *http2serverConn) setConnState(state ConnState) {
	if sc.hs.ConnState != nil {
		sc.hs.ConnState(sc.conn, state)
	}
}

type ConnState int

const (
	StateNew ConnState = iota

	StateActive

	StateIdle

	StateHijacked

	StateClosed
)

func http2h1ServerKeepAlivesDisabled(hs *Server) bool {
	var x interface{} = hs
	type I interface {
		doKeepAlives() bool
	}
	if hs, ok := x.(I); ok {
		return !hs.doKeepAlives()
	}
	return false
}

func (sc *http2serverConn) startGracefulShutdown() {
	sc.goAwayIn(http2ErrCodeNo, 0)
}

func (sc *http2serverConn) goAwayIn(code http2ErrCode, forceCloseIn time.Duration) {
	sc.serveG.check()
	if sc.inGoAway {
		return
	}
	if forceCloseIn != 0 {
		sc.shutDownIn(forceCloseIn)
	}
	sc.inGoAway = true
	sc.needToSendGoAway = true
	sc.goAwayCode = code
	sc.scheduleFrameWrite()
}

func (sc *http2serverConn) scheduleFrameWrite() {
	sc.serveG.check()
	if sc.writingFrame || sc.inFrameScheduleLoop {
		return
	}
	sc.inFrameScheduleLoop = true
	for !sc.writingFrameAsync {
		if sc.needToSendGoAway {
			sc.needToSendGoAway = false
			sc.startFrameWrite(http2FrameWriteRequest{
				write: &http2writeGoAway{
					maxStreamID: sc.maxClientStreamID,
					code:        sc.goAwayCode,
				},
			})
			continue
		}
		if sc.needToSendSettingsAck {
			sc.needToSendSettingsAck = false
			sc.startFrameWrite(http2FrameWriteRequest{write: http2writeSettingsAck{}})
			continue
		}
		if !sc.inGoAway || sc.goAwayCode == http2ErrCodeNo {
			if wr, ok := sc.writeSched.Pop(); ok {
				sc.startFrameWrite(wr)
				continue
			}
		}
		if sc.needsFrameFlush {
			sc.startFrameWrite(http2FrameWriteRequest{write: http2flushFrameWriter{}})
			sc.needsFrameFlush = false
			continue
		}
		break
	}
	sc.inFrameScheduleLoop = false
}

type http2flushFrameWriter struct{}

type http2writeSettingsAck struct{}

type http2writeGoAway struct {
	maxStreamID uint32
	code        http2ErrCode
}

func (p *http2writeGoAway) writeFrame(ctx http2writeContext) error {
	err := ctx.Framer().WriteGoAway(p.maxStreamID, p.code, nil)
	if p.code != 0 {
		ctx.Flush()
		time.Sleep(50 * time.Millisecond)
		ctx.CloseConn()
	}
	return err
}

func (f *http2Framer) startWrite(ftype http2FrameType, flags http2Flags, streamID uint32) {

	f.wbuf = append(f.wbuf[:0],
		0,
		0,
		0,
		byte(ftype),
		byte(flags),
		byte(streamID>>24),
		byte(streamID>>16),
		byte(streamID>>8),
		byte(streamID))
}

func (f *http2Framer) WriteGoAway(maxStreamID uint32, code http2ErrCode, debugData []byte) error {
	f.startWrite(http2FrameGoAway, 0, 0)
	f.writeUint32(maxStreamID & (1<<31 - 1))
	f.writeUint32(uint32(code))
	f.writeBytes(debugData)
	return f.endWrite()
}

var http2ErrFrameTooLarge = errors.New("http2: frame too large")

func (f *http2Framer) endWrite() error {

	length := len(f.wbuf) - http2frameHeaderLen
	if length >= (1 << 24) {
		return http2ErrFrameTooLarge
	}
	_ = append(f.wbuf[:0],
		byte(length>>16),
		byte(length>>8),
		byte(length))
	if f.logWrites {
		f.logWrite()
	}

	n, err := f.w.Write(f.wbuf)
	if err == nil && n != len(f.wbuf) {
		err = io.ErrShortWrite
	}
	return err
}

func (f *http2Framer) logWrite() {
	if f.debugFramer == nil {
		f.debugFramerBuf = new(bytes.Buffer)
		f.debugFramer = http2NewFramer(nil, f.debugFramerBuf)
		f.debugFramer.logReads = false

		f.debugFramer.AllowIllegalReads = true
	}
	f.debugFramerBuf.Write(f.wbuf)
	fr, err := f.debugFramer.ReadFrame()
	if err != nil {
		f.debugWriteLoggerf("http2: Framer %p: failed to decode just-written frame", f)
		return
	}
	f.debugWriteLoggerf("http2: Framer %p: wrote %v", f, http2summarizeFrame(fr))
}

type http2SettingsFrame struct {
	http2FrameHeader
	p []byte
}

type http2SettingID uint16

func (f *http2SettingsFrame) ForeachSetting(fn func(http2Setting) error) error {
	f.checkValid()
	buf := f.p
	for len(buf) > 0 {
		if err := fn(http2Setting{
			http2SettingID(binary.BigEndian.Uint16(buf[:2])),
			binary.BigEndian.Uint32(buf[2:6]),
		}); err != nil {
			return err
		}
		buf = buf[6:]
	}
	return nil
}

func (h *http2FrameHeader) checkValid() {
	if !h.valid {
		panic("Frame accessor called on non-owned Frame")
	}
}

type http2Setting struct {
	// ID is which setting is being set.
	// See http://http2.github.io/http2-spec/#SettingValues
	ID http2SettingID

	// Val is the value.
	Val uint32
}

func http2summarizeFrame(f http2Frame) string {
	var buf bytes.Buffer
	f.Header().writeDebug(&buf)
	switch f := f.(type) {
	case *http2SettingsFrame:
		n := 0
		f.ForeachSetting(func(s http2Setting) error {
			n++
			if n == 1 {
				buf.WriteString(", settings:")
			}
			fmt.Fprintf(&buf, " %v=%v,", s.ID, s.Val)
			return nil
		})
		if n > 0 {
			buf.Truncate(buf.Len() - 1)
		}
	case *http2DataFrame:
		data := f.Data()
		const max = 256
		if len(data) > max {
			data = data[:max]
		}
		fmt.Fprintf(&buf, " data=%q", data)
		if len(f.Data()) > max {
			fmt.Fprintf(&buf, " (%d bytes omitted)", len(f.Data())-max)
		}
	case *http2WindowUpdateFrame:
		if f.StreamID == 0 {
			buf.WriteString(" (conn)")
		}
		fmt.Fprintf(&buf, " incr=%v", f.Increment)
	case *http2PingFrame:
		fmt.Fprintf(&buf, " ping=%q", f.Data[:])
	case *http2GoAwayFrame:
		fmt.Fprintf(&buf, " LastStreamID=%v ErrCode=%v Debug=%q",
			f.LastStreamID, f.ErrCode, f.debugData)
	case *http2RSTStreamFrame:
		fmt.Fprintf(&buf, " ErrCode=%v", f.ErrCode)
	}
	return buf.String()
}

func (f *http2DataFrame) Data() []byte {
	f.checkValid()
	return f.data
}

type http2PingFrame struct {
	http2FrameHeader
	Data [8]byte
}

type http2DataFrame struct {
	http2FrameHeader
	data []byte
}

func (t http2FrameType) String() string {
	if s, ok := http2frameName[t]; ok {
		return s
	}
	return fmt.Sprintf("UNKNOWN_FRAME_TYPE_%d", uint8(t))
}

var http2frameName = map[http2FrameType]string{
	http2FrameData:         "DATA",
	http2FrameHeaders:      "HEADERS",
	http2FramePriority:     "PRIORITY",
	http2FrameRSTStream:    "RST_STREAM",
	http2FrameSettings:     "SETTINGS",
	http2FramePushPromise:  "PUSH_PROMISE",
	http2FramePing:         "PING",
	http2FrameGoAway:       "GOAWAY",
	http2FrameWindowUpdate: "WINDOW_UPDATE",
	http2FrameContinuation: "CONTINUATION",
}

func (h http2FrameHeader) writeDebug(buf *bytes.Buffer) {
	buf.WriteString(h.Type.String())
	if h.Flags != 0 {
		buf.WriteString(" flags=")
		set := 0
		for i := uint8(0); i < 8; i++ {
			if h.Flags&(1<<i) == 0 {
				continue
			}
			set++
			if set > 1 {
				buf.WriteByte('|')
			}
			name := http2flagName[h.Type][http2Flags(1<<i)]
			if name != "" {
				buf.WriteString(name)
			} else {
				fmt.Fprintf(buf, "0x%x", 1<<i)
			}
		}
	}
	if h.StreamID != 0 {
		fmt.Fprintf(buf, " stream=%d", h.StreamID)
	}
	fmt.Fprintf(buf, " len=%d", h.Length)
}

const (
	// Data Frame
	http2FlagDataEndStream http2Flags = 0x1
	http2FlagDataPadded    http2Flags = 0x8

	// Headers Frame
	http2FlagHeadersEndStream  http2Flags = 0x1
	http2FlagHeadersEndHeaders http2Flags = 0x4
	http2FlagHeadersPadded     http2Flags = 0x8
	http2FlagHeadersPriority   http2Flags = 0x20

	// Settings Frame
	http2FlagSettingsAck http2Flags = 0x1

	// Ping Frame
	http2FlagPingAck http2Flags = 0x1

	// Continuation Frame
	http2FlagContinuationEndHeaders http2Flags = 0x4

	http2FlagPushPromiseEndHeaders http2Flags = 0x4
	http2FlagPushPromisePadded     http2Flags = 0x8
)

var http2flagName = map[http2FrameType]map[http2Flags]string{
	http2FrameData: {
		http2FlagDataEndStream: "END_STREAM",
		http2FlagDataPadded:    "PADDED",
	},
	http2FrameHeaders: {
		http2FlagHeadersEndStream:  "END_STREAM",
		http2FlagHeadersEndHeaders: "END_HEADERS",
		http2FlagHeadersPadded:     "PADDED",
		http2FlagHeadersPriority:   "PRIORITY",
	},
	http2FrameSettings: {
		http2FlagSettingsAck: "ACK",
	},
	http2FramePing: {
		http2FlagPingAck: "ACK",
	},
	http2FrameContinuation: {
		http2FlagContinuationEndHeaders: "END_HEADERS",
	},
	http2FramePushPromise: {
		http2FlagPushPromiseEndHeaders: "END_HEADERS",
		http2FlagPushPromisePadded:     "PADDED",
	},
}

func http2readFrameHeader(buf []byte, r io.Reader) (http2FrameHeader, error) {
	_, err := io.ReadFull(r, buf[:http2frameHeaderLen])
	if err != nil {
		return http2FrameHeader{}, err
	}
	return http2FrameHeader{
		Length:   (uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])),
		Type:     http2FrameType(buf[3]),
		Flags:    http2Flags(buf[4]),
		StreamID: binary.BigEndian.Uint32(buf[5:]) & (1<<31 - 1),
		valid:    true,
	}, nil
}

func http2typeFrameParser(t http2FrameType) http2frameParser {
	if f := http2frameParsers[t]; f != nil {
		return f
	}
	return http2parseUnknownFrame
}

type http2frameParser func(fh http2FrameHeader, payload []byte) (http2Frame, error)

var http2frameParsers = map[http2FrameType]http2frameParser{
	http2FrameData:         http2parseDataFrame,
	http2FrameHeaders:      http2parseHeadersFrame,
	http2FramePriority:     http2parsePriorityFrame,
	http2FrameRSTStream:    http2parseRSTStreamFrame,
	http2FrameSettings:     http2parseSettingsFrame,
	http2FramePushPromise:  http2parsePushPromise,
	http2FramePing:         http2parsePingFrame,
	http2FrameGoAway:       http2parseGoAwayFrame,
	http2FrameWindowUpdate: http2parseWindowUpdateFrame,
	http2FrameContinuation: http2parseContinuationFrame,
}

type http2ContinuationFrame struct {
	http2FrameHeader
	headerFragBuf []byte
}

func http2parseContinuationFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.StreamID == 0 {
		return nil, http2connError{http2ErrCodeProtocol, "CONTINUATION frame with stream ID 0"}
	}
	return &http2ContinuationFrame{fh, p}, nil
}

type http2WindowUpdateFrame struct {
	http2FrameHeader
	Increment uint32 // never read with high bit set
}

func http2parseWindowUpdateFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if len(p) != 4 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	inc := binary.BigEndian.Uint32(p[:4]) & 0x7fffffff
	if inc == 0 {

		if fh.StreamID == 0 {
			return nil, http2ConnectionError(http2ErrCodeProtocol)
		}
		return nil, http2streamError(fh.StreamID, http2ErrCodeProtocol)
	}
	return &http2WindowUpdateFrame{
		http2FrameHeader: fh,
		Increment:        inc,
	}, nil
}

type http2GoAwayFrame struct {
	http2FrameHeader
	LastStreamID uint32
	ErrCode      http2ErrCode
	debugData    []byte
}

func http2parseGoAwayFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.StreamID != 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	if len(p) < 8 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	return &http2GoAwayFrame{
		http2FrameHeader: fh,
		LastStreamID:     binary.BigEndian.Uint32(p[:4]) & (1<<31 - 1),
		ErrCode:          http2ErrCode(binary.BigEndian.Uint32(p[4:8])),
		debugData:        p[8:],
	}, nil
}

func http2parsePingFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if len(payload) != 8 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID != 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	f := &http2PingFrame{http2FrameHeader: fh}
	copy(f.Data[:], payload)
	return f, nil
}

type http2PushPromiseFrame struct {
	http2FrameHeader
	PromiseID     uint32
	headerFragBuf []byte // not owned
}

func http2parsePushPromise(fh http2FrameHeader, p []byte) (_ http2Frame, err error) {
	pp := &http2PushPromiseFrame{
		http2FrameHeader: fh,
	}
	if pp.StreamID == 0 {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	// The PUSH_PROMISE frame includes optional padding.
	// Padding fields and flags are identical to those defined for DATA frames
	var padLength uint8
	if fh.Flags.Has(http2FlagPushPromisePadded) {
		if p, padLength, err = http2readByte(p); err != nil {
			return
		}
	}

	p, pp.PromiseID, err = http2readUint32(p)
	if err != nil {
		return
	}
	pp.PromiseID = pp.PromiseID & (1<<31 - 1)

	if int(padLength) > len(p) {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	pp.headerFragBuf = p[:len(p)-int(padLength)]
	return pp, nil
}

type http2UnknownFrame struct {
	http2FrameHeader
	p []byte
}

func http2parseUnknownFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	return &http2UnknownFrame{fh, p}, nil
}

func (f *http2SettingsFrame) Value(s http2SettingID) (v uint32, ok bool) {
	f.checkValid()
	buf := f.p
	for len(buf) > 0 {
		settingID := http2SettingID(binary.BigEndian.Uint16(buf[:2]))
		if settingID == s {
			return binary.BigEndian.Uint32(buf[2:6]), true
		}
		buf = buf[6:]
	}
	return 0, false
}

func http2parseSettingsFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.Flags.Has(http2FlagSettingsAck) && fh.Length > 0 {

		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID != 0 {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	if len(p)%6 != 0 {

		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	f := &http2SettingsFrame{http2FrameHeader: fh, p: p}
	if v, ok := f.Value(http2SettingInitialWindowSize); ok && v > (1<<31)-1 {

		return nil, http2ConnectionError(http2ErrCodeFlowControl)
	}
	return f, nil
}

type http2RSTStreamFrame struct {
	http2FrameHeader
	ErrCode http2ErrCode
}

func http2parseRSTStreamFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if len(p) != 4 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID == 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	return &http2RSTStreamFrame{fh, http2ErrCode(binary.BigEndian.Uint32(p[:4]))}, nil
}

func http2parsePriorityFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if fh.StreamID == 0 {
		return nil, http2connError{http2ErrCodeProtocol, "PRIORITY frame with stream ID 0"}
	}
	if len(payload) != 5 {
		return nil, http2connError{http2ErrCodeFrameSize, fmt.Sprintf("PRIORITY frame payload size was %d; want 5", len(payload))}
	}
	v := binary.BigEndian.Uint32(payload[:4])
	streamID := v & 0x7fffffff
	return &http2PriorityFrame{
		http2FrameHeader: fh,
		http2PriorityParam: http2PriorityParam{
			Weight:    payload[4],
			StreamDep: streamID,
			Exclusive: streamID != v,
		},
	}, nil
}

type http2PriorityFrame struct {
	http2FrameHeader
	http2PriorityParam
}

func http2readUint32(p []byte) (remain []byte, v uint32, err error) {
	if len(p) < 4 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	return p[4:], binary.BigEndian.Uint32(p[:4]), nil
}

func http2parseHeadersFrame(fh http2FrameHeader, p []byte) (_ http2Frame, err error) {
	hf := &http2HeadersFrame{
		http2FrameHeader: fh,
	}
	if fh.StreamID == 0 {

		return nil, http2connError{http2ErrCodeProtocol, "HEADERS frame with stream ID 0"}
	}
	var padLength uint8
	if fh.Flags.Has(http2FlagHeadersPadded) {
		if p, padLength, err = http2readByte(p); err != nil {
			return
		}
	}
	if fh.Flags.Has(http2FlagHeadersPriority) {
		var v uint32
		p, v, err = http2readUint32(p)
		if err != nil {
			return nil, err
		}
		hf.Priority.StreamDep = v & 0x7fffffff
		hf.Priority.Exclusive = (v != hf.Priority.StreamDep)
		p, hf.Priority.Weight, err = http2readByte(p)
		if err != nil {
			return nil, err
		}
	}
	if len(p)-int(padLength) <= 0 {
		return nil, http2streamError(fh.StreamID, http2ErrCodeProtocol)
	}
	hf.headerFragBuf = p[:len(p)-int(padLength)]
	return hf, nil
}

const (
	http2FrameData         http2FrameType = 0x0
	http2FrameHeaders      http2FrameType = 0x1
	http2FramePriority     http2FrameType = 0x2
	http2FrameRSTStream    http2FrameType = 0x3
	http2FrameSettings     http2FrameType = 0x4
	http2FramePushPromise  http2FrameType = 0x5
	http2FramePing         http2FrameType = 0x6
	http2FrameGoAway       http2FrameType = 0x7
	http2FrameWindowUpdate http2FrameType = 0x8
	http2FrameContinuation http2FrameType = 0x9
)

const (
	http2ErrCodeNo              http2ErrCode = 0x0
	http2ErrCodeProtocol        http2ErrCode = 0x1
	http2ErrCodeInternal        http2ErrCode = 0x2
	http2ErrCodeFlowControl     http2ErrCode = 0x3
	http2ErrCodeSettingsTimeout http2ErrCode = 0x4
	http2ErrCodeStreamClosed    http2ErrCode = 0x5
	http2ErrCodeFrameSize       http2ErrCode = 0x6
	http2ErrCodeRefusedStream   http2ErrCode = 0x7
	http2ErrCodeCancel          http2ErrCode = 0x8
	http2ErrCodeCompression     http2ErrCode = 0x9
	http2ErrCodeConnect         http2ErrCode = 0xa
	http2ErrCodeEnhanceYourCalm http2ErrCode = 0xb
	http2ErrCodeHTTP11Required  http2ErrCode = 0xd
)

func http2readByte(p []byte) (remain []byte, b byte, err error) {
	if len(p) == 0 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	return p[1:], p[0], nil
}

func (f http2Flags) Has(v http2Flags) bool {
	return (f & v) == v
}

func http2parseDataFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if fh.StreamID == 0 {
		return nil, http2connError{http2ErrCodeProtocol, "DATA frame with stream ID 0"}
	}
	f := &http2DataFrame{
		http2FrameHeader: fh,
	}
	var padSize byte
	if fh.Flags.Has(http2FlagDataPadded) {
		var err error
		payload, padSize, err = http2readByte(payload)
		if err != nil {
			return nil, err
		}
	}
	if int(padSize) > len(payload) {

		return nil, http2connError{http2ErrCodeProtocol, "pad size larger than data payload"}
	}
	f.data = payload[:len(payload)-int(padSize)]
	return f, nil
}

func (fr *http2Framer) ReadFrame() (http2Frame, error) {
	fr.errDetail = nil
	if fr.lastFrame != nil {
		fr.lastFrame.invalidate()
	}
	fh, err := http2readFrameHeader(fr.headerBuf[:], fr.r)
	if err != nil {
		return nil, err
	}
	if fh.Length > fr.maxReadSize {
		return nil, http2ErrFrameTooLarge
	}
	payload := fr.getReadBuf(fh.Length)
	if _, err := io.ReadFull(fr.r, payload); err != nil {
		return nil, err
	}
	f, err := http2typeFrameParser(fh.Type)(fh, payload)
	if err != nil {
		if ce, ok := err.(http2connError); ok {
			return nil, fr.connError(ce.Code, ce.Reason)
		}
		return nil, err
	}
	if err := fr.checkFrameOrder(f); err != nil {
		return nil, err
	}
	if fr.logReads {
		fr.debugReadLoggerf("http2: Framer %p: read %v", fr, http2summarizeFrame(f))
	}
	if fh.Type == http2FrameHeaders && fr.ReadMetaHeaders != nil {
		return fr.readMetaFrame(f.(*http2HeadersFrame))
	}
	return f, nil
}

type http2HeadersFrame struct {
	http2FrameHeader

	// Priority is set if FlagHeadersPriority is set in the FrameHeader.
	Priority http2PriorityParam

	headerFragBuf []byte // not owned
}

type http2MetaHeadersFrame struct {
	*http2HeadersFrame

	Fields []hpack.HeaderField

	Truncated bool
}

func (fr *http2Framer) maxHeaderListSize() uint32 {
	if fr.MaxHeaderListSize == 0 {
		return 16 << 20
	}
	return fr.MaxHeaderListSize
}

func (fr *http2Framer) maxHeaderStringLen() int {
	v := fr.maxHeaderListSize()
	if uint32(int(v)) == v {
		return int(v)
	}

	return 0
}

var http2VerboseLogs bool

type http2headerFieldValueError string

func (e http2headerFieldValueError) Error() string {
	return fmt.Sprintf("invalid header field value %q", string(e))
}

var http2errPseudoAfterRegular = errors.New("pseudo header field after regular")

func (fr *http2Framer) readMetaFrame(hf *http2HeadersFrame) (*http2MetaHeadersFrame, error) {
	if fr.AllowIllegalReads {
		return nil, errors.New("illegal use of AllowIllegalReads with ReadMetaHeaders")
	}
	mh := &http2MetaHeadersFrame{
		http2HeadersFrame: hf,
	}
	var remainSize = fr.maxHeaderListSize()
	var sawRegular bool

	var invalid error // pseudo header field errors
	hdec := fr.ReadMetaHeaders
	hdec.SetEmitEnabled(true)
	hdec.SetMaxStringLength(fr.maxHeaderStringLen())
	hdec.SetEmitFunc(func(hf hpack.HeaderField) {
		if http2VerboseLogs && fr.logReads {
			fr.debugReadLoggerf("http2: decoded hpack field %+v", hf)
		}
		if !httplex.ValidHeaderFieldValue(hf.Value) {
			invalid = http2headerFieldValueError(hf.Value)
		}
		isPseudo := strings.HasPrefix(hf.Name, ":")
		if isPseudo {
			if sawRegular {
				invalid = http2errPseudoAfterRegular
			}
		} else {
			sawRegular = true
			if !http2validWireHeaderFieldName(hf.Name) {
				invalid = http2headerFieldNameError(hf.Name)
			}
		}

		if invalid != nil {
			hdec.SetEmitEnabled(false)
			return
		}

		size := hf.Size()
		if size > remainSize {
			hdec.SetEmitEnabled(false)
			mh.Truncated = true
			return
		}
		remainSize -= size

		mh.Fields = append(mh.Fields, hf)
	})

	defer hdec.SetEmitFunc(func(hf hpack.HeaderField) {})

	var hc http2headersOrContinuation = hf
	for {
		frag := hc.HeaderBlockFragment()
		if _, err := hdec.Write(frag); err != nil {
			return nil, http2ConnectionError(http2ErrCodeCompression)
		}

		if hc.HeadersEnded() {
			break
		}
		if f, err := fr.ReadFrame(); err != nil {
			return nil, err
		} else {
			hc = f.(*http2ContinuationFrame)
		}
	}

	mh.http2HeadersFrame.headerFragBuf = nil
	mh.http2HeadersFrame.invalidate()

	if err := hdec.Close(); err != nil {
		return nil, http2ConnectionError(http2ErrCodeCompression)
	}
	if invalid != nil {
		fr.errDetail = invalid
		if http2VerboseLogs {
			log.Printf("http2: invalid header: %v", invalid)
		}
		return nil, http2StreamError{mh.StreamID, http2ErrCodeProtocol, invalid}
	}
	if err := mh.checkPseudos(); err != nil {
		fr.errDetail = err
		if http2VerboseLogs {
			log.Printf("http2: invalid pseudo headers: %v", err)
		}
		return nil, http2StreamError{mh.StreamID, http2ErrCodeProtocol, err}
	}
	return mh, nil
}

func (mh *http2MetaHeadersFrame) PseudoFields() []hpack.HeaderField {
	for i, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return mh.Fields[:i]
		}
	}
	return mh.Fields
}

type http2pseudoHeaderError string

func (e http2pseudoHeaderError) Error() string {
	return fmt.Sprintf("invalid pseudo-header %q", string(e))
}

type http2duplicatePseudoHeaderError string

func (e http2duplicatePseudoHeaderError) Error() string {
	return fmt.Sprintf("duplicate pseudo-header %q", string(e))
}

var http2errMixPseudoHeaderTypes = errors.New("mix of request and response pseudo headers")

func (mh *http2MetaHeadersFrame) checkPseudos() error {
	var isRequest, isResponse bool
	pf := mh.PseudoFields()
	for i, hf := range pf {
		switch hf.Name {
		case ":method", ":path", ":scheme", ":authority":
			isRequest = true
		case ":status":
			isResponse = true
		default:
			return http2pseudoHeaderError(hf.Name)
		}

		for _, hf2 := range pf[:i] {
			if hf.Name == hf2.Name {
				return http2duplicatePseudoHeaderError(hf.Name)
			}
		}
	}
	if isRequest && isResponse {
		return http2errMixPseudoHeaderTypes
	}
	return nil
}

type http2StreamError struct {
	StreamID uint32
	Code     http2ErrCode
	Cause    error // optional additional detail
}

func (h *http2FrameHeader) invalidate() { h.valid = false }

type http2ContinuationFrame struct {
	http2FrameHeader
	headerFragBuf []byte
}

type http2headersOrContinuation interface {
	http2headersEnder
	HeaderBlockFragment() []byte
}

type http2headersEnder interface {
	HeadersEnded() bool
}

type http2ConnectionError http2ErrCode

func (e http2ConnectionError) Error() string {
	return fmt.Sprintf("connection error: %s", http2ErrCode(e))
}

type http2headersOrContinuation interface {
	http2headersEnder
	HeaderBlockFragment() []byte
}

type http2headerFieldNameError string

func (e http2headerFieldNameError) Error() string {
	return fmt.Sprintf("invalid header field name %q", string(e))
}

func http2validWireHeaderFieldName(v string) bool {
	if len(v) == 0 {
		return false
	}
	for _, r := range v {
		if !httplex.IsTokenRune(r) {
			return false
		}
		if 'A' <= r && r <= 'Z' {
			return false
		}
	}
	return true
}

func (fr *http2Framer) checkFrameOrder(f http2Frame) error {
	last := fr.lastFrame
	fr.lastFrame = f
	if fr.AllowIllegalReads {
		return nil
	}

	fh := f.Header()
	if fr.lastHeaderStream != 0 {
		if fh.Type != http2FrameContinuation {
			return fr.connError(http2ErrCodeProtocol,
				fmt.Sprintf("got %s for stream %d; expected CONTINUATION following %s for stream %d",
					fh.Type, fh.StreamID,
					last.Header().Type, fr.lastHeaderStream))
		}
		if fh.StreamID != fr.lastHeaderStream {
			return fr.connError(http2ErrCodeProtocol,
				fmt.Sprintf("got CONTINUATION for stream %d; expected stream %d",
					fh.StreamID, fr.lastHeaderStream))
		}
	} else if fh.Type == http2FrameContinuation {
		return fr.connError(http2ErrCodeProtocol, fmt.Sprintf("unexpected CONTINUATION for stream %d", fh.StreamID))
	}

	switch fh.Type {
	case http2FrameHeaders, http2FrameContinuation:
		if fh.Flags.Has(http2FlagHeadersEndHeaders) {
			fr.lastHeaderStream = 0
		} else {
			fr.lastHeaderStream = fh.StreamID
		}
	}

	return nil
}

func (fr *http2Framer) connError(code http2ErrCode, reason string) error {
	fr.errDetail = errors.New(reason)
	return http2ConnectionError(code)
}

func (f *http2Framer) writeBytes(v []byte) { f.wbuf = append(f.wbuf, v...) }

func (f *http2Framer) writeUint32(v uint32) {
	f.wbuf = append(f.wbuf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

type http2connError struct {
	Code   http2ErrCode
	Reason string
}

const (
	http2stateIdle http2streamState = iota
	http2stateOpen
	http2stateHalfClosedLocal
	http2stateHalfClosedRemote
	http2stateClosed
)

type http2handlerPanicRST struct {
	StreamID uint32
}

type http2writeWindowUpdate struct {
	streamID uint32 // or 0 for conn-level
	n        uint32
}

func (sc *http2serverConn) startFrameWrite(wr http2FrameWriteRequest) {
	sc.serveG.check()
	if sc.writingFrame {
		panic("internal error: can only be writing one frame at a time")
	}

	st := wr.stream
	if st != nil {
		switch st.state {
		case http2stateHalfClosedLocal:
			switch wr.write.(type) {
			case http2StreamError, http2handlerPanicRST, http2writeWindowUpdate:

			default:
				panic(fmt.Sprintf("internal error: attempt to send frame on a half-closed-local stream: %v", wr))
			}
		case http2stateClosed:
			panic(fmt.Sprintf("internal error: attempt to send frame on a closed stream: %v", wr))
		}
	}
	if wpp, ok := wr.write.(*http2writePushPromise); ok {
		var err error
		wpp.promisedID, err = wpp.allocatePromisedID()
		if err != nil {
			sc.writingFrameAsync = false
			wr.replyToWriter(err)
			return
		}
	}

	sc.writingFrame = true
	sc.needsFrameFlush = true
	if wr.write.staysWithinBuffer(sc.bw.Available()) {
		sc.writingFrameAsync = false
		err := wr.write.writeFrame(sc)
		sc.wroteFrame(http2frameWriteResult{wr, err})
	} else {
		sc.writingFrameAsync = true
		go sc.writeFrameAsync(wr)
	}
}

func (sc *http2serverConn) writeFrameAsync(wr http2FrameWriteRequest) {
	err := wr.write.writeFrame(sc)
	sc.wroteFrameCh <- http2frameWriteResult{wr, err}
}

func http2writeEndsStream(w http2writeFramer) bool {
	switch v := w.(type) {
	case *http2writeData:
		return v.endStream
	case *http2writeResHeaders:
		return v.endStream
	case nil:

		panic("writeEndsStream called on nil writeFramer")
	}
	return false
}

type http2writeResHeaders struct {
	streamID    uint32
	httpResCode int      // 0 means no ":status" line
	h           Header   // may be nil
	trailers    []string // if non-nil, which keys of h to write. nil means all.
	endStream   bool

	date          string
	contentType   string
	contentLength string
}

type http2writeData struct {
	streamID  uint32
	p         []byte
	endStream bool
}

func (sc *http2serverConn) wroteFrame(res http2frameWriteResult) {
	sc.serveG.check()
	if !sc.writingFrame {
		panic("internal error: expected to be already writing a frame")
	}
	sc.writingFrame = false
	sc.writingFrameAsync = false

	wr := res.wr

	if http2writeEndsStream(wr.write) {
		st := wr.stream
		if st == nil {
			panic("internal error: expecting non-nil stream")
		}
		switch st.state {
		case http2stateOpen:

			st.state = http2stateHalfClosedLocal
			sc.resetStream(http2streamError(st.id, http2ErrCodeCancel))
		case http2stateHalfClosedRemote:
			sc.closeStream(st, http2errHandlerComplete)
		}
	} else {
		switch v := wr.write.(type) {
		case http2StreamError:

			if st, ok := sc.streams[v.StreamID]; ok {
				sc.closeStream(st, v)
			}
		case http2handlerPanicRST:
			sc.closeStream(wr.stream, http2errHandlerPanicked)
		}
	}

	wr.replyToWriter(res.err)

	sc.scheduleFrameWrite()
}

var http2errHandlerComplete = errors.New("http2: request body closed due to handler exiting")
var http2errHandlerPanicked = errors.New("http2: handler panicked")

func (sc *http2serverConn) resetStream(se http2StreamError) {
	sc.serveG.check()
	sc.writeFrame(http2FrameWriteRequest{write: se})
	if st, ok := sc.streams[se.StreamID]; ok {
		st.resetQueued = true
	}
}

func (wr http2FrameWriteRequest) StreamID() uint32 {
	if wr.stream == nil {
		if se, ok := wr.write.(http2StreamError); ok {

			return se.StreamID
		}
		return 0
	}
	return wr.stream.id
}

func (wr http2FrameWriteRequest) StreamID() uint32 {
	if wr.stream == nil {
		if se, ok := wr.write.(http2StreamError); ok {

			return se.StreamID
		}
		return 0
	}
	return wr.stream.id
}

func (sc *http2serverConn) state(streamID uint32) (http2streamState, *http2stream) {
	sc.serveG.check()

	if st, ok := sc.streams[streamID]; ok {
		return st.state, st
	}

	if streamID%2 == 1 {
		if streamID <= sc.maxClientStreamID {
			return http2stateClosed, nil
		}
	} else {
		if streamID <= sc.maxPushPromiseID {
			return http2stateClosed, nil
		}
	}
	return http2stateIdle, nil
}

type http2write100ContinueHeadersFrame struct {
	streamID uint32
}

func (sc *http2serverConn) writeFrame(wr http2FrameWriteRequest) {
	sc.serveG.check()

	// If true, wr will not be written and wr.done will not be signaled.
	var ignoreWrite bool

	if wr.StreamID() != 0 {
		_, isReset := wr.write.(http2StreamError)
		if state, _ := sc.state(wr.StreamID()); state == http2stateClosed && !isReset {
			ignoreWrite = true
		}
	}

	switch wr.write.(type) {
	case *http2writeResHeaders:
		wr.stream.wroteHeaders = true
	case http2write100ContinueHeadersFrame:
		if wr.stream.wroteHeaders {

			if wr.done != nil {
				panic("wr.done != nil for write100ContinueHeadersFrame")
			}
			ignoreWrite = true
		}
	}

	if !ignoreWrite {
		sc.writeSched.Push(wr)
	}
	sc.scheduleFrameWrite()
}

func http2streamError(id uint32, code http2ErrCode) http2StreamError {
	return http2StreamError{StreamID: id, Code: code}
}

const http2bufWriterPoolBufferSize = 4 << 10

func (w *http2bufferedWriter) Available() int {
	if w.bw == nil {
		return http2bufWriterPoolBufferSize
	}
	return w.bw.Available()
}

func (wr *http2FrameWriteRequest) replyToWriter(err error) {
	if wr.done == nil {
		return
	}
	select {
	case wr.done <- err:
	default:
		panic(fmt.Sprintf("unbuffered done channel passed in for type %T", wr.write))
	}
	wr.write = nil
}

type http2writePushPromise struct {
	streamID uint32   // pusher stream
	method   string   // for :method
	url      *url.URL // for :scheme, :authority, :path
	h        Header

	allocatePromisedID func() (uint32, error)
	promisedID         uint32
}

func (sc *http2serverConn) shutDownIn(d time.Duration) {
	sc.serveG.check()
	sc.shutdownTimer = time.NewTimer(d)
	sc.shutdownTimerCh = sc.shutdownTimer.C
}

func (sc *http2serverConn) closeStream(st *http2stream, err error) {
	sc.serveG.check()
	if st.state == http2stateIdle || st.state == http2stateClosed {
		panic(fmt.Sprintf("invariant; can't close stream in state %v", st.state))
	}
	st.state = http2stateClosed
	if st.isPushed() {
		sc.curPushedStreams--
	} else {
		sc.curClientStreams--
	}
	delete(sc.streams, st.id)
	if len(sc.streams) == 0 {
		sc.setConnState(StateIdle)
		if sc.srv.IdleTimeout != 0 {
			sc.idleTimer.Reset(sc.srv.IdleTimeout)
		}
		if http2h1ServerKeepAlivesDisabled(sc.hs) {
			sc.startGracefulShutdown()
		}
	}
	if p := st.body; p != nil {

		sc.sendWindowUpdate(nil, p.Len())

		p.CloseWithError(err)
	}
	st.cw.Close()
	sc.writeSched.CloseStream(st.id)
}

func (p *http2pipe) CloseWithError(err error) { p.closeWithError(&p.err, err, nil) }

func (cw http2closeWaiter) Close() {
	close(cw)
}

func (p *http2pipe) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b.Len()
}

func (p *http2pipe) closeWithError(dst *error, err error, fn func()) {
	if err == nil {
		panic("err must be non-nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c.L == nil {
		p.c.L = &p.mu
	}
	defer p.c.Signal()
	if *dst != nil {

		return
	}
	p.readFn = fn
	*dst = err
	p.closeDoneLocked()
}

func (p *http2pipe) closeDoneLocked() {
	if p.donec == nil {
		return
	}

	select {
	case <-p.donec:
	default:
		close(p.donec)
	}
}

// st may be nil for conn-level
func (sc *http2serverConn) sendWindowUpdate(st *http2stream, n int) {
	sc.serveG.check()
	// "The legal range for the increment to the flow control
	// window is 1 to 2^31-1 (2,147,483,647) octets."
	// A Go Read call on 64-bit machines could in theory read
	// a larger Read than this. Very unlikely, but we handle it here
	// rather than elsewhere for now.
	const maxUint31 = 1<<31 - 1
	for n >= maxUint31 {
		sc.sendWindowUpdate32(st, maxUint31)
		n -= maxUint31
	}
	sc.sendWindowUpdate32(st, int32(n))
}

func (sc *http2serverConn) sendWindowUpdate32(st *http2stream, n int32) {
	sc.serveG.check()
	if n == 0 {
		return
	}
	if n < 0 {
		panic("negative update")
	}
	var streamID uint32
	if st != nil {
		streamID = st.id
	}
	sc.writeFrame(http2FrameWriteRequest{
		write:  http2writeWindowUpdate{streamID: streamID, n: uint32(n)},
		stream: st,
	})
	var ok bool
	if st == nil {
		ok = sc.inflow.add(n)
	} else {
		ok = st.inflow.add(n)
	}
	if !ok {
		panic("internal error; sent too many window updates without decrements?")
	}
}

var http2errClientDisconnected = errors.New("client disconnected")

func (sc *http2serverConn) stopShutdownTimer() {
	sc.serveG.check()
	if t := sc.shutdownTimer; t != nil {
		t.Stop()
	}
}

func (sc *http2serverConn) vlogf(format string, args ...interface{}) {
	if http2VerboseLogs {
		sc.logf(format, args...)
	}
}

func (sc *http2serverConn) logf(format string, args ...interface{}) {
	if lg := sc.hs.ErrorLog; lg != nil {
		lg.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

type http2writeSettings []http2Setting

const (
	http2SettingHeaderTableSize      http2SettingID = 0x1
	http2SettingEnablePush           http2SettingID = 0x2
	http2SettingMaxConcurrentStreams http2SettingID = 0x3
	http2SettingInitialWindowSize    http2SettingID = 0x4
	http2SettingMaxFrameSize         http2SettingID = 0x5
	http2SettingMaxHeaderListSize    http2SettingID = 0x6
)

func (sc *http2serverConn) serve() {
	sc.serveG.check()
	defer sc.notePanic()
	defer sc.conn.Close()
	defer sc.closeAllStreamsOnConnClose()
	defer sc.stopShutdownTimer()
	defer close(sc.doneServing)

	if http2VerboseLogs {
		sc.vlogf("http2: server connection from %v on %p", sc.conn.RemoteAddr(), sc.hs)
	}

	sc.writeFrame(http2FrameWriteRequest{
		write: http2writeSettings{
			{http2SettingMaxFrameSize, sc.srv.maxReadFrameSize()},
			{http2SettingMaxConcurrentStreams, sc.advMaxStreams},
			{http2SettingMaxHeaderListSize, sc.maxHeaderListSize()},
		},
	})
	sc.unackedSettings++

	if err := sc.readPreface(); err != nil {
		sc.condlogf(err, "http2: server: error reading preface from client %v: %v", sc.conn.RemoteAddr(), err)
		return
	}

	sc.setConnState(StateActive)
	sc.setConnState(StateIdle)

	if sc.srv.IdleTimeout != 0 {
		sc.idleTimer = time.NewTimer(sc.srv.IdleTimeout)
		defer sc.idleTimer.Stop()
		sc.idleTimerCh = sc.idleTimer.C
	}

	var gracefulShutdownCh chan struct{}
	if sc.hs != nil {
		ch := http2h1ServerShutdownChan(sc.hs)
		if ch != nil {
			gracefulShutdownCh = make(chan struct{})
			go sc.awaitGracefulShutdown(ch, gracefulShutdownCh)
		}
	}

	go sc.readFrames()

	settingsTimer := time.NewTimer(http2firstSettingsTimeout)
	loopNum := 0
	for {
		loopNum++
		select {
		case wr := <-sc.wantWriteFrameCh:
			sc.writeFrame(wr)
		case spr := <-sc.wantStartPushCh:
			sc.startPush(spr)
		case res := <-sc.wroteFrameCh:
			sc.wroteFrame(res)
		case res := <-sc.readFrameCh:
			if !sc.processFrameFromReader(res) {
				return
			}
			res.readMore()
			if settingsTimer.C != nil {
				settingsTimer.Stop()
				settingsTimer.C = nil
			}
		case m := <-sc.bodyReadCh:
			sc.noteBodyRead(m.st, m.n)
		case <-settingsTimer.C:
			sc.logf("timeout waiting for SETTINGS frames from %v", sc.conn.RemoteAddr())
			return
		case <-gracefulShutdownCh:
			gracefulShutdownCh = nil
			sc.startGracefulShutdown()
		case <-sc.shutdownTimerCh:
			sc.vlogf("GOAWAY close timer fired; closing conn from %v", sc.conn.RemoteAddr())
			return
		case <-sc.idleTimerCh:
			sc.vlogf("connection is idle")
			sc.goAway(http2ErrCodeNo)
		case fn := <-sc.testHookCh:
			fn(loopNum)
		}

		if sc.inGoAway && sc.curOpenStreams() == 0 && !sc.needToSendGoAway && !sc.writingFrame {
			return
		}
	}
}

func (sc *http2serverConn) curOpenStreams() uint32 {
	sc.serveG.check()
	return sc.curClientStreams + sc.curPushedStreams
}

func (sc *http2serverConn) goAway(code http2ErrCode) {
	sc.serveG.check()
	var forceCloseIn time.Duration
	if code != http2ErrCodeNo {
		forceCloseIn = 250 * time.Millisecond
	} else {

		forceCloseIn = 1 * time.Second
	}
	sc.goAwayIn(code, forceCloseIn)
}

func (sc *http2serverConn) noteBodyRead(st *http2stream, n int) {
	sc.serveG.check()
	sc.sendWindowUpdate(nil, n)
	if st.state != http2stateHalfClosedRemote && st.state != http2stateClosed {

		sc.sendWindowUpdate(st, n)
	}
}

type http2goAwayFlowError struct{}

func (http2goAwayFlowError) Error() string { return "connection exceeded flow control window size" }

func (sc *http2serverConn) processFrameFromReader(res http2readFrameResult) bool {
	sc.serveG.check()
	err := res.err
	if err != nil {
		if err == http2ErrFrameTooLarge {
			sc.goAway(http2ErrCodeFrameSize)
			return true
		}
		clientGone := err == io.EOF || err == io.ErrUnexpectedEOF || http2isClosedConnError(err)
		if clientGone {

			return false
		}
	} else {
		f := res.f
		if http2VerboseLogs {
			sc.vlogf("http2: server read frame %v", http2summarizeFrame(f))
		}
		err = sc.processFrame(f)
		if err == nil {
			return true
		}
	}

	switch ev := err.(type) {
	case http2StreamError:
		sc.resetStream(ev)
		return true
	case http2goAwayFlowError:
		sc.goAway(http2ErrCodeFlowControl)
		return true
	case http2ConnectionError:
		sc.logf("http2: server connection error from %v: %v", sc.conn.RemoteAddr(), ev)
		sc.goAway(http2ErrCode(ev))
		return true
	default:
		if res.err != nil {
			sc.vlogf("http2: server closing client connection; error reading frame from client %s: %v", sc.conn.RemoteAddr(), err)
		} else {
			sc.logf("http2: server closing client connection: %v", err)
		}
		return false
	}
}

func (sc *http2serverConn) processFrame(f http2Frame) error {
	sc.serveG.check()

	if !sc.sawFirstSettings {
		if _, ok := f.(*http2SettingsFrame); !ok {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
		sc.sawFirstSettings = true
	}

	switch f := f.(type) {
	case *http2SettingsFrame:
		return sc.processSettings(f)
	case *http2MetaHeadersFrame:
		return sc.processHeaders(f)
	case *http2WindowUpdateFrame:
		return sc.processWindowUpdate(f)
	case *http2PingFrame:
		return sc.processPing(f)
	case *http2DataFrame:
		return sc.processData(f)
	case *http2RSTStreamFrame:
		return sc.processResetStream(f)
	case *http2PriorityFrame:
		return sc.processPriority(f)
	case *http2GoAwayFrame:
		return sc.processGoAway(f)
	case *http2PushPromiseFrame:

		return http2ConnectionError(http2ErrCodeProtocol)
	default:
		sc.vlogf("http2: server ignoring frame: %v", f.Header())
		return nil
	}
}

func (sc *http2serverConn) processGoAway(f *http2GoAwayFrame) error {
	sc.serveG.check()
	if f.ErrCode != http2ErrCodeNo {
		sc.logf("http2: received GOAWAY %+v, starting graceful shutdown", f)
	} else {
		sc.vlogf("http2: received GOAWAY %+v, starting graceful shutdown", f)
	}
	sc.startGracefulShutdown()

	sc.pushEnabled = false
	return nil
}

func http2checkPriority(streamID uint32, p http2PriorityParam) error {
	if streamID == p.StreamDep {

		return http2streamError(streamID, http2ErrCodeProtocol)
	}
	return nil
}

func (sc *http2serverConn) processPriority(f *http2PriorityFrame) error {
	if sc.inGoAway {
		return nil
	}
	if err := http2checkPriority(f.StreamID, f.http2PriorityParam); err != nil {
		return err
	}
	sc.writeSched.AdjustStream(f.StreamID, f.http2PriorityParam)
	return nil
}

func (h http2FrameHeader) Header() http2FrameHeader { return h }

func (f *http2flow) available() int32 {
	n := f.n
	if f.conn != nil && f.conn.n < n {
		n = f.conn.n
	}
	return n
}

func (sc *http2serverConn) processData(f *http2DataFrame) error {
	sc.serveG.check()
	if sc.inGoAway && sc.goAwayCode != http2ErrCodeNo {
		return nil
	}
	data := f.Data()

	id := f.Header().StreamID
	state, st := sc.state(id)
	if id == 0 || state == http2stateIdle {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if st == nil || state != http2stateOpen || st.gotTrailerHeader || st.resetQueued {

		if sc.inflow.available() < int32(f.Length) {
			return http2streamError(id, http2ErrCodeFlowControl)
		}

		sc.inflow.take(int32(f.Length))
		sc.sendWindowUpdate(nil, int(f.Length))

		if st != nil && st.resetQueued {

			return nil
		}
		return http2streamError(id, http2ErrCodeStreamClosed)
	}
	if st.body == nil {
		panic("internal error: should have a body in this state")
	}

	if st.declBodyBytes != -1 && st.bodyBytes+int64(len(data)) > st.declBodyBytes {
		st.body.CloseWithError(fmt.Errorf("sender tried to send more than declared Content-Length of %d bytes", st.declBodyBytes))
		return http2streamError(id, http2ErrCodeStreamClosed)
	}
	if f.Length > 0 {

		if st.inflow.available() < int32(f.Length) {
			return http2streamError(id, http2ErrCodeFlowControl)
		}
		st.inflow.take(int32(f.Length))

		if len(data) > 0 {
			wrote, err := st.body.Write(data)
			if err != nil {
				return http2streamError(id, http2ErrCodeStreamClosed)
			}
			if wrote != len(data) {
				panic("internal error: bad Writer")
			}
			st.bodyBytes += int64(len(data))
		}

		if pad := int32(f.Length) - int32(len(data)); pad > 0 {
			sc.sendWindowUpdate32(nil, pad)
			sc.sendWindowUpdate32(st, pad)
		}
	}
	if f.StreamEnded() {
		st.endStream()
	}
	return nil
}

func (st *http2stream) endStream() {
	sc := st.sc
	sc.serveG.check()

	if st.declBodyBytes != -1 && st.declBodyBytes != st.bodyBytes {
		st.body.CloseWithError(fmt.Errorf("request declared a Content-Length of %d but only wrote %d bytes",
			st.declBodyBytes, st.bodyBytes))
	} else {
		st.body.closeWithErrorAndCode(io.EOF, st.copyTrailersToHandlerRequest)
		st.body.CloseWithError(io.EOF)
	}
	st.state = http2stateHalfClosedRemote
}

func (st *http2stream) copyTrailersToHandlerRequest() {
	for k, vv := range st.trailer {
		if _, ok := st.reqTrailer[k]; ok {

			st.reqTrailer[k] = vv
		}
	}
}

func (p *http2pipe) closeWithErrorAndCode(err error, fn func()) { p.closeWithError(&p.err, err, fn) }

func (f *http2DataFrame) StreamEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagDataEndStream)
}

func (p *http2pipe) Write(d []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c.L == nil {
		p.c.L = &p.mu
	}
	defer p.c.Signal()
	if p.err != nil {
		return 0, http2errClosedPipeWrite
	}
	return p.b.Write(d)
}

var http2errClosedPipeWrite = errors.New("write on closed buffer")

func (f *http2flow) take(n int32) {
	if n > f.available() {
		panic("internal error: took too much")
	}
	f.n -= n
	if f.conn != nil {
		f.conn.n -= n
	}
}

func (sc *http2serverConn) processPing(f *http2PingFrame) error {
	sc.serveG.check()
	if f.IsAck() {

		return nil
	}
	if f.StreamID != 0 {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if sc.inGoAway && sc.goAwayCode != http2ErrCodeNo {
		return nil
	}
	sc.writeFrame(http2FrameWriteRequest{write: http2writePingAck{f}})
	return nil
}

type http2writePingAck struct{ pf *http2PingFrame }

func (f *http2PingFrame) IsAck() bool { return f.Flags.Has(http2FlagPingAck) }

func (sc *http2serverConn) processHeaders(f *http2MetaHeadersFrame) error {
	sc.serveG.check()
	id := f.StreamID
	if sc.inGoAway {

		return nil
	}

	if id%2 != 1 {
		return http2ConnectionError(http2ErrCodeProtocol)
	}

	if st := sc.streams[f.StreamID]; st != nil {
		if st.resetQueued {

			return nil
		}
		return st.processTrailerHeaders(f)
	}

	if id <= sc.maxClientStreamID {
		return http2ConnectionError(http2ErrCodeProtocol)
	}
	sc.maxClientStreamID = id

	if sc.idleTimer != nil {
		sc.idleTimer.Stop()
	}

	if sc.curClientStreams+1 > sc.advMaxStreams {
		if sc.unackedSettings == 0 {

			return http2streamError(id, http2ErrCodeProtocol)
		}

		return http2streamError(id, http2ErrCodeRefusedStream)
	}

	initialState := http2stateOpen
	if f.StreamEnded() {
		initialState = http2stateHalfClosedRemote
	}
	st := sc.newStream(id, 0, initialState)

	if f.HasPriority() {
		if err := http2checkPriority(f.StreamID, f.Priority); err != nil {
			return err
		}
		sc.writeSched.AdjustStream(st.id, f.Priority)
	}

	rw, req, err := sc.newWriterAndRequest(st, f)
	if err != nil {
		return err
	}
	st.reqTrailer = req.Trailer
	if st.reqTrailer != nil {
		st.trailer = make(Header)
	}
	st.body = req.Body.(*http2requestBody).pipe
	st.declBodyBytes = req.ContentLength

	handler := sc.handler.ServeHTTP
	if f.Truncated {

		handler = http2handleHeaderListTooLong
	} else if err := http2checkValidHTTP2RequestHeaders(req.Header); err != nil {
		handler = http2new400Handler(err)
	}

	if sc.hs.ReadTimeout != 0 {
		sc.conn.SetReadDeadline(time.Time{})
	}

	go sc.runHandler(rw, req, handler)
	return nil
}

func (sc *http2serverConn) runHandler(rw *http2responseWriter, req *Request, handler func(ResponseWriter, *Request)) {
	didPanic := true
	defer func() {
		rw.rws.stream.cancelCtx()
		if didPanic {
			e := recover()
			sc.writeFrameFromHandler(http2FrameWriteRequest{
				write:  http2handlerPanicRST{rw.rws.stream.id},
				stream: rw.rws.stream,
			})

			if http2shouldLogPanic(e) {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				sc.logf("http2: panic serving %v: %v\n%s", sc.conn.RemoteAddr(), e, buf)
			}
			return
		}
		rw.handlerDone()
	}()
	handler(rw, req)
	didPanic = false
}

func (w *http2responseWriter) handlerDone() {
	rws := w.rws
	rws.handlerDone = true
	w.Flush()
	w.rws = nil
	http2responseWriterStatePool.Put(rws)
}

func (w *http2responseWriter) Flush() {
	rws := w.rws
	if rws == nil {
		panic("Header called after Handler finished")
	}
	if rws.bw.Buffered() > 0 {
		if err := rws.bw.Flush(); err != nil {
			return
		}
	} else {
		rws.writeChunk(nil)
	}
}

func (rws *http2responseWriterState) writeHeader(code int) {
	if !rws.wroteHeader {
		rws.wroteHeader = true
		rws.status = code
		if len(rws.handlerHeader) > 0 {
			rws.snapHeader = http2cloneHeader(rws.handlerHeader)
		}
	}
}

func (rws *http2responseWriterState) writeChunk(p []byte) (n int, err error) {
	if !rws.wroteHeader {
		rws.writeHeader(200)
	}

	isHeadResp := rws.req.Method == "HEAD"
	if !rws.sentHeader {
		rws.sentHeader = true
		var ctype, clen string
		if clen = rws.snapHeader.Get("Content-Length"); clen != "" {
			rws.snapHeader.Del("Content-Length")
			clen64, err := strconv.ParseInt(clen, 10, 64)
			if err == nil && clen64 >= 0 {
				rws.sentContentLen = clen64
			} else {
				clen = ""
			}
		}
		if clen == "" && rws.handlerDone && http2bodyAllowedForStatus(rws.status) && (len(p) > 0 || !isHeadResp) {
			clen = strconv.Itoa(len(p))
		}
		_, hasContentType := rws.snapHeader["Content-Type"]
		if !hasContentType && http2bodyAllowedForStatus(rws.status) {
			ctype = DetectContentType(p)
		}
		var date string
		if _, ok := rws.snapHeader["Date"]; !ok {

			date = time.Now().UTC().Format(TimeFormat)
		}

		for _, v := range rws.snapHeader["Trailer"] {
			http2foreachHeaderElement(v, rws.declareTrailer)
		}

		endStream := (rws.handlerDone && !rws.hasTrailers() && len(p) == 0) || isHeadResp
		err = rws.conn.writeHeaders(rws.stream, &http2writeResHeaders{
			streamID:      rws.stream.id,
			httpResCode:   rws.status,
			h:             rws.snapHeader,
			endStream:     endStream,
			contentType:   ctype,
			contentLength: clen,
			date:          date,
		})
		if err != nil {
			return 0, err
		}
		if endStream {
			return 0, nil
		}
	}
	if isHeadResp {
		return len(p), nil
	}
	if len(p) == 0 && !rws.handlerDone {
		return 0, nil
	}

	if rws.handlerDone {
		rws.promoteUndeclaredTrailers()
	}

	endStream := rws.handlerDone && !rws.hasTrailers()
	if len(p) > 0 || endStream {

		if err := rws.conn.writeDataFromHandler(rws.stream, p, endStream); err != nil {
			return 0, err
		}
	}

	if rws.handlerDone && rws.hasTrailers() {
		err = rws.conn.writeHeaders(rws.stream, &http2writeResHeaders{
			streamID:  rws.stream.id,
			h:         rws.handlerHeader,
			trailers:  rws.trailers,
			endStream: true,
		})
		return len(p), err
	}
	return len(p), nil
}

func (rws *http2responseWriterState) declareTrailer(k string) {
	k = CanonicalHeaderKey(k)
	if !http2ValidTrailerHeader(k) {

		rws.conn.logf("ignoring invalid trailer %q", k)
		return
	}
	if !http2strSliceContains(rws.trailers, k) {
		rws.trailers = append(rws.trailers, k)
	}
}

var http2errChanPool = sync.Pool{
	New: func() interface{} { return make(chan error, 1) },
}

var http2errStreamClosed = errors.New("http2: stream closed")

func (sc *http2serverConn) writeHeaders(st *http2stream, headerData *http2writeResHeaders) error {
	sc.serveG.checkNotOn()
	var errc chan error
	if headerData.h != nil {

		errc = http2errChanPool.Get().(chan error)
	}
	if err := sc.writeFrameFromHandler(http2FrameWriteRequest{
		write:  headerData,
		stream: st,
		done:   errc,
	}); err != nil {
		return err
	}
	if errc != nil {
		select {
		case err := <-errc:
			http2errChanPool.Put(errc)
			return err
		case <-sc.doneServing:
			return http2errClientDisconnected
		case <-st.cw:
			return http2errStreamClosed
		}
	}
	return nil
}

var http2writeDataPool = sync.Pool{
	New: func() interface{} { return new(http2writeData) },
}

func (sc *http2serverConn) writeDataFromHandler(stream *http2stream, data []byte, endStream bool) error {
	ch := http2errChanPool.Get().(chan error)
	writeArg := http2writeDataPool.Get().(*http2writeData)
	*writeArg = http2writeData{stream.id, data, endStream}
	err := sc.writeFrameFromHandler(http2FrameWriteRequest{
		write:  writeArg,
		stream: stream,
		done:   ch,
	})
	if err != nil {
		return err
	}
	var frameWriteDone bool // the frame write is done (successfully or not)
	select {
	case err = <-ch:
		frameWriteDone = true
	case <-sc.doneServing:
		return http2errClientDisconnected
	case <-stream.cw:

		select {
		case err = <-ch:
			frameWriteDone = true
		default:
			return http2errStreamClosed
		}
	}
	http2errChanPool.Put(ch)
	if frameWriteDone {
		http2writeDataPool.Put(writeArg)
	}
	return err
}

func http2ValidTrailerHeader(name string) bool {
	name = CanonicalHeaderKey(name)
	if strings.HasPrefix(name, "If-") || http2badTrailer[name] {
		return false
	}
	return true
}

var http2badTrailer = map[string]bool{
	"Authorization":       true,
	"Cache-Control":       true,
	"Connection":          true,
	"Content-Encoding":    true,
	"Content-Length":      true,
	"Content-Range":       true,
	"Content-Type":        true,
	"Expect":              true,
	"Host":                true,
	"Keep-Alive":          true,
	"Max-Forwards":        true,
	"Pragma":              true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Range":               true,
	"Realm":               true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Www-Authenticate":    true,
}

func http2strSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func (rws *http2responseWriterState) hasTrailers() bool { return len(rws.trailers) != 0 }

const http2TrailerPrefix = "Trailer:"

func CanonicalHeaderKey(s string) string { return textproto.CanonicalMIMEHeaderKey(s) }

func (rws *http2responseWriterState) declareTrailer(k string) {
	k = CanonicalHeaderKey(k)
	if !http2ValidTrailerHeader(k) {

		rws.conn.logf("ignoring invalid trailer %q", k)
		return
	}
	if !http2strSliceContains(rws.trailers, k) {
		rws.trailers = append(rws.trailers, k)
	}
}

var http2sorterPool = sync.Pool{New: func() interface{} { return new(http2sorter) }}

func (rws *http2responseWriterState) promoteUndeclaredTrailers() {
	for k, vv := range rws.handlerHeader {
		if !strings.HasPrefix(k, http2TrailerPrefix) {
			continue
		}
		trailerKey := strings.TrimPrefix(k, http2TrailerPrefix)
		rws.declareTrailer(trailerKey)
		rws.handlerHeader[CanonicalHeaderKey(trailerKey)] = vv
	}

	if len(rws.trailers) > 1 {
		sorter := http2sorterPool.Get().(*http2sorter)
		sorter.SortStrings(rws.trailers)
		http2sorterPool.Put(sorter)
	}
}

type http2sorter struct {
	v []string // owned by sorter
}

func (s *http2sorter) SortStrings(ss []string) {

	save := s.v
	s.v = ss
	sort.Sort(s)
	s.v = save
}

func http2foreachHeaderElement(v string, fn func(string)) {
	v = textproto.TrimString(v)
	if v == "" {
		return
	}
	if !strings.Contains(v, ",") {
		fn(v)
		return
	}
	for _, f := range strings.Split(v, ",") {
		if f = textproto.TrimString(f); f != "" {
			fn(f)
		}
	}
}

func http2bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == 204:
		return false
	case status == 304:
		return false
	}
	return true
}

// Del deletes the values associated with key.
func (h Header) Del(key string) {
	textproto.MIMEHeader(h).Del(key)
}

var http2responseWriterStatePool = sync.Pool{
	New: func() interface{} {
		rws := &http2responseWriterState{}
		rws.bw = bufio.NewWriterSize(http2chunkWriter{rws}, http2handlerChunkWriteSize)
		return rws
	},
}
var ErrAbortHandler = errors.New("net/http: abort Handler")

type http2chunkWriter struct{ rws *http2responseWriterState }

func http2shouldLogPanic(panicValue interface{}) bool {
	return panicValue != nil && panicValue != ErrAbortHandler
}

func (sc *http2serverConn) writeFrameFromHandler(wr http2FrameWriteRequest) error {
	sc.serveG.checkNotOn()
	select {
	case sc.wantWriteFrameCh <- wr:
		return nil
	case <-sc.doneServing:

		return http2errClientDisconnected
	}
}

func (g http2goroutineLock) checkNotOn() {
	if !http2DebugGoroutines {
		return
	}
	if http2curGoroutineID() == uint64(g) {
		panic("running on the wrong goroutine")
	}
}

type http2responseWriter struct {
	rws *http2responseWriterState
}

type http2responseWriterState struct {
	// immutable within a request:
	stream *http2stream
	req    *Request
	body   *http2requestBody // to close at end of request, if DATA frames didn't
	conn   *http2serverConn

	// TODO: adjust buffer writing sizes based on server config, frame size updates from peer, etc
	bw *bufio.Writer // writing to a chunkWriter{this *responseWriterState}

	// mutated by http.Handler goroutine:
	handlerHeader Header   // nil until called
	snapHeader    Header   // snapshot of handlerHeader at WriteHeader time
	trailers      []string // set in writeChunk
	status        int      // status code passed to WriteHeader
	wroteHeader   bool     // WriteHeader called (explicitly or implicitly). Not necessarily sent to user yet.
	sentHeader    bool     // have we sent the header frame?
	handlerDone   bool     // handler has finished

	sentContentLen int64 // non-zero if handler set a Content-Length header
	wroteBytes     int64

	closeNotifierMu sync.Mutex // guards closeNotifierCh
	closeNotifierCh chan bool  // nil until first used
}

func http2new400Handler(err error) HandlerFunc {
	return func(w ResponseWriter, r *Request) {
		Error(w, err.Error(), StatusBadRequest)
	}
}

var http2connHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"Transfer-Encoding",
	"Upgrade",
}

func http2checkValidHTTP2RequestHeaders(h Header) error {
	for _, k := range http2connHeaders {
		if _, ok := h[k]; ok {
			return fmt.Errorf("request header %q is not valid in HTTP/2", k)
		}
	}
	te := h["Te"]
	if len(te) > 0 && (len(te) > 1 || (te[0] != "trailers" && te[0] != "")) {
		return errors.New(`request header "TE" may only be "trailers" in HTTP/2`)
	}
	return nil
}

func http2handleHeaderListTooLong(w ResponseWriter, r *Request) {
	// 10.5.1 Limits on Header Block Size:
	// .. "A server that receives a larger header block than it is
	// willing to handle can send an HTTP 431 (Request Header Fields Too
	// Large) status code"
	const statusRequestHeaderFieldsTooLarge = 431 // only in Go 1.6+
	w.WriteHeader(statusRequestHeaderFieldsTooLarge)
	io.WriteString(w, "<h1>HTTP Error 431</h1><p>Request Header Field(s) Too Large</p>")
}

type http2requestBody struct {
	stream        *http2stream
	conn          *http2serverConn
	closed        bool       // for use by Close only
	sawEOF        bool       // for use by Read only
	pipe          *http2pipe // non-nil if we have a HTTP entity message body
	needsContinue bool       // need to send a 100-continue
}

type http2requestParam struct {
	method                  string
	scheme, authority, path string
	header                  Header
}

func (mh *http2MetaHeadersFrame) PseudoValue(pseudo string) string {
	for _, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return ""
		}
		if hf.Name[1:] == pseudo {
			return hf.Value
		}
	}
	return ""
}

func (sc *http2serverConn) newWriterAndRequest(st *http2stream, f *http2MetaHeadersFrame) (*http2responseWriter, *Request, error) {
	sc.serveG.check()

	rp := http2requestParam{
		method:    f.PseudoValue("method"),
		scheme:    f.PseudoValue("scheme"),
		authority: f.PseudoValue("authority"),
		path:      f.PseudoValue("path"),
	}

	isConnect := rp.method == "CONNECT"
	if isConnect {
		if rp.path != "" || rp.scheme != "" || rp.authority == "" {
			return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
		}
	} else if rp.method == "" || rp.path == "" || (rp.scheme != "https" && rp.scheme != "http") {

		return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
	}

	bodyOpen := !f.StreamEnded()
	if rp.method == "HEAD" && bodyOpen {

		return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
	}

	rp.header = make(Header)
	for _, hf := range f.RegularFields() {
		rp.header.Add(sc.canonicalHeader(hf.Name), hf.Value)
	}
	if rp.authority == "" {
		rp.authority = rp.header.Get("Host")
	}

	rw, req, err := sc.newWriterAndRequestNoBody(st, rp)
	if err != nil {
		return nil, nil, err
	}
	if bodyOpen {
		st.reqBuf = http2getRequestBodyBuf()
		req.Body.(*http2requestBody).pipe = &http2pipe{
			b: &http2fixedBuffer{buf: st.reqBuf},
		}

		if vv, ok := rp.header["Content-Length"]; ok {
			req.ContentLength, _ = strconv.ParseInt(vv[0], 10, 64)
		} else {
			req.ContentLength = -1
		}
	}
	return rw, req, nil
}

type http2fixedBuffer struct {
	buf  []byte
	r, w int
}

var http2reqBodyCache = make(chan []byte, 8)

func http2getRequestBodyBuf() []byte {
	select {
	case b := <-http2reqBodyCache:
		return b
	default:
		return make([]byte, http2initialWindowSize)
	}
}

func (sc *http2serverConn) newWriterAndRequestNoBody(st *http2stream, rp http2requestParam) (*http2responseWriter, *Request, error) {
	sc.serveG.check()

	var tlsState *tls.ConnectionState // nil if not scheme https
	if rp.scheme == "https" {
		tlsState = sc.tlsState
	}

	needsContinue := rp.header.Get("Expect") == "100-continue"
	if needsContinue {
		rp.header.Del("Expect")
	}

	if cookies := rp.header["Cookie"]; len(cookies) > 1 {
		rp.header.Set("Cookie", strings.Join(cookies, "; "))
	}

	// Setup Trailers
	var trailer Header
	for _, v := range rp.header["Trailer"] {
		for _, key := range strings.Split(v, ",") {
			key = CanonicalHeaderKey(strings.TrimSpace(key))
			switch key {
			case "Transfer-Encoding", "Trailer", "Content-Length":

			default:
				if trailer == nil {
					trailer = make(Header)
				}
				trailer[key] = nil
			}
		}
	}
	delete(rp.header, "Trailer")

	var url_ *url.URL
	var requestURI string
	if rp.method == "CONNECT" {
		url_ = &url.URL{Host: rp.authority}
		requestURI = rp.authority
	} else {
		var err error
		url_, err = url.ParseRequestURI(rp.path)
		if err != nil {
			return nil, nil, http2streamError(st.id, http2ErrCodeProtocol)
		}
		requestURI = rp.path
	}

	body := &http2requestBody{
		conn:          sc,
		stream:        st,
		needsContinue: needsContinue,
	}
	req := &Request{
		Method:     rp.method,
		URL:        url_,
		RemoteAddr: sc.remoteAddrStr,
		Header:     rp.header,
		RequestURI: requestURI,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		TLS:        tlsState,
		Host:       rp.authority,
		Body:       body,
		Trailer:    trailer,
	}
	req = http2requestWithContext(req, st.ctx)

	rws := http2responseWriterStatePool.Get().(*http2responseWriterState)
	bwSave := rws.bw
	*rws = http2responseWriterState{}
	rws.conn = sc
	rws.bw = bwSave
	rws.bw.Reset(http2chunkWriter{rws})
	rws.stream = st
	rws.req = req
	rws.body = body

	rw := &http2responseWriter{rws: rws}
	return rw, req, nil
}

func http2requestWithContext(req *Request, ctx http2contextContext) *Request {
	return req.WithContext(ctx)
}

func (r *Request) WithContext(ctx context.Context) *Request {
	if ctx == nil {
		panic("nil context")
	}
	r2 := new(Request)
	*r2 = *r
	r2.ctx = ctx
	return r2
}

func (h Header) Add(key, value string) {
	textproto.MIMEHeader(h).Add(key, value)
}

var (
	http2commonLowerHeader = map[string]string{} // Go-Canonical-Case -> lower-case
	http2commonCanonHeader = map[string]string{} // lower-case -> Go-Canonical-Case
)

func (sc *http2serverConn) canonicalHeader(v string) string {
	sc.serveG.check()
	cv, ok := http2commonCanonHeader[v]
	if ok {
		return cv
	}
	cv, ok = sc.canonHeader[v]
	if ok {
		return cv
	}
	if sc.canonHeader == nil {
		sc.canonHeader = make(map[string]string)
	}
	cv = CanonicalHeaderKey(v)
	sc.canonHeader[v] = cv
	return cv
}

func (sc *http2serverConn) canonicalHeader(v string) string {
	sc.serveG.check()
	cv, ok := http2commonCanonHeader[v]
	if ok {
		return cv
	}
	cv, ok = sc.canonHeader[v]
	if ok {
		return cv
	}
	if sc.canonHeader == nil {
		sc.canonHeader = make(map[string]string)
	}
	cv = CanonicalHeaderKey(v)
	sc.canonHeader[v] = cv
	return cv
}

func (h Header) Add(key, value string) {
	textproto.MIMEHeader(h).Add(key, value)
}

func (sc *http2serverConn) canonicalHeader(v string) string {
	sc.serveG.check()
	cv, ok := http2commonCanonHeader[v]
	if ok {
		return cv
	}
	cv, ok = sc.canonHeader[v]
	if ok {
		return cv
	}
	if sc.canonHeader == nil {
		sc.canonHeader = make(map[string]string)
	}
	cv = CanonicalHeaderKey(v)
	sc.canonHeader[v] = cv
	return cv
}

func (mh *http2MetaHeadersFrame) RegularFields() []hpack.HeaderField {
	for i, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return mh.Fields[i:]
		}
	}
	return nil
}

func (f *http2HeadersFrame) StreamEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagHeadersEndStream)
}

func (f *http2HeadersFrame) HasPriority() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagHeadersPriority)
}

func (sc *http2serverConn) newStream(id, pusherID uint32, state http2streamState) *http2stream {
	sc.serveG.check()
	if id == 0 {
		panic("internal error: cannot create stream with id 0")
	}

	ctx, cancelCtx := http2contextWithCancel(sc.baseCtx)
	st := &http2stream{
		sc:        sc,
		id:        id,
		state:     state,
		ctx:       ctx,
		cancelCtx: cancelCtx,
	}
	st.cw.Init()
	st.flow.conn = &sc.flow
	st.flow.add(sc.initialWindowSize)
	st.inflow.conn = &sc.inflow
	st.inflow.add(http2initialWindowSize)

	sc.streams[id] = st
	sc.writeSched.OpenStream(st.id, http2OpenStreamOptions{PusherID: pusherID})
	if st.isPushed() {
		sc.curPushedStreams++
	} else {
		sc.curClientStreams++
	}
	if sc.curOpenStreams() == 1 {
		sc.setConnState(StateActive)
	}

	return st
}

func (cw *http2closeWaiter) Init() {
	*cw = make(chan struct{})
}

func http2contextWithCancel(ctx http2contextContext) (_ http2contextContext, cancel func()) {
	return context.WithCancel(ctx)
}

func (st *http2stream) processTrailerHeaders(f *http2MetaHeadersFrame) error {
	sc := st.sc
	sc.serveG.check()
	if st.gotTrailerHeader {
		return http2ConnectionError(http2ErrCodeProtocol)
	}
	st.gotTrailerHeader = true
	if !f.StreamEnded() {
		return http2streamError(st.id, http2ErrCodeProtocol)
	}

	if len(f.PseudoFields()) > 0 {
		return http2streamError(st.id, http2ErrCodeProtocol)
	}
	if st.trailer != nil {
		for _, hf := range f.RegularFields() {
			key := sc.canonicalHeader(hf.Name)
			if !http2ValidTrailerHeader(key) {

				return http2streamError(st.id, http2ErrCodeProtocol)
			}
			st.trailer[key] = append(st.trailer[key], hf.Value)
		}
	}
	st.endStream()
	return nil
}

func (sc *http2serverConn) processWindowUpdate(f *http2WindowUpdateFrame) error {
	sc.serveG.check()
	switch {
	case f.StreamID != 0:
		state, st := sc.state(f.StreamID)
		if state == http2stateIdle {

			return http2ConnectionError(http2ErrCodeProtocol)
		}
		if st == nil {

			return nil
		}
		if !st.flow.add(int32(f.Increment)) {
			return http2streamError(f.StreamID, http2ErrCodeFlowControl)
		}
	default:
		if !sc.flow.add(int32(f.Increment)) {
			return http2goAwayFlowError{}
		}
	}
	sc.scheduleFrameWrite()
	return nil
}

func (sc *http2serverConn) processResetStream(f *http2RSTStreamFrame) error {
	sc.serveG.check()

	state, st := sc.state(f.StreamID)
	if state == http2stateIdle {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if st != nil {
		st.cancelCtx()
		sc.closeStream(st, http2streamError(f.StreamID, f.ErrCode))
	}
	return nil
}

func (sc *http2serverConn) processSettings(f *http2SettingsFrame) error {
	sc.serveG.check()
	if f.IsAck() {
		sc.unackedSettings--
		if sc.unackedSettings < 0 {

			return http2ConnectionError(http2ErrCodeProtocol)
		}
		return nil
	}
	if err := f.ForeachSetting(sc.processSetting); err != nil {
		return err
	}
	sc.needToSendSettingsAck = true
	sc.scheduleFrameWrite()
	return nil
}

func (f *http2SettingsFrame) IsAck() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagSettingsAck)
}

func (sc *http2serverConn) processSetting(s http2Setting) error {
	sc.serveG.check()
	if err := s.Valid(); err != nil {
		return err
	}
	if http2VerboseLogs {
		sc.vlogf("http2: server processing setting %v", s)
	}
	switch s.ID {
	case http2SettingHeaderTableSize:
		sc.headerTableSize = s.Val
		sc.hpackEncoder.SetMaxDynamicTableSize(s.Val)
	case http2SettingEnablePush:
		sc.pushEnabled = s.Val != 0
	case http2SettingMaxConcurrentStreams:
		sc.clientMaxStreams = s.Val
	case http2SettingInitialWindowSize:
		return sc.processSettingInitialWindowSize(s.Val)
	case http2SettingMaxFrameSize:
		sc.maxFrameSize = int32(s.Val)
	case http2SettingMaxHeaderListSize:
		sc.peerMaxHeaderListSize = s.Val
	default:

		if http2VerboseLogs {
			sc.vlogf("http2: server ignoring unknown setting %v", s)
		}
	}
	return nil
}

func (sc *http2serverConn) processSettingInitialWindowSize(val uint32) error {
	sc.serveG.check()

	old := sc.initialWindowSize
	sc.initialWindowSize = int32(val)
	growth := sc.initialWindowSize - old
	for _, st := range sc.streams {
		if !st.flow.add(growth) {

			return http2ConnectionError(http2ErrCodeFlowControl)
		}
	}
	return nil
}

func (s http2Setting) Valid() error {

	switch s.ID {
	case http2SettingEnablePush:
		if s.Val != 1 && s.Val != 0 {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
	case http2SettingInitialWindowSize:
		if s.Val > 1<<31-1 {
			return http2ConnectionError(http2ErrCodeFlowControl)
		}
	case http2SettingMaxFrameSize:
		if s.Val < 16384 || s.Val > 1<<24-1 {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
	}
	return nil
}

func http2isClosedConnError(err error) bool {
	if err == nil {
		return false
	}

	str := err.Error()
	if strings.Contains(str, "use of closed network connection") {
		return true
	}

	return false
}

func http2errno(v error) uintptr {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Uintptr {
		return uintptr(rv.Uint())
	}
	return 0
}

var (
	ErrNotSupported          = &ProtocolError{"feature not supported"}
	http2ErrPushLimitReached = errors.New("http2: push would exceed peer's SETTINGS_MAX_CONCURRENT_STREAMS")
)

type ProtocolError struct {
	ErrorString string
}

func (pe *ProtocolError) Error() string { return pe.ErrorString }

func (sc *http2serverConn) startPush(msg http2startPushRequest) {
	sc.serveG.check()

	if msg.parent.state != http2stateOpen && msg.parent.state != http2stateHalfClosedRemote {

		msg.done <- http2errStreamClosed
		return
	}

	if !sc.pushEnabled {
		msg.done <- ErrNotSupported
		return
	}

	allocatePromisedID := func() (uint32, error) {
		sc.serveG.check()

		if !sc.pushEnabled {
			return 0, ErrNotSupported
		}

		if sc.curPushedStreams+1 > sc.clientMaxStreams {
			return 0, http2ErrPushLimitReached
		}

		if sc.maxPushPromiseID+2 >= 1<<31 {
			sc.startGracefulShutdown()
			return 0, http2ErrPushLimitReached
		}
		sc.maxPushPromiseID += 2
		promisedID := sc.maxPushPromiseID

		promised := sc.newStream(promisedID, msg.parent.id, http2stateHalfClosedRemote)
		rw, req, err := sc.newWriterAndRequestNoBody(promised, http2requestParam{
			method:    msg.method,
			scheme:    msg.url.Scheme,
			authority: msg.url.Host,
			path:      msg.url.RequestURI(),
			header:    http2cloneHeader(msg.header),
		})
		if err != nil {

			panic(fmt.Sprintf("newWriterAndRequestNoBody(%+v): %v", msg.url, err))
		}

		go sc.runHandler(rw, req, sc.handler.ServeHTTP)
		return promisedID, nil
	}

	sc.writeFrame(http2FrameWriteRequest{
		write: &http2writePushPromise{
			streamID:           msg.parent.id,
			method:             msg.method,
			url:                msg.url,
			h:                  msg.header,
			allocatePromisedID: allocatePromisedID,
		},
		stream: msg.parent,
		done:   msg.done,
	})
}

func http2cloneHeader(h Header) Header {
	h2 := make(Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

const (
	http2prefaceTimeout        = 10 * time.Second
	http2firstSettingsTimeout  = 2 * time.Second // should be in-flight with preface anyway
	http2handlerChunkWriteSize = 4 << 10
	http2defaultMaxStreams     = 250 // TODO: make this 100 as the GFE seems to?
)

func (sc *http2serverConn) readFrames() {
	gate := make(http2gate)
	gateDone := gate.Done
	for {
		f, err := sc.framer.ReadFrame()
		select {
		case sc.readFrameCh <- http2readFrameResult{f, err, gateDone}:
		case <-sc.doneServing:
			return
		}
		select {
		case <-gate:
		case <-sc.doneServing:
			return
		}
		if http2terminalReadFrameError(err) {
			return
		}
	}
}

func http2terminalReadFrameError(err error) bool {
	if _, ok := err.(http2StreamError); ok {
		return false
	}
	return err != nil
}

type http2gate chan struct{}

func (g http2gate) Done() { g <- struct{}{} }

var http2testh1ServerShutdownChan func(hs *Server) <-chan struct{}

func http2h1ServerShutdownChan(hs *Server) <-chan struct{} {
	if fn := http2testh1ServerShutdownChan; fn != nil {
		return fn(hs)
	}
	var x interface{} = hs
	type I interface {
		getDoneChan() <-chan struct{}
	}
	if hs, ok := x.(I); ok {
		return hs.getDoneChan()
	}
	return nil
}

func (sc *http2serverConn) awaitGracefulShutdown(sharedCh <-chan struct{}, privateCh chan struct{}) {
	select {
	case <-sc.doneServing:
	case <-sharedCh:
		close(privateCh)
	}
}

func (sc *http2serverConn) condlogf(err error, format string, args ...interface{}) {
	if err == nil {
		return
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF || http2isClosedConnError(err) {

		sc.vlogf(format, args...)
	} else {
		sc.logf(format, args...)
	}
}

const http2ClientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

func (sc *http2serverConn) readPreface() error {
	errc := make(chan error, 1)
	go func() {

		buf := make([]byte, len(http2ClientPreface))
		if _, err := io.ReadFull(sc.conn, buf); err != nil {
			errc <- err
		} else if !bytes.Equal(buf, http2clientPreface) {
			errc <- fmt.Errorf("bogus greeting %q", buf)
		} else {
			errc <- nil
		}
	}()
	timer := time.NewTimer(http2prefaceTimeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		return errors.New("timeout waiting for client preface")
	case err := <-errc:
		if err == nil {
			if http2VerboseLogs {
				sc.vlogf("http2: server: client %v said hello", sc.conn.RemoteAddr())
			}
		}
		return err
	}
}

var (
	http2clientPreface = []byte(http2ClientPreface)
)

var http2testHookGetServerConn func(*http2serverConn)

func (sc *http2serverConn) rejectConn(err http2ErrCode, debug string) {
	sc.vlogf("http2: server rejecting conn: %v, %s", err, debug)

	sc.framer.WriteGoAway(0, err, []byte(debug))
	sc.bw.Flush()
	sc.conn.Close()
}

var http2bufWriterPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewWriterSize(nil, http2bufWriterPoolBufferSize)
	},
}

func (w *http2bufferedWriter) Flush() error {
	bw := w.bw
	if bw == nil {
		return nil
	}
	err := bw.Flush()
	bw.Reset(nil)
	http2bufWriterPool.Put(bw)
	w.bw = nil
	return err
}

const http2ErrCodeInadequateSecurity http2ErrCode = 0xc

type http2connectionStater interface {
	ConnectionState() tls.ConnectionState
}

const http2minMaxFrameSize = 1 << 14

const http2defaultMaxReadFrameSize = 1 << 20
const http2maxFrameSize = 1<<24 - 1

func (s *http2Server) maxReadFrameSize() uint32 {
	if v := s.MaxReadFrameSize; v >= http2minMaxFrameSize && v <= http2maxFrameSize {
		return v
	}
	return http2defaultMaxReadFrameSize
}

func (fr *http2Framer) SetMaxReadFrameSize(v uint32) {
	if v > http2maxFrameSize {
		v = http2maxFrameSize
	}
	fr.maxReadSize = v
}

func (sc *http2serverConn) maxHeaderListSize() uint32 {
	n := sc.hs.MaxHeaderBytes
	if n <= 0 {
		n = DefaultMaxHeaderBytes
	}
	// http2's count is in a slightly different unit and includes 32 bytes per pair.
	// So, take the net/http.Server value and pad it up a bit, assuming 10 headers.
	const perFieldOverhead = 32 // per http2 spec
	const typicalHeaders = 10   // conservative
	return uint32(n + typicalHeaders*perFieldOverhead)
}

var http2logFrameReads bool
var http2logFrameWrites bool

func http2NewFramer(w io.Writer, r io.Reader) *http2Framer {
	fr := &http2Framer{
		w:                 w,
		r:                 r,
		logReads:          http2logFrameReads,
		logWrites:         http2logFrameWrites,
		debugReadLoggerf:  log.Printf,
		debugWriteLoggerf: log.Printf,
	}
	fr.getReadBuf = func(size uint32) []byte {
		if cap(fr.readBuf) >= int(size) {
			return fr.readBuf[:size]
		}
		fr.readBuf = make([]byte, size)
		return fr.readBuf
	}
	fr.SetMaxReadFrameSize(http2maxFrameSize)
	return fr
}

func (f *http2flow) add(n int32) bool {
	remain := (1<<31 - 1) - f.n
	if n > remain {
		return false
	}
	f.n += n
	return true
}

func http2NewRandomWriteScheduler() http2WriteScheduler {
	return &http2randomWriteScheduler{sq: make(map[uint32]*http2writeQueue)}
}

type http2writeQueue struct {
	s []http2FrameWriteRequest
}

type http2randomWriteScheduler struct {
	// zero are frames not associated with a specific stream.
	zero http2writeQueue

	// sq contains the stream-specific queues, keyed by stream ID.
	// When a stream is idle or closed, it's deleted from the map.
	sq map[uint32]*http2writeQueue

	// pool of empty queues for reuse.
	queuePool http2writeQueuePool
}

type http2writeQueuePool []*http2writeQueue

var http2testHookOnConn func()

func (srv *Server) shouldConfigureHTTP2ForServe() bool {
	if srv.TLSConfig == nil {
		return true
	}
	return strSliceContains(srv.TLSConfig.NextProtos, http2NextProtoTLS)
}

func strSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

var testHookServerServe func(*Server, net.Listener) // used if non-nil

func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()
	if fn := testHookServerServe; fn != nil {
		fn(srv, l)
	}
	var tempDelay time.Duration // how long to sleep on accept failure

	if err := srv.setupHTTP2_Serve(); err != nil {
		return err
	}

	srv.trackListener(l, true)
	defer srv.trackListener(l, false)

	baseCtx := context.Background() // base is always background, per Issue 16220
	ctx := context.WithValue(baseCtx, ServerContextKey, srv)
	ctx = context.WithValue(ctx, LocalAddrContextKey, l.Addr())
	for {
		rw, e := l.Accept()
		if e != nil {
			select {
			case <-srv.getDoneChan():
				return ErrServerClosed
			default:
			}
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				srv.logf("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c := srv.newConn(rw)
		c.setState(c.rwc, StateNew) // before Serve can return
		go c.serve(ctx)
	}
}

func (c *conn) serve(ctx context.Context) {
	c.remoteAddr = c.rwc.RemoteAddr().String()
	defer func() {
		if err := recover(); err != nil && err != ErrAbortHandler {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			c.server.logf("http: panic serving %v: %v\n%s", c.remoteAddr, err, buf)
		}
		if !c.hijacked() {
			c.close()
			c.setState(c.rwc, StateClosed)
		}
	}()

	if tlsConn, ok := c.rwc.(*tls.Conn); ok {
		if d := c.server.ReadTimeout; d != 0 {
			c.rwc.SetReadDeadline(time.Now().Add(d))
		}
		if d := c.server.WriteTimeout; d != 0 {
			c.rwc.SetWriteDeadline(time.Now().Add(d))
		}
		if err := tlsConn.Handshake(); err != nil {
			c.server.logf("http: TLS handshake error from %s: %v", c.rwc.RemoteAddr(), err)
			return
		}
		c.tlsState = new(tls.ConnectionState)
		*c.tlsState = tlsConn.ConnectionState()
		if proto := c.tlsState.NegotiatedProtocol; validNPN(proto) {
			if fn := c.server.TLSNextProto[proto]; fn != nil {
				h := initNPNRequest{tlsConn, serverHandler{c.server}}
				fn(c.server, tlsConn, h)
			}
			return
		}
	}

	// HTTP/1.x from here on.

	ctx, cancelCtx := context.WithCancel(ctx)
	c.cancelCtx = cancelCtx
	defer cancelCtx()

	c.r = &connReader{conn: c}
	c.bufr = newBufioReader(c.r)
	c.bufw = newBufioWriterSize(checkConnErrorWriter{c}, 4<<10)

	for {
		w, err := c.readRequest(ctx)
		if c.r.remain != c.server.initialReadLimitSize() {
			// If we read any bytes off the wire, we're active.
			c.setState(c.rwc, StateActive)
		}
		if err != nil {
			const errorHeaders = "\r\nContent-Type: text/plain; charset=utf-8\r\nConnection: close\r\n\r\n"

			if err == errTooLarge {
				// Their HTTP client may or may not be
				// able to read this if we're
				// responding to them and hanging up
				// while they're still writing their
				// request. Undefined behavior.
				const publicErr = "431 Request Header Fields Too Large"
				fmt.Fprintf(c.rwc, "HTTP/1.1 "+publicErr+errorHeaders+publicErr)
				c.closeWriteAndWait()
				return
			}
			if isCommonNetReadError(err) {
				return // don't reply
			}

			publicErr := "400 Bad Request"
			if v, ok := err.(badRequestError); ok {
				publicErr = publicErr + ": " + string(v)
			}

			fmt.Fprintf(c.rwc, "HTTP/1.1 "+publicErr+errorHeaders+publicErr)
			return
		}

		// Expect 100 Continue support
		req := w.req
		if req.expectsContinue() {
			if req.ProtoAtLeast(1, 1) && req.ContentLength != 0 {
				// Wrap the Body reader with one that replies on the connection
				req.Body = &expectContinueReader{readCloser: req.Body, resp: w}
			}
		} else if req.Header.get("Expect") != "" {
			w.sendExpectationFailed()
			return
		}

		c.curReq.Store(w)

		if requestBodyRemains(req.Body) {
			registerOnHitEOF(req.Body, w.conn.r.startBackgroundRead)
		} else {
			if w.conn.bufr.Buffered() > 0 {
				w.conn.r.closeNotifyFromPipelinedRequest()
			}
			w.conn.r.startBackgroundRead()
		}

		// HTTP cannot have multiple simultaneous active requests.[*]
		// Until the server replies to this request, it can't read another,
		// so we might as well run the handler in this goroutine.
		// [*] Not strictly true: HTTP pipelining. We could let them all process
		// in parallel even if their responses need to be serialized.
		// But we're not going to implement HTTP pipelining because it
		// was never deployed in the wild and the answer is HTTP/2.
		serverHandler{c.server}.ServeHTTP(w, w.req)
		w.cancelCtx()
		if c.hijacked() {
			return
		}
		w.finishRequest()
		if !w.shouldReuseConnection() {
			if w.requestBodyLimitHit || w.closedRequestBodyEarly() {
				c.closeWriteAndWait()
			}
			return
		}
		c.setState(c.rwc, StateIdle)
		c.curReq.Store((*response)(nil))

		if !w.conn.server.doKeepAlives() {
			// We're in shutdown mode. We might've replied
			// to the user without "Connection: close" and
			// they might think they can send another
			// request, but such is life with HTTP/1.1.
			return
		}

		if d := c.server.idleTimeout(); d != 0 {
			c.rwc.SetReadDeadline(time.Now().Add(d))
			if _, err := c.bufr.Peek(4); err != nil {
				return
			}
		}
		c.rwc.SetReadDeadline(time.Time{})
	}
}
func (w *response) sendExpectationFailed() {
	w.Header().Set("Connection", "close")
	w.WriteHeader(StatusExpectationFailed)
	w.finishRequest()
}

type expectContinueReader struct {
	resp       *response
	readCloser io.ReadCloser
	closed     bool
	sawEOF     bool
}

func (r *Request) expectsContinue() bool {
	return hasToken(r.Header.get("Expect"), "100-continue")
}
func hasToken(v, token string) bool {
	if len(token) > len(v) || token == "" {
		return false
	}
	if v == token {
		return true
	}
	for sp := 0; sp <= len(v)-len(token); sp++ {
		// Check that first character is good.
		// The token is ASCII, so checking only a single byte
		// is sufficient. We skip this potential starting
		// position if both the first byte and its potential
		// ASCII uppercase equivalent (b|0x20) don't match.
		// False positives ('^' => '~') are caught by EqualFold.
		if b := v[sp]; b != token[0] && b|0x20 != token[0] {
			continue
		}
		// Check that start pos is on a valid token boundary.
		if sp > 0 && !isTokenBoundary(v[sp-1]) {
			continue
		}
		// Check that end pos is on a valid token boundary.
		if endPos := sp + len(token); endPos != len(v) && !isTokenBoundary(v[endPos]) {
			continue
		}
		if strings.EqualFold(v[sp:sp+len(token)], token) {
			return true
		}
	}
	return false
}

func isTokenBoundary(b byte) bool {
	return b == ' ' || b == ',' || b == '\t'
}

// A response represents the server side of an HTTP response.
type response struct {
	conn             *conn
	req              *Request // request for this response
	reqBody          io.ReadCloser
	cancelCtx        context.CancelFunc // when ServeHTTP exits
	wroteHeader      bool               // reply header has been (logically) written
	wroteContinue    bool               // 100 Continue response was written
	wants10KeepAlive bool               // HTTP/1.0 w/ Connection "keep-alive"
	wantsClose       bool               // HTTP request has Connection "close"

	w  *bufio.Writer // buffers output in chunks to chunkWriter
	cw chunkWriter

	// handlerHeader is the Header that Handlers get access to,
	// which may be retained and mutated even after WriteHeader.
	// handlerHeader is copied into cw.header at WriteHeader
	// time, and privately mutated thereafter.
	handlerHeader Header
	calledHeader  bool // handler accessed handlerHeader via Header

	written       int64 // number of bytes written in body
	contentLength int64 // explicitly-declared Content-Length; or -1
	status        int   // status code passed to WriteHeader

	// close connection after this reply.  set on request and
	// updated after response from handler if there's a
	// "Connection: keep-alive" response header and a
	// Content-Length.
	closeAfterReply bool

	// requestBodyLimitHit is set by requestTooLarge when
	// maxBytesReader hits its max size. It is checked in
	// WriteHeader, to make sure we don't consume the
	// remaining request body to try to advance to the next HTTP
	// request. Instead, when this is set, we stop reading
	// subsequent requests on this connection and stop reading
	// input from it.
	requestBodyLimitHit bool

	// trailers are the headers to be sent after the handler
	// finishes writing the body. This field is initialized from
	// the Trailer response header when the response header is
	// written.
	trailers []string

	handlerDone atomicBool // set true when the handler exits

	// Buffers for Date and Content-Length
	dateBuf [len(TimeFormat)]byte
	clenBuf [10]byte

	// closeNotifyCh is the channel returned by CloseNotify.
	// TODO(bradfitz): this is currently (for Go 1.8) always
	// non-nil. Make this lazily-created again as it used to be?
	closeNotifyCh  chan bool
	didCloseNotify int32 // atomic (only 0->1 winner should send)
}

type badRequestError string

func (e badRequestError) Error() string { return "Bad Request: " + string(e) }

func isCommonNetReadError(err error) bool {
	if err == io.EOF {
		return true
	}
	if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
		return true
	}
	if oe, ok := err.(*net.OpError); ok && oe.Op == "read" {
		return true
	}
	return false
}
func (c *conn) closeWriteAndWait() {
	c.finalFlush()
	if tcp, ok := c.rwc.(closeWriter); ok {
		tcp.CloseWrite()
	}
	time.Sleep(rstAvoidanceDelay)
}

const DefaultMaxHeaderBytes = 1 << 20 // 1 MB

func (srv *Server) maxHeaderBytes() int {
	if srv.MaxHeaderBytes > 0 {
		return srv.MaxHeaderBytes
	}
	return DefaultMaxHeaderBytes
}

func (srv *Server) initialReadLimitSize() int64 {
	return int64(srv.maxHeaderBytes()) + 4096 // bufio slop
}

var errTooLarge = errors.New("http: request too large")

func (c *conn) readRequest(ctx context.Context) (w *response, err error) {
	if c.hijacked() {
		return nil, ErrHijacked
	}

	var (
		wholeReqDeadline time.Time // or zero if none
		hdrDeadline      time.Time // or zero if none
	)
	t0 := time.Now()
	if d := c.server.readHeaderTimeout(); d != 0 {
		hdrDeadline = t0.Add(d)
	}
	if d := c.server.ReadTimeout; d != 0 {
		wholeReqDeadline = t0.Add(d)
	}
	c.rwc.SetReadDeadline(hdrDeadline)
	if d := c.server.WriteTimeout; d != 0 {
		defer func() {
			c.rwc.SetWriteDeadline(time.Now().Add(d))
		}()
	}

	c.r.setReadLimit(c.server.initialReadLimitSize())
	if c.lastMethod == "POST" {
		// RFC 2616 section 4.1 tolerance for old buggy clients.
		peek, _ := c.bufr.Peek(4) // ReadRequest will get err below
		c.bufr.Discard(numLeadingCRorLF(peek))
	}
	req, err := readRequest(c.bufr, keepHostHeader)
	if err != nil {
		if c.r.hitReadLimit() {
			return nil, errTooLarge
		}
		return nil, err
	}

	if !http1ServerSupportsRequest(req) {
		return nil, badRequestError("unsupported protocol version")
	}

	c.lastMethod = req.Method
	c.r.setInfiniteReadLimit()

	hosts, haveHost := req.Header["Host"]
	isH2Upgrade := req.isH2Upgrade()
	if req.ProtoAtLeast(1, 1) && (!haveHost || len(hosts) == 0) && !isH2Upgrade {
		return nil, badRequestError("missing required Host header")
	}
	if len(hosts) > 1 {
		return nil, badRequestError("too many Host headers")
	}
	if len(hosts) == 1 && !httplex.ValidHostHeader(hosts[0]) {
		return nil, badRequestError("malformed Host header")
	}
	for k, vv := range req.Header {
		if !httplex.ValidHeaderFieldName(k) {
			return nil, badRequestError("invalid header name")
		}
		for _, v := range vv {
			if !httplex.ValidHeaderFieldValue(v) {
				return nil, badRequestError("invalid header value")
			}
		}
	}
	delete(req.Header, "Host")

	ctx, cancelCtx := context.WithCancel(ctx)
	req.ctx = ctx
	req.RemoteAddr = c.remoteAddr
	req.TLS = c.tlsState
	if body, ok := req.Body.(*body); ok {
		body.doEarlyClose = true
	}

	// Adjust the read deadline if necessary.
	if !hdrDeadline.Equal(wholeReqDeadline) {
		c.rwc.SetReadDeadline(wholeReqDeadline)
	}

	w = &response{
		conn:          c,
		cancelCtx:     cancelCtx,
		req:           req,
		reqBody:       req.Body,
		handlerHeader: make(Header),
		contentLength: -1,
		closeNotifyCh: make(chan bool, 1),

		// We populate these ahead of time so we're not
		// reading from req.Header after their Handler starts
		// and maybe mutates it (Issue 14940)
		wants10KeepAlive: req.wantsHttp10KeepAlive(),
		wantsClose:       req.wantsClose(),
	}
	if isH2Upgrade {
		w.closeAfterReply = true
	}
	w.cw.res = w
	w.w = newBufioWriterSize(&w.cw, bufferBeforeChunkingSize)
	return w, nil
}

type checkConnErrorWriter struct {
	c *conn
}

func newBufioReader(r io.Reader) *bufio.Reader {
	if v := bufioReaderPool.Get(); v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	// Note: if this reader size is ever changed, update
	// TestHandlerBodyClose's assumptions.
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

type connReader struct {
	conn *conn

	mu      sync.Mutex // guards following
	hasByte bool
	byteBuf [1]byte
	bgErr   error // non-nil means error happened on background read
	cond    *sync.Cond
	inRead  bool
	aborted bool  // set true before conn.rwc deadline is set to past
	remain  int64 // bytes remaining
}

type serverHandler struct {
	srv *Server
}

type initNPNRequest struct {
	c *tls.Conn
	h serverHandler
}

func validNPN(proto string) bool {
	switch proto {
	case "", "http/1.1", "http/1.0":
		return false
	}
	return true
}

var (
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

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	if pool := bufioWriterPool(bw.Available()); pool != nil {
		pool.Put(bw)
	}
}

func (c *conn) finalFlush() {
	if c.bufr != nil {
		// Steal the bufio.Reader (~4KB worth of memory) and its associated
		// reader for a future connection.
		putBufioReader(c.bufr)
		c.bufr = nil
	}

	if c.bufw != nil {
		c.bufw.Flush()
		// Steal the bufio.Writer (~4KB worth of memory) and its associated
		// writer for a future connection.
		putBufioWriter(c.bufw)
		c.bufw = nil
	}
}

// Close the connection.
func (c *conn) close() {
	c.finalFlush()
	c.rwc.Close()
}

func (c *conn) hijacked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hijackedv
}

func (c *conn) setState(nc net.Conn, state ConnState) {
	srv := c.server
	switch state {
	case StateNew:
		srv.trackConn(c, true)
	case StateHijacked, StateClosed:
		srv.trackConn(c, false)
	}
	c.curState.Store(connStateInterface[state])
	if hook := srv.ConnState; hook != nil {
		hook(nc, state)
	}
}

var connStateInterface = [...]interface{}{
	StateNew:      StateNew,
	StateActive:   StateActive,
	StateIdle:     StateIdle,
	StateHijacked: StateHijacked,
	StateClosed:   StateClosed,
}

func (s *Server) trackConn(c *conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeConn == nil {
		s.activeConn = make(map[*conn]struct{})
	}
	if add {
		s.activeConn[c] = struct{}{}
	} else {
		delete(s.activeConn, c)
	}
}

// A conn represents the server side of an HTTP connection.
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

	// lastMethod is the method of the most recent request
	// on this connection, if any.
	lastMethod string

	curReq atomic.Value // of *response (which has a Request in it)

	curState atomic.Value // of ConnState

	// mu guards hijackedv
	mu sync.Mutex

	// hijackedv is whether this connection has been hijacked
	// by a Handler with the Hijacker interface.
	// It is guarded by mu.
	hijackedv bool
}

func (srv *Server) newConn(rwc net.Conn) *conn {
	c := &conn{
		server: srv,
		rwc:    rwc,
	}
	if debugServerConnections {
		c.rwc = newLoggingConn("server", c.rwc)
	}
	return c
}

func (s *Server) logf(format string, args ...interface{}) {
	if s.ErrorLog != nil {
		s.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

var ErrServerClosed = errors.New("http: Server closed")

func (s *Server) getDoneChan() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getDoneChanLocked()
}

func (s *Server) getDoneChanLocked() chan struct{} {
	if s.doneChan == nil {
		s.doneChan = make(chan struct{})
	}
	return s.doneChan
}

func (s *Server) trackListener(ln net.Listener, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = make(map[net.Listener]struct{})
	}
	if add {
		// If the *Server is being reused after a previous
		// Close or Shutdown, reset its doneChan:
		if len(s.listeners) == 0 && len(s.activeConn) == 0 {
			s.doneChan = nil
		}
		s.listeners[ln] = struct{}{}
	} else {
		delete(s.listeners, ln)
	}
}

func (srv *Server) ListenAndServe() error {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
}

type Server struct {
	Addr      string      // TCP address to listen on, ":http" if empty
	Handler   Handler     // handler to invoke, http.DefaultServeMux if nil
	TLSConfig *tls.Config // optional TLS config, used by ListenAndServeTLS

	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	//
	// Because ReadTimeout does not let Handlers make per-request
	// decisions on each request body's acceptable deadline or
	// upload rate, most users will prefer to use
	// ReadHeaderTimeout. It is valid to use them both.
	ReadTimeout time.Duration

	// ReadHeaderTimeout is the amount of time allowed to read
	// request headers. The connection's read deadline is reset
	// after reading the headers and the Handler can decide what
	// is considered too slow for the body.
	ReadHeaderTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset whenever a new
	// request's header is read. Like ReadTimeout, it does not
	// let Handlers make decisions on a per-request basis.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, there is no timeout.
	IdleTimeout time.Duration

	// MaxHeaderBytes controls the maximum number of bytes the
	// server will read parsing the request header's keys and
	// values, including the request line. It does not limit the
	// size of the request body.
	// If zero, DefaultMaxHeaderBytes is used.
	MaxHeaderBytes int

	// TLSNextProto optionally specifies a function to take over
	// ownership of the provided TLS connection when an NPN/ALPN
	// protocol upgrade has occurred. The map key is the protocol
	// name negotiated. The Handler argument should be used to
	// handle HTTP requests and will initialize the Request's TLS
	// and RemoteAddr if not already set. The connection is
	// automatically closed when the function returns.
	// If TLSNextProto is not nil, HTTP/2 support is not enabled
	// automatically.
	TLSNextProto map[string]func(*Server, *tls.Conn, Handler)

	// ConnState specifies an optional callback function that is
	// called when a client connection changes state. See the
	// ConnState type and associated constants for details.
	ConnState func(net.Conn, ConnState)

	// ErrorLog specifies an optional logger for errors accepting
	// connections and unexpected behavior from handlers.
	// If nil, logging goes to os.Stderr via the log package's
	// standard logger.
	ErrorLog *log.Logger

	disableKeepAlives int32     // accessed atomically.
	inShutdown        int32     // accessed atomically (non-zero means we're in Shutdown)
	nextProtoOnce     sync.Once // guards setupHTTP2_* init
	nextProtoErr      error     // result of http2.ConfigureServer if used

	mu         sync.Mutex
	listeners  map[net.Listener]struct{}
	activeConn map[*conn]struct{}
	doneChan   chan struct{}
}

func (mux *ServeMux) Handle(pattern string, handler Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if pattern == "" {
		panic("http: invalid pattern " + pattern)
	}
	if handler == nil {
		panic("http: nil handler")
	}
	if mux.m[pattern].explicit {
		panic("http: multiple registrations for " + pattern)
	}

	if mux.m == nil {
		mux.m = make(map[string]muxEntry)
	}
	mux.m[pattern] = muxEntry{explicit: true, h: handler, pattern: pattern}

	if pattern[0] != '/' {
		mux.hosts = true
	}

	// Helpful behavior:
	// If pattern is /tree/, insert an implicit permanent redirect for /tree.
	// It can be overridden by an explicit registration.
	n := len(pattern)
	if n > 0 && pattern[n-1] == '/' && !mux.m[pattern[0:n-1]].explicit {
		// If pattern contains a host name, strip it and use remaining
		// path for redirect.
		path := pattern
		if pattern[0] != '/' {
			// In pattern, at least the last character is a '/', so
			// strings.Index can't be -1.
			path = pattern[strings.Index(pattern, "/"):]
		}
		url := &url.URL{Path: path}
		mux.m[pattern[0:n-1]] = muxEntry{h: RedirectHandler(url.String(), StatusMovedPermanently), pattern: pattern}
	}
}

type redirectHandler struct {
	url  string
	code int
}

func (rh *redirectHandler) ServeHTTP(w ResponseWriter, r *Request) {
	Redirect(w, r, rh.url, rh.code)
}

func Redirect(w ResponseWriter, r *Request, urlStr string, code int) {
	if u, err := url.Parse(urlStr); err == nil {
		if u.Scheme == "" && u.Host == "" {
			oldpath := r.URL.Path
			if oldpath == "" { // should not happen, but avoid a crash if it does
				oldpath = "/"
			}

			// no leading http://server
			if urlStr == "" || urlStr[0] != '/' {
				// make relative path absolute
				olddir, _ := path.Split(oldpath)
				urlStr = olddir + urlStr
			}

			var query string
			if i := strings.Index(urlStr, "?"); i != -1 {
				urlStr, query = urlStr[:i], urlStr[i:]
			}

			// clean up but preserve trailing slash
			trailing := strings.HasSuffix(urlStr, "/")
			urlStr = path.Clean(urlStr)
			if trailing && !strings.HasSuffix(urlStr, "/") {
				urlStr += "/"
			}
			urlStr += query
		}
	}

	w.Header().Set("Location", hexEscapeNonASCII(urlStr))
	w.WriteHeader(code)

	// RFC 2616 recommends that a short note "SHOULD" be included in the
	// response because older user agents may not understand 301/307.
	// Shouldn't send the response for POST or HEAD; that leaves GET.
	if r.Method == "GET" {
		note := "<a href=\"" + htmlEscape(urlStr) + "\">" + statusText[code] + "</a>.\n"
		fmt.Fprintln(w, note)
	}
}

func htmlEscape(s string) string {
	return htmlReplacer.Replace(s)
}

func hexEscapeNonASCII(s string) string {
	newLen := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			newLen += 3
		} else {
			newLen++
		}
	}
	if newLen == len(s) {
		return s
	}
	b := make([]byte, 0, newLen)
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			b = append(b, '%')
			b = strconv.AppendInt(b, int64(s[i]), 16)
		} else {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// RedirectHandler returns a request handler that redirects
// each request it receives to the given url using the given
// status code.
//
// The provided code should be in the 3xx range and is usually
// StatusMovedPermanently, StatusFound or StatusSeeOther.
func RedirectHandler(url string, code int) Handler {
	return &redirectHandler{url, code}
}

var DefaultServeMux = &defaultServeMux

var defaultServeMux ServeMux

type ServeMux struct {
	mu    sync.RWMutex
	m     map[string]muxEntry
	hosts bool // whether any patterns contain hostnames
}

type muxEntry struct {
	explicit bool
	h        Handler
	pattern  string
}

func Handle(pattern string, handler Handler) { DefaultServeMux.Handle(pattern, handler) }

type Dir string

type File interface {
	io.Closer
	io.Reader
	io.Seeker
	Readdir(count int) ([]os.FileInfo, error)
	Stat() (os.FileInfo, error)
}

func (d Dir) Open(name string) (File, error) {
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) ||
		strings.Contains(name, "\x00") {
		return nil, errors.New("http: invalid character in file path")
	}
	dir := string(d)
	if dir == "" {
		dir = "."
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(path.Clean("/"+name))))
	if err != nil {
		return nil, err
	}
	return f, nil
}

type FileSystem interface {
	Open(name string) (File, error)
}

func FileServer(root FileSystem) Handler {
	return &fileHandler{root}
}

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

type Request struct {
	Method string

	URL *url.URL

	Proto      string
	ProtoMajor int
	ProtoMinor int

	Header Header

	Body io.ReadCloser

	GetBody func() (io.ReadCloser, error)

	ContentLength int64

	TransferEncoding []string

	Close bool

	Host string

	Form url.Values

	PostForm url.Values

	MultipartForm *multipart.Form

	Trailer Header

	RemoteAddr string

	RequestURI string

	TLS *tls.ConnectionState

	Cancel <-chan struct{}

	Response *Response

	ctx context.Context
}

type Response struct {
	Status     string // e.g. "200 OK"
	StatusCode int    // e.g. 200
	Proto      string // e.g. "HTTP/1.0"
	ProtoMajor int    // e.g. 1
	ProtoMinor int    // e.g. 0

	Header Header

	Body io.ReadCloser

	ContentLength int64

	TransferEncoding []string

	Close bool

	Uncompressed bool

	Trailer Header

	Request *Request

	TLS *tls.ConnectionState
}

type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(int)
}

type Header map[string][]string

type fileHandler struct {
	root FileSystem
}

func (h Header) Set(key, value string) {
	textproto.MIMEHeader(h).Set(key, value)
}

func (f *fileHandler) ServeHTTP(w ResponseWriter, r *Request) {
	upath := r.URL.Path
	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
		r.URL.Path = upath
	}
	serveFile(w, r, f.root, path.Clean(upath), true)
}

func serveFile(w ResponseWriter, r *Request, fs FileSystem, name string, redirect bool) {
	const indexPage = "/index.html"

	if strings.HasSuffix(r.URL.Path, indexPage) {
		localRedirect(w, r, "./")
		return
	}

	f, err := fs.Open(name)
	if err != nil {
		msg, code := toHTTPError(err)
		Error(w, msg, code)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		msg, code := toHTTPError(err)
		Error(w, msg, code)
		return
	}

	if redirect {
		url := r.URL.Path
		if d.IsDir() {
			if url[len(url)-1] != '/' {
				localRedirect(w, r, path.Base(url)+"/")
				return
			}
		} else {
			if url[len(url)-1] == '/' {
				localRedirect(w, r, "../"+path.Base(url))
				return
			}
		}
	}

	// redirect if the directory name doesn't end in a slash
	if d.IsDir() {
		url := r.URL.Path
		if url[len(url)-1] != '/' {
			localRedirect(w, r, path.Base(url)+"/")
			return
		}
	}

	// use contents of index.html for directory, if present
	if d.IsDir() {
		index := strings.TrimSuffix(name, "/") + indexPage
		ff, err := fs.Open(index)
		if err == nil {
			defer ff.Close()
			dd, err := ff.Stat()
			if err == nil {
				name = index
				d = dd
				f = ff
			}
		}
	}

	// Still a directory? (we didn't find an index.html file)
	if d.IsDir() {
		if checkIfModifiedSince(w, r, d.ModTime()) == condFalse {
			writeNotModified(w)
			return
		}
		w.Header().Set("Last-Modified", d.ModTime().UTC().Format(TimeFormat))
		dirList(w, f)
		return
	}

	// serveContent will check modification time
	sizeFunc := func() (int64, error) { return d.Size(), nil }
	serveContent(w, r, d.Name(), d.ModTime(), sizeFunc, f)
}

const TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

type condResult int

const (
	condNone condResult = iota
	condTrue
	condFalse
)

var timeFormats = []string{
	TimeFormat,
	time.RFC850,
	time.ANSIC,
}

func ParseTime(text string) (t time.Time, err error) {
	for _, layout := range timeFormats {
		t, err = time.Parse(layout, text)
		if err == nil {
			return
		}
	}
	return
}
func (h Header) Get(key string) string {
	return textproto.MIMEHeader(h).Get(key)
}

func checkIfModifiedSince(w ResponseWriter, r *Request, modtime time.Time) condResult {
	if r.Method != "GET" && r.Method != "HEAD" {
		return condNone
	}
	ims := r.Header.Get("If-Modified-Since")
	if ims == "" || isZeroTime(modtime) {
		return condNone
	}
	t, err := ParseTime(ims)
	if err != nil {
		return condNone
	}
	// The Date-Modified header truncates sub-second precision, so
	// use mtime < t+1s instead of mtime <= t to check for unmodified.
	if modtime.Before(t.Add(1 * time.Second)) {
		return condFalse
	}
	return condTrue
}

func localRedirect(w ResponseWriter, r *Request, newPath string) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set("Location", newPath)
	w.WriteHeader(StatusMovedPermanently)
}

// all errors. We don't want to start leaking information in error messages.
func toHTTPError(err error) (msg string, httpStatus int) {
	if os.IsNotExist(err) {
		return "404 page not found", StatusNotFound
	}
	if os.IsPermission(err) {
		return "403 Forbidden", StatusForbidden
	}
	// Default:
	return "500 Internal Server Error", StatusInternalServerError
}

func Error(w ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}

func writeNotModified(w ResponseWriter) {
	// RFC 7232 section 4.1:
	// a sender SHOULD NOT generate representation metadata other than the
	// above listed fields unless said metadata exists for the purpose of
	// guiding cache updates (e.g., Last-Modified might be useful if the
	// response does not have an ETag field).
	h := w.Header()
	delete(h, "Content-Type")
	delete(h, "Content-Length")
	if h.Get("Etag") != "" {
		delete(h, "Last-Modified")
	}
	w.WriteHeader(StatusNotModified)
}

func dirList(w ResponseWriter, f File) {
	dirs, err := f.Readdir(-1)
	if err != nil {
		// TODO: log err.Error() to the Server.ErrorLog, once it's possible
		// for a handler to get at its Server via the ResponseWriter. See
		// Issue 12438.
		Error(w, "Error reading directory", StatusInternalServerError)
		return
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<pre>\n")
	for _, d := range dirs {
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		// name may contain '?' or '#', which must be escaped to remain
		// part of the URL path, and not indicate the start of a query
		// string or fragment.
		url := url.URL{Path: name}
		fmt.Fprintf(w, "<a href=\"%s\">%s</a>\n", url.String(), htmlReplacer.Replace(name))
	}
	fmt.Fprintf(w, "</pre>\n")
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
)

func setLastModified(w ResponseWriter, modtime time.Time) {
	if !isZeroTime(modtime) {
		w.Header().Set("Last-Modified", modtime.UTC().Format(TimeFormat))
	}
}

func scanETag(s string) (etag string, remain string) {
	s = textproto.TrimString(s)
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}
	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}
	// ETag is either W/"text" or "text".
	// See RFC 7232 2.3.
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		// Character values allowed in ETags.
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return string(s[:i+1]), s[i+1:]
		default:
			break
		}
	}
	return "", ""
}

func etagStrongMatch(a, b string) bool {
	return a == b && a != "" && a[0] == '"'
}

func checkIfMatch(w ResponseWriter, r *Request) condResult {
	im := r.Header.Get("If-Match")
	if im == "" {
		return condNone
	}
	for {
		im = textproto.TrimString(im)
		if len(im) == 0 {
			break
		}
		if im[0] == ',' {
			im = im[1:]
			continue
		}
		if im[0] == '*' {
			return condTrue
		}
		etag, remain := scanETag(im)
		if etag == "" {
			break
		}
		if etagStrongMatch(etag, w.Header().get("Etag")) {
			return condTrue
		}
		im = remain
	}

	return condFalse
}

func (h Header) get(key string) string {
	if v := h[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func checkIfUnmodifiedSince(w ResponseWriter, r *Request, modtime time.Time) condResult {
	ius := r.Header.Get("If-Unmodified-Since")
	if ius == "" || isZeroTime(modtime) {
		return condNone
	}
	if t, err := ParseTime(ius); err == nil {
		// The Date-Modified header truncates sub-second precision, so
		// use mtime < t+1s instead of mtime <= t to check for unmodified.
		if modtime.Before(t.Add(1 * time.Second)) {
			return condTrue
		}
		return condFalse
	}
	return condNone
}

func checkIfNoneMatch(w ResponseWriter, r *Request) condResult {
	inm := r.Header.get("If-None-Match")
	if inm == "" {
		return condNone
	}
	buf := inm
	for {
		buf = textproto.TrimString(buf)
		if len(buf) == 0 {
			break
		}
		if buf[0] == ',' {
			buf = buf[1:]
		}
		if buf[0] == '*' {
			return condFalse
		}
		etag, remain := scanETag(buf)
		if etag == "" {
			break
		}
		if etagWeakMatch(etag, w.Header().get("Etag")) {
			return condFalse
		}
		buf = remain
	}
	return condTrue
}

func etagWeakMatch(a, b string) bool {
	return strings.TrimPrefix(a, "W/") == strings.TrimPrefix(b, "W/")
}

func checkPreconditions(w ResponseWriter, r *Request, modtime time.Time) (done bool, rangeHeader string) {
	// This function carefully follows RFC 7232 section 6.
	ch := checkIfMatch(w, r)
	if ch == condNone {
		ch = checkIfUnmodifiedSince(w, r, modtime)
	}
	if ch == condFalse {
		w.WriteHeader(StatusPreconditionFailed)
		return true, ""
	}
	switch checkIfNoneMatch(w, r) {
	case condFalse:
		if r.Method == "GET" || r.Method == "HEAD" {
			writeNotModified(w)
			return true, ""
		} else {
			w.WriteHeader(StatusPreconditionFailed)
			return true, ""
		}
	case condNone:
		if checkIfModifiedSince(w, r, modtime) == condFalse {
			writeNotModified(w)
			return true, ""
		}
	}

	rangeHeader = r.Header.get("Range")
	if rangeHeader != "" {
		if checkIfRange(w, r, modtime) == condFalse {
			rangeHeader = ""
		}
	}
	return false, rangeHeader
}

func checkIfRange(w ResponseWriter, r *Request, modtime time.Time) condResult {
	if r.Method != "GET" {
		return condNone
	}
	ir := r.Header.get("If-Range")
	if ir == "" {
		return condNone
	}
	etag, _ := scanETag(ir)
	if etag != "" {
		if etagStrongMatch(etag, w.Header().Get("Etag")) {
			return condTrue
		} else {
			return condFalse
		}
	}
	// The If-Range value is typically the ETag value, but it may also be
	// the modtime date. See golang.org/issue/8367.
	if modtime.IsZero() {
		return condFalse
	}
	t, err := ParseTime(ir)
	if err != nil {
		return condFalse
	}
	if t.Unix() == modtime.Unix() {
		return condTrue
	}
	return condFalse
}

var unixEpochTime = time.Unix(0, 0)

func isZeroTime(t time.Time) bool {
	return t.IsZero() || t.Equal(unixEpochTime)
}

const sniffLen = 512

func isWS(b byte) bool {
	switch b {
	case '\t', '\n', '\x0c', '\r', ' ':
		return true
	}
	return false
}

type sniffSig interface {
	// match returns the MIME type of the data, or "" if unknown.
	match(data []byte, firstNonWS int) string
}

type htmlSig []byte

type maskedSig struct {
	mask, pat []byte
	skipWS    bool
	ct        string
}
type exactSig struct {
	sig []byte
	ct  string
}

var sniffSignatures = []sniffSig{
	htmlSig("<!DOCTYPE HTML"),
	htmlSig("<HTML"),
	htmlSig("<HEAD"),
	htmlSig("<SCRIPT"),
	htmlSig("<IFRAME"),
	htmlSig("<H1"),
	htmlSig("<DIV"),
	htmlSig("<FONT"),
	htmlSig("<TABLE"),
	htmlSig("<A"),
	htmlSig("<STYLE"),
	htmlSig("<TITLE"),
	htmlSig("<B"),
	htmlSig("<BODY"),
	htmlSig("<BR"),
	htmlSig("<P"),
	htmlSig("<!--"),

	&maskedSig{mask: []byte("\xFF\xFF\xFF\xFF\xFF"), pat: []byte("<?xml"), skipWS: true, ct: "text/xml; charset=utf-8"},

	&exactSig{[]byte("%PDF-"), "application/pdf"},
	&exactSig{[]byte("%!PS-Adobe-"), "application/postscript"},

	// UTF BOMs.
	&maskedSig{mask: []byte("\xFF\xFF\x00\x00"), pat: []byte("\xFE\xFF\x00\x00"), ct: "text/plain; charset=utf-16be"},
	&maskedSig{mask: []byte("\xFF\xFF\x00\x00"), pat: []byte("\xFF\xFE\x00\x00"), ct: "text/plain; charset=utf-16le"},
	&maskedSig{mask: []byte("\xFF\xFF\xFF\x00"), pat: []byte("\xEF\xBB\xBF\x00"), ct: "text/plain; charset=utf-8"},

	&exactSig{[]byte("GIF87a"), "image/gif"},
	&exactSig{[]byte("GIF89a"), "image/gif"},
	&exactSig{[]byte("\x89\x50\x4E\x47\x0D\x0A\x1A\x0A"), "image/png"},
	&exactSig{[]byte("\xFF\xD8\xFF"), "image/jpeg"},
	&exactSig{[]byte("BM"), "image/bmp"},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF\xFF\xFF"),
		pat:  []byte("RIFF\x00\x00\x00\x00WEBPVP"),
		ct:   "image/webp",
	},
	&exactSig{[]byte("\x00\x00\x01\x00"), "image/vnd.microsoft.icon"},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF"),
		pat:  []byte("RIFF\x00\x00\x00\x00WAVE"),
		ct:   "audio/wave",
	},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF"),
		pat:  []byte("FORM\x00\x00\x00\x00AIFF"),
		ct:   "audio/aiff",
	},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF"),
		pat:  []byte(".snd"),
		ct:   "audio/basic",
	},
	&maskedSig{
		mask: []byte("OggS\x00"),
		pat:  []byte("\x4F\x67\x67\x53\x00"),
		ct:   "application/ogg",
	},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF\xFF\xFF\xFF\xFF"),
		pat:  []byte("MThd\x00\x00\x00\x06"),
		ct:   "audio/midi",
	},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF"),
		pat:  []byte("ID3"),
		ct:   "audio/mpeg",
	},
	&maskedSig{
		mask: []byte("\xFF\xFF\xFF\xFF\x00\x00\x00\x00\xFF\xFF\xFF\xFF"),
		pat:  []byte("RIFF\x00\x00\x00\x00AVI "),
		ct:   "video/avi",
	},
	&exactSig{[]byte("\x1A\x45\xDF\xA3"), "video/webm"},
	&exactSig{[]byte("\x52\x61\x72\x20\x1A\x07\x00"), "application/x-rar-compressed"},
	&exactSig{[]byte("\x50\x4B\x03\x04"), "application/zip"},
	&exactSig{[]byte("\x1F\x8B\x08"), "application/x-gzip"},

	mp4Sig{},

	textSig{}, // should be last
}

type mp4Sig struct{}

type textSig struct{}

func DetectContentType(data []byte) string {
	if len(data) > sniffLen {
		data = data[:sniffLen]
	}

	// Index of the first non-whitespace byte in data.
	firstNonWS := 0
	for ; firstNonWS < len(data) && isWS(data[firstNonWS]); firstNonWS++ {
	}

	for _, sig := range sniffSignatures {
		if ct := sig.match(data, firstNonWS); ct != "" {
			return ct
		}
	}

	return "application/octet-stream" // fallback
}

var errNoOverlap = errors.New("invalid range: failed to overlap")

func sumRangesSize(ranges []httpRange) (size int64) {
	for _, ra := range ranges {
		size += ra.length
	}
	return
}

type httpRange struct {
	start, length int64
}

func (r httpRange) contentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.start+r.length-1, size)
}

func (r httpRange) mimeHeader(contentType string, size int64) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Range": {r.contentRange(size)},
		"Content-Type":  {contentType},
	}
}

func serveContent(w ResponseWriter, r *Request, name string, modtime time.Time, sizeFunc func() (int64, error), content io.ReadSeeker) {
	setLastModified(w, modtime)
	done, rangeReq := checkPreconditions(w, r, modtime)
	if done {
		return
	}

	code := StatusOK

	// If Content-Type isn't set, use the file's extension to find it, but
	// if the Content-Type is unset explicitly, do not sniff the type.
	ctypes, haveType := w.Header()["Content-Type"]
	var ctype string
	if !haveType {
		ctype = mime.TypeByExtension(filepath.Ext(name))
		if ctype == "" {
			// read a chunk to decide between utf-8 text and binary
			var buf [sniffLen]byte
			n, _ := io.ReadFull(content, buf[:])
			ctype = DetectContentType(buf[:n])
			_, err := content.Seek(0, io.SeekStart) // rewind to output whole file
			if err != nil {
				Error(w, "seeker can't seek", StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", ctype)
	} else if len(ctypes) > 0 {
		ctype = ctypes[0]
	}

	size, err := sizeFunc()
	if err != nil {
		Error(w, err.Error(), StatusInternalServerError)
		return
	}

	// handle Content-Range header.
	sendSize := size
	var sendContent io.Reader = content
	if size >= 0 {
		ranges, err := parseRange(rangeReq, size)
		if err != nil {
			if err == errNoOverlap {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			}
			Error(w, err.Error(), StatusRequestedRangeNotSatisfiable)
			return
		}
		if sumRangesSize(ranges) > size {
			// The total number of bytes in all the ranges
			// is larger than the size of the file by
			// itself, so this is probably an attack, or a
			// dumb client. Ignore the range request.
			ranges = nil
		}
		switch {
		case len(ranges) == 1:
			// RFC 2616, Section 14.16:
			// "When an HTTP message includes the content of a single
			// range (for example, a response to a request for a
			// single range, or to a request for a set of ranges
			// that overlap without any holes), this content is
			// transmitted with a Content-Range header, and a
			// Content-Length header showing the number of bytes
			// actually transferred.
			// ...
			// A response to a request for a single range MUST NOT
			// be sent using the multipart/byteranges media type."
			ra := ranges[0]
			if _, err := content.Seek(ra.start, io.SeekStart); err != nil {
				Error(w, err.Error(), StatusRequestedRangeNotSatisfiable)
				return
			}
			sendSize = ra.length
			code = StatusPartialContent
			w.Header().Set("Content-Range", ra.contentRange(size))
		case len(ranges) > 1:
			sendSize = rangesMIMESize(ranges, ctype, size)
			code = StatusPartialContent

			pr, pw := io.Pipe()
			mw := multipart.NewWriter(pw)
			w.Header().Set("Content-Type", "multipart/byteranges; boundary="+mw.Boundary())
			sendContent = pr
			defer pr.Close() // cause writing goroutine to fail and exit if CopyN doesn't finish.
			go func() {
				for _, ra := range ranges {
					part, err := mw.CreatePart(ra.mimeHeader(ctype, size))
					if err != nil {
						pw.CloseWithError(err)
						return
					}
					if _, err := content.Seek(ra.start, io.SeekStart); err != nil {
						pw.CloseWithError(err)
						return
					}
					if _, err := io.CopyN(part, content, ra.length); err != nil {
						pw.CloseWithError(err)
						return
					}
				}
				mw.Close()
				pw.Close()
			}()
		}

		w.Header().Set("Accept-Ranges", "bytes")
		if w.Header().Get("Content-Encoding") == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
		}
	}

	w.WriteHeader(code)

	if r.Method != "HEAD" {
		io.CopyN(w, sendContent, sendSize)
	}
}

func (h htmlSig) match(data []byte, firstNonWS int) string {
	data = data[firstNonWS:]
	if len(data) < len(h)+1 {
		return ""
	}
	for i, b := range h {
		db := data[i]
		if 'A' <= b && b <= 'Z' {
			db &= 0xDF
		}
		if b != db {
			return ""
		}
	}
	// Next byte must be space or right angle bracket.
	if db := data[len(h)]; db != ' ' && db != '>' {
		return ""
	}
	return "text/html; charset=utf-8"
}

func (m *maskedSig) match(data []byte, firstNonWS int) string {
	// pattern matching algorithm section 6
	// https://mimesniff.spec.whatwg.org/#pattern-matching-algorithm

	if m.skipWS {
		data = data[firstNonWS:]
	}
	if len(m.pat) != len(m.mask) {
		return ""
	}
	if len(data) < len(m.mask) {
		return ""
	}
	for i, mask := range m.mask {
		db := data[i] & mask
		if db != m.pat[i] {
			return ""
		}
	}
	return m.ct
}

var mp4ftype = []byte("ftyp")

var mp4 = []byte("mp4")

func (mp4Sig) match(data []byte, firstNonWS int) string {
	// https://mimesniff.spec.whatwg.org/#signature-for-mp4
	// c.f. section 6.2.1
	if len(data) < 12 {
		return ""
	}
	boxSize := int(binary.BigEndian.Uint32(data[:4]))
	if boxSize%4 != 0 || len(data) < boxSize {
		return ""
	}
	if !bytes.Equal(data[4:8], mp4ftype) {
		return ""
	}
	for st := 8; st < boxSize; st += 4 {
		if st == 12 {
			// minor version number
			continue
		}
		if bytes.Equal(data[st:st+3], mp4) {
			return "video/mp4"
		}
	}
	return ""
}

func (e *exactSig) match(data []byte, firstNonWS int) string {
	if bytes.HasPrefix(data, e.sig) {
		return e.ct
	}
	return ""
}

func (textSig) match(data []byte, firstNonWS int) string {
	// c.f. section 5, step 4.
	for _, b := range data[firstNonWS:] {
		switch {
		case b <= 0x08,
			b == 0x0B,
			0x0E <= b && b <= 0x1A,
			0x1C <= b && b <= 0x1F:
			return ""
		}
	}
	return "text/plain; charset=utf-8"
}

// parseRange parses a Range header string as per RFC 2616.
// errNoOverlap is returned if none of the ranges overlap.
func parseRange(s string, size int64) ([]httpRange, error) {
	if s == "" {
		return nil, nil // header not present
	}
	const b = "bytes="
	if !strings.HasPrefix(s, b) {
		return nil, errors.New("invalid range")
	}
	var ranges []httpRange
	noOverlap := false
	for _, ra := range strings.Split(s[len(b):], ",") {
		ra = strings.TrimSpace(ra)
		if ra == "" {
			continue
		}
		i := strings.Index(ra, "-")
		if i < 0 {
			return nil, errors.New("invalid range")
		}
		start, end := strings.TrimSpace(ra[:i]), strings.TrimSpace(ra[i+1:])
		var r httpRange
		if start == "" {
			// If no start is specified, end specifies the
			// range start relative to the end of the file.
			i, err := strconv.ParseInt(end, 10, 64)
			if err != nil {
				return nil, errors.New("invalid range")
			}
			if i > size {
				i = size
			}
			r.start = size - i
			r.length = size - r.start
		} else {
			i, err := strconv.ParseInt(start, 10, 64)
			if err != nil || i < 0 {
				return nil, errors.New("invalid range")
			}
			if i >= size {
				// If the range begins after the size of the content,
				// then it does not overlap.
				noOverlap = true
				continue
			}
			r.start = i
			if end == "" {
				// If no end is specified, range extends to end of the file.
				r.length = size - r.start
			} else {
				i, err := strconv.ParseInt(end, 10, 64)
				if err != nil || r.start > i {
					return nil, errors.New("invalid range")
				}
				if i >= size {
					i = size - 1
				}
				r.length = i - r.start + 1
			}
		}
		ranges = append(ranges, r)
	}
	if noOverlap && len(ranges) == 0 {
		// The specified ranges did not overlap with the content.
		return nil, errNoOverlap
	}
	return ranges, nil
}

func rangesMIMESize(ranges []httpRange, contentType string, contentSize int64) (encSize int64) {
	var w countingWriter
	mw := multipart.NewWriter(&w)
	for _, ra := range ranges {
		mw.CreatePart(ra.mimeHeader(contentType, contentSize))
		encSize += ra.length
	}
	mw.Close()
	encSize += int64(w)
	return
}

type countingWriter int64

func (w *countingWriter) Write(p []byte) (n int, err error) {
	*w += countingWriter(len(p))
	return len(p), nil
}

// Errors used by the HTTP server.
var (
	// ErrBodyNotAllowed is returned by ResponseWriter.Write calls
	// when the HTTP method or response code does not permit a
	// body.
	ErrBodyNotAllowed = errors.New("http: request method or response status code does not allow body")

	// ErrHijacked is returned by ResponseWriter.Write calls when
	// the underlying connection has been hijacked using the
	// Hijacker interface. A zero-byte write on a hijacked
	// connection will return ErrHijacked without any other side
	// effects.
	ErrHijacked = errors.New("http: connection has been hijacked")

	// ErrContentLength is returned by ResponseWriter.Write calls
	// when a Handler set a Content-Length response header with a
	// declared size and then attempted to write more bytes than
	// declared.
	ErrContentLength = errors.New("http: wrote more than the declared Content-Length")

	// Deprecated: ErrWriteAfterFlush is no longer used.
	ErrWriteAfterFlush = errors.New("unused")
)

// A Handler responds to an HTTP request.
//
// ServeHTTP should write reply headers and data to the ResponseWriter
// and then return. Returning signals that the request is finished; it
// is not valid to use the ResponseWriter or read from the
// Request.Body after or concurrently with the completion of the
// ServeHTTP call.
//
// Depending on the HTTP client software, HTTP protocol version, and
// any intermediaries between the client and the Go server, it may not
// be possible to read from the Request.Body after writing to the
// ResponseWriter. Cautious handlers should read the Request.Body
// first, and then reply.
//
// Except for reading the body, handlers should not modify the
// provided Request.
//
// If ServeHTTP panics, the server (the caller of ServeHTTP) assumes
// that the effect of the panic was isolated to the active request.
// It recovers the panic, logs a stack trace to the server error log,
// and hangs up the connection. To abort a handler so the client sees
// an interrupted response but the server doesn't log an error, panic
// with the value ErrAbortHandler.
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

// A ResponseWriter interface is used by an HTTP handler to
// construct an HTTP response.
//
// A ResponseWriter may not be used after the Handler.ServeHTTP method
// has returned.
type ResponseWriter interface {
	// Header returns the header map that will be sent by
	// WriteHeader. The Header map also is the mechanism with which
	// Handlers can set HTTP trailers.
	//
	// Changing the header map after a call to WriteHeader (or
	// Write) has no effect unless the modified headers are
	// trailers.
	//
	// There are two ways to set Trailers. The preferred way is to
	// predeclare in the headers which trailers you will later
	// send by setting the "Trailer" header to the names of the
	// trailer keys which will come later. In this case, those
	// keys of the Header map are treated as if they were
	// trailers. See the example. The second way, for trailer
	// keys not known to the Handler until after the first Write,
	// is to prefix the Header map keys with the TrailerPrefix
	// constant value. See TrailerPrefix.
	//
	// To suppress implicit response headers (such as "Date"), set
	// their value to nil.
	Header() Header

	// Write writes the data to the connection as part of an HTTP reply.
	//
	// If WriteHeader has not yet been called, Write calls
	// WriteHeader(http.StatusOK) before writing the data. If the Header
	// does not contain a Content-Type line, Write adds a Content-Type set
	// to the result of passing the initial 512 bytes of written data to
	// DetectContentType.
	//
	// Depending on the HTTP protocol version and the client, calling
	// Write or WriteHeader may prevent future reads on the
	// Request.Body. For HTTP/1.x requests, handlers should read any
	// needed request body data before writing the response. Once the
	// headers have been flushed (due to either an explicit Flusher.Flush
	// call or writing enough data to trigger a flush), the request body
	// may be unavailable. For HTTP/2 requests, the Go HTTP server permits
	// handlers to continue to read the request body while concurrently
	// writing the response. However, such behavior may not be supported
	// by all HTTP/2 clients. Handlers should read before writing if
	// possible to maximize compatibility.
	Write([]byte) (int, error)

	// WriteHeader sends an HTTP response header with status code.
	// If WriteHeader is not called explicitly, the first call to Write
	// will trigger an implicit WriteHeader(http.StatusOK).
	// Thus explicit calls to WriteHeader are mainly used to
	// send error codes.
	WriteHeader(int)
}

// The Flusher interface is implemented by ResponseWriters that allow
// an HTTP handler to flush buffered data to the client.
//
// The default HTTP/1.x and HTTP/2 ResponseWriter implementations
// support Flusher, but ResponseWriter wrappers may not. Handlers
// should always test for this ability at runtime.
//
// Note that even for ResponseWriters that support Flush,
// if the client is connected through an HTTP proxy,
// the buffered data may not reach the client until the response
// completes.
type Flusher interface {
	// Flush sends any buffered data to the client.
	Flush()
}

// The Hijacker interface is implemented by ResponseWriters that allow
// an HTTP handler to take over the connection.
//
// The default ResponseWriter for HTTP/1.x connections supports
// Hijacker, but HTTP/2 connections intentionally do not.
// ResponseWriter wrappers may also not support Hijacker. Handlers
// should always test for this ability at runtime.
type Hijacker interface {
	// Hijack lets the caller take over the connection.
	// After a call to Hijack the HTTP server library
	// will not do anything else with the connection.
	//
	// It becomes the caller's responsibility to manage
	// and close the connection.
	//
	// The returned net.Conn may have read or write deadlines
	// already set, depending on the configuration of the
	// Server. It is the caller's responsibility to set
	// or clear those deadlines as needed.
	//
	// The returned bufio.Reader may contain unprocessed buffered
	// data from the client.
	Hijack() (net.Conn, *bufio.ReadWriter, error)
}

// The CloseNotifier interface is implemented by ResponseWriters which
// allow detecting when the underlying connection has gone away.
//
// This mechanism can be used to cancel long operations on the server
// if the client has disconnected before the response is ready.
type CloseNotifier interface {
	// CloseNotify returns a channel that receives at most a
	// single value (true) when the client connection has gone
	// away.
	//
	// CloseNotify may wait to notify until Request.Body has been
	// fully read.
	//
	// After the Handler has returned, there is no guarantee
	// that the channel receives a value.
	//
	// If the protocol is HTTP/1.1 and CloseNotify is called while
	// processing an idempotent request (such a GET) while
	// HTTP/1.1 pipelining is in use, the arrival of a subsequent
	// pipelined request may cause a value to be sent on the
	// returned channel. In practice HTTP/1.1 pipelining is not
	// enabled in browsers and not seen often in the wild. If this
	// is a problem, use HTTP/2 or only use CloseNotify on methods
	// such as POST.
	CloseNotify() <-chan bool
}

var (
	// ServerContextKey is a context key. It can be used in HTTP
	// handlers with context.WithValue to access the server that
	// started the handler. The associated value will be of
	// type *Server.
	ServerContextKey = &contextKey{"http-server"}

	// LocalAddrContextKey is a context key. It can be used in
	// HTTP handlers with context.WithValue to access the address
	// the local address the connection arrived on.
	// The associated value will be of type net.Addr.
	LocalAddrContextKey = &contextKey{"local-addr"}
)

// A conn represents the server side of an HTTP connection.
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

	// lastMethod is the method of the most recent request
	// on this connection, if any.
	lastMethod string

	curReq atomic.Value // of *response (which has a Request in it)

	curState atomic.Value // of ConnState

	// mu guards hijackedv
	mu sync.Mutex

	// hijackedv is whether this connection has been hijacked
	// by a Handler with the Hijacker interface.
	// It is guarded by mu.
	hijackedv bool
}

func (c *conn) hijacked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hijackedv
}

// c.mu must be held.
func (c *conn) hijackLocked() (rwc net.Conn, buf *bufio.ReadWriter, err error) {
	if c.hijackedv {
		return nil, nil, ErrHijacked
	}
	c.r.abortPendingRead()

	c.hijackedv = true
	rwc = c.rwc
	rwc.SetDeadline(time.Time{})

	buf = bufio.NewReadWriter(c.bufr, bufio.NewWriter(rwc))
	if c.r.hasByte {
		if _, err := c.bufr.Peek(c.bufr.Buffered() + 1); err != nil {
			return nil, nil, fmt.Errorf("unexpected Peek failure reading buffered byte: %v", err)
		}
	}
	c.setState(rwc, StateHijacked)
	return
}

// This should be >= 512 bytes for DetectContentType,
// but otherwise it's somewhat arbitrary.
const bufferBeforeChunkingSize = 2048

// chunkWriter writes to a response's conn buffer, and is the writer
// wrapped by the response.bufw buffered writer.
//
// chunkWriter also is responsible for finalizing the Header, including
// conditionally setting the Content-Type and setting a Content-Length
// in cases where the handler's final output is smaller than the buffer
// size. It also conditionally adds chunk headers, when in chunking mode.
//
// See the comment above (*response).Write for the entire write flow.
type chunkWriter struct {
	res *response

	// header is either nil or a deep clone of res.handlerHeader
	// at the time of res.WriteHeader, if res.WriteHeader is
	// called and extra buffering is being done to calculate
	// Content-Type and/or Content-Length.
	header Header

	// wroteHeader tells whether the header's been written to "the
	// wire" (or rather: w.conn.buf). this is unlike
	// (*response).wroteHeader, which tells only whether it was
	// logically written.
	wroteHeader bool

	// set by the writeHeader method:
	chunking bool // using chunked transfer encoding for reply body
}

var (
	crlf       = []byte("\r\n")
	colonSpace = []byte(": ")
)

func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	if !cw.wroteHeader {
		cw.writeHeader(p)
	}
	if cw.res.req.Method == "HEAD" {
		// Eat writes.
		return len(p), nil
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
		bw := cw.res.conn.bufw // conn's bufio writer
		// zero chunk to mark EOF
		bw.WriteString("0\r\n")
		if trailers := cw.res.finalTrailers(); trailers != nil {
			trailers.Write(bw) // the writer handles noting errors
		}
		// final blank line after the trailers (whether
		// present or not)
		bw.WriteString("\r\n")
	}
}

// A response represents the server side of an HTTP response.
type response struct {
	conn             *conn
	req              *Request // request for this response
	reqBody          io.ReadCloser
	cancelCtx        context.CancelFunc // when ServeHTTP exits
	wroteHeader      bool               // reply header has been (logically) written
	wroteContinue    bool               // 100 Continue response was written
	wants10KeepAlive bool               // HTTP/1.0 w/ Connection "keep-alive"
	wantsClose       bool               // HTTP request has Connection "close"

	w  *bufio.Writer // buffers output in chunks to chunkWriter
	cw chunkWriter

	// handlerHeader is the Header that Handlers get access to,
	// which may be retained and mutated even after WriteHeader.
	// handlerHeader is copied into cw.header at WriteHeader
	// time, and privately mutated thereafter.
	handlerHeader Header
	calledHeader  bool // handler accessed handlerHeader via Header

	written       int64 // number of bytes written in body
	contentLength int64 // explicitly-declared Content-Length; or -1
	status        int   // status code passed to WriteHeader

	// close connection after this reply.  set on request and
	// updated after response from handler if there's a
	// "Connection: keep-alive" response header and a
	// Content-Length.
	closeAfterReply bool

	// requestBodyLimitHit is set by requestTooLarge when
	// maxBytesReader hits its max size. It is checked in
	// WriteHeader, to make sure we don't consume the
	// remaining request body to try to advance to the next HTTP
	// request. Instead, when this is set, we stop reading
	// subsequent requests on this connection and stop reading
	// input from it.
	requestBodyLimitHit bool

	// trailers are the headers to be sent after the handler
	// finishes writing the body. This field is initialized from
	// the Trailer response header when the response header is
	// written.
	trailers []string

	handlerDone atomicBool // set true when the handler exits

	// Buffers for Date and Content-Length
	dateBuf [len(TimeFormat)]byte
	clenBuf [10]byte

	// closeNotifyCh is the channel returned by CloseNotify.
	// TODO(bradfitz): this is currently (for Go 1.8) always
	// non-nil. Make this lazily-created again as it used to be?
	closeNotifyCh  chan bool
	didCloseNotify int32 // atomic (only 0->1 winner should send)
}

// TrailerPrefix is a magic prefix for ResponseWriter.Header map keys
// that, if present, signals that the map entry is actually for
// the response trailers, and not the response headers. The prefix
// is stripped after the ServeHTTP call finishes and the values are
// sent in the trailers.
//
// This mechanism is intended only for trailers that are not known
// prior to the headers being written. If the set of trailers is fixed
// or known before the header is written, the normal Go trailers mechanism
// is preferred:
//    https://golang.org/pkg/net/http/#ResponseWriter
//    https://golang.org/pkg/net/http/#example_ResponseWriter_trailers
const TrailerPrefix = "Trailer:"

// finalTrailers is called after the Handler exits and returns a non-nil
// value if the Handler set any trailers.
func (w *response) finalTrailers() Header {
	var t Header
	for k, vv := range w.handlerHeader {
		if strings.HasPrefix(k, TrailerPrefix) {
			if t == nil {
				t = make(Header)
			}
			t[strings.TrimPrefix(k, TrailerPrefix)] = vv
		}
	}
	for _, k := range w.trailers {
		if t == nil {
			t = make(Header)
		}
		for _, v := range w.handlerHeader[k] {
			t.Add(k, v)
		}
	}
	return t
}

type atomicBool int32

func (b *atomicBool) isSet() bool { return atomic.LoadInt32((*int32)(b)) != 0 }
func (b *atomicBool) setTrue()    { atomic.StoreInt32((*int32)(b), 1) }

// declareTrailer is called for each Trailer header when the
// response header is written. It notes that a header will need to be
// written in the trailers at the end of the response.
func (w *response) declareTrailer(k string) {
	k = CanonicalHeaderKey(k)
	switch k {
	case "Transfer-Encoding", "Content-Length", "Trailer":
		// Forbidden by RFC 2616 14.40.
		return
	}
	w.trailers = append(w.trailers, k)
}

// requestTooLarge is called by maxBytesReader when too much input has
// been read from the client.
func (w *response) requestTooLarge() {
	w.closeAfterReply = true
	w.requestBodyLimitHit = true
	if !w.wroteHeader {
		w.Header().Set("Connection", "close")
	}
}

// needsSniff reports whether a Content-Type still needs to be sniffed.
func (w *response) needsSniff() bool {
	_, haveType := w.handlerHeader["Content-Type"]
	return !w.cw.wroteHeader && !haveType && w.written < sniffLen
}

// writerOnly hides an io.Writer value's optional ReadFrom method
// from io.Copy.
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

// ReadFrom is here to optimize copying from an *os.File regular file
// to a *net.TCPConn with sendfile.
func (w *response) ReadFrom(src io.Reader) (n int64, err error) {
	// Our underlying w.conn.rwc is usually a *TCPConn (with its
	// own ReadFrom method). If not, or if our src isn't a regular
	// file, just fall back to the normal copy method.
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

	// sendfile path:

	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}

	if w.needsSniff() {
		n0, err := io.Copy(writerOnly{w}, io.LimitReader(src, sniffLen))
		n += n0
		if err != nil {
			return n, err
		}
	}

	w.w.Flush()  // get rid of any previous writes
	w.cw.flush() // make sure Header is written; flush data to rwc

	// Now that cw has been flushed, its chunking field is guaranteed initialized.
	if !w.cw.chunking && w.bodyAllowed() {
		n0, err := rf.ReadFrom(src)
		n += n0
		w.written += n0
		return n, err
	}

	n0, err := io.Copy(writerOnly{w}, src)
	n += n0
	return n, err
}

// debugServerConnections controls whether all server connections are wrapped
// with a verbose logging wrapper.
const debugServerConnections = false

// Create new connection from rwc.
func (srv *Server) newConn(rwc net.Conn) *conn {
	c := &conn{
		server: srv,
		rwc:    rwc,
	}
	if debugServerConnections {
		c.rwc = newLoggingConn("server", c.rwc)
	}
	return c
}

type readResult struct {
	n   int
	err error
	b   byte // byte read, if n == 1
}

// connReader is the io.Reader wrapper used by *conn. It combines a
// selectively-activated io.LimitedReader (to bound request header
// read sizes) with support for selectively keeping an io.Reader.Read
// call blocked in a background goroutine to wait for activity and
// trigger a CloseNotifier channel.
type connReader struct {
	conn *conn

	mu      sync.Mutex // guards following
	hasByte bool
	byteBuf [1]byte
	bgErr   error // non-nil means error happened on background read
	cond    *sync.Cond
	inRead  bool
	aborted bool  // set true before conn.rwc deadline is set to past
	remain  int64 // bytes remaining
}

func (cr *connReader) lock() {
	cr.mu.Lock()
	if cr.cond == nil {
		cr.cond = sync.NewCond(&cr.mu)
	}
}

func (cr *connReader) unlock() { cr.mu.Unlock() }

func (cr *connReader) startBackgroundRead() {
	cr.lock()
	defer cr.unlock()
	if cr.inRead {
		panic("invalid concurrent Body.Read call")
	}
	if cr.hasByte {
		return
	}
	cr.inRead = true
	cr.conn.rwc.SetReadDeadline(time.Time{})
	go cr.backgroundRead()
}

func (cr *connReader) backgroundRead() {
	n, err := cr.conn.rwc.Read(cr.byteBuf[:])
	cr.lock()
	if n == 1 {
		cr.hasByte = true
		// We were at EOF already (since we wouldn't be in a
		// background read otherwise), so this is a pipelined
		// HTTP request.
		cr.closeNotifyFromPipelinedRequest()
	}
	if ne, ok := err.(net.Error); ok && cr.aborted && ne.Timeout() {
		// Ignore this error. It's the expected error from
		// another goroutine calling abortPendingRead.
	} else if err != nil {
		cr.handleReadError(err)
	}
	cr.aborted = false
	cr.inRead = false
	cr.unlock()
	cr.cond.Broadcast()
}

func (cr *connReader) abortPendingRead() {
	cr.lock()
	defer cr.unlock()
	if !cr.inRead {
		return
	}
	cr.aborted = true
	cr.conn.rwc.SetReadDeadline(aLongTimeAgo)
	for cr.inRead {
		cr.cond.Wait()
	}
	cr.conn.rwc.SetReadDeadline(time.Time{})
}

func (cr *connReader) setReadLimit(remain int64) { cr.remain = remain }
func (cr *connReader) setInfiniteReadLimit()     { cr.remain = maxInt64 }
func (cr *connReader) hitReadLimit() bool        { return cr.remain <= 0 }

// may be called from multiple goroutines.
func (cr *connReader) handleReadError(err error) {
	cr.conn.cancelCtx()
	cr.closeNotify()
}

// closeNotifyFromPipelinedRequest simply calls closeNotify.
//
// This method wrapper is here for documentation. The callers are the
// cases where we send on the closenotify channel because of a
// pipelined HTTP request, per the previous Go behavior and
// documentation (that this "MAY" happen).
//
// TODO: consider changing this behavior and making context
// cancelation and closenotify work the same.
func (cr *connReader) closeNotifyFromPipelinedRequest() {
	cr.closeNotify()
}

// may be called from multiple goroutines.
func (cr *connReader) closeNotify() {
	res, _ := cr.conn.curReq.Load().(*response)
	if res != nil {
		if atomic.CompareAndSwapInt32(&res.didCloseNotify, 0, 1) {
			res.closeNotifyCh <- true
		}
	}
}

func (cr *connReader) Read(p []byte) (n int, err error) {
	cr.lock()
	if cr.inRead {
		cr.unlock()
		panic("invalid concurrent Body.Read call")
	}
	if cr.hitReadLimit() {
		cr.unlock()
		return 0, io.EOF
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
	if int64(len(p)) > cr.remain {
		p = p[:cr.remain]
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

	cr.lock()
	cr.inRead = false
	if err != nil {
		cr.handleReadError(err)
	}
	cr.remain -= int64(n)
	cr.unlock()

	cr.cond.Broadcast()
	return n, err
}

var (
	bufioReaderPool   sync.Pool
	bufioWriter2kPool sync.Pool
	bufioWriter4kPool sync.Pool
)

var copyBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

func bufioWriterPool(size int) *sync.Pool {
	switch size {
	case 2 << 10:
		return &bufioWriter2kPool
	case 4 << 10:
		return &bufioWriter4kPool
	}
	return nil
}

func newBufioReader(r io.Reader) *bufio.Reader {
	if v := bufioReaderPool.Get(); v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	// Note: if this reader size is ever changed, update
	// TestHandlerBodyClose's assumptions.
	return bufio.NewReader(r)
}

func putBufioReader(br *bufio.Reader) {
	br.Reset(nil)
	bufioReaderPool.Put(br)
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

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	if pool := bufioWriterPool(bw.Available()); pool != nil {
		pool.Put(bw)
	}
}

// DefaultMaxHeaderBytes is the maximum permitted size of the headers
// in an HTTP request.
// This can be overridden by setting Server.MaxHeaderBytes.
const DefaultMaxHeaderBytes = 1 << 20 // 1 MB

func (srv *Server) maxHeaderBytes() int {
	if srv.MaxHeaderBytes > 0 {
		return srv.MaxHeaderBytes
	}
	return DefaultMaxHeaderBytes
}

func (srv *Server) initialReadLimitSize() int64 {
	return int64(srv.maxHeaderBytes()) + 4096 // bufio slop
}

// wrapper around io.ReaderCloser which on first read, sends an
// HTTP/1.1 100 Continue header
type expectContinueReader struct {
	resp       *response
	readCloser io.ReadCloser
	closed     bool
	sawEOF     bool
}

func (ecr *expectContinueReader) Read(p []byte) (n int, err error) {
	if ecr.closed {
		return 0, ErrBodyReadAfterClose
	}
	if !ecr.resp.wroteContinue && !ecr.resp.conn.hijacked() {
		ecr.resp.wroteContinue = true
		ecr.resp.conn.bufw.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
		ecr.resp.conn.bufw.Flush()
	}
	n, err = ecr.readCloser.Read(p)
	if err == io.EOF {
		ecr.sawEOF = true
	}
	return
}

func (ecr *expectContinueReader) Close() error {
	ecr.closed = true
	return ecr.readCloser.Close()
}

// TimeFormat is the time format to use when generating times in HTTP
// headers. It is like time.RFC1123 but hard-codes GMT as the time
// zone. The time being formatted must be in UTC for Format to
// generate the correct format.
//
// For parsing this time format, see ParseTime.
const TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

// appendTime is a non-allocating version of []byte(t.UTC().Format(TimeFormat))
func appendTime(b []byte, t time.Time) []byte {
	const days = "SunMonTueWedThuFriSat"
	const months = "JanFebMarAprMayJunJulAugSepOctNovDec"

	t = t.UTC()
	yy, mm, dd := t.Date()
	hh, mn, ss := t.Clock()
	day := days[3*t.Weekday():]
	mon := months[3*(mm-1):]

	return append(b,
		day[0], day[1], day[2], ',', ' ',
		byte('0'+dd/10), byte('0'+dd%10), ' ',
		mon[0], mon[1], mon[2], ' ',
		byte('0'+yy/1000), byte('0'+(yy/100)%10), byte('0'+(yy/10)%10), byte('0'+yy%10), ' ',
		byte('0'+hh/10), byte('0'+hh%10), ':',
		byte('0'+mn/10), byte('0'+mn%10), ':',
		byte('0'+ss/10), byte('0'+ss%10), ' ',
		'G', 'M', 'T')
}

var errTooLarge = errors.New("http: request too large")

// Read next request from connection.
func (c *conn) readRequest(ctx context.Context) (w *response, err error) {
	if c.hijacked() {
		return nil, ErrHijacked
	}

	var (
		wholeReqDeadline time.Time // or zero if none
		hdrDeadline      time.Time // or zero if none
	)
	t0 := time.Now()
	if d := c.server.readHeaderTimeout(); d != 0 {
		hdrDeadline = t0.Add(d)
	}
	if d := c.server.ReadTimeout; d != 0 {
		wholeReqDeadline = t0.Add(d)
	}
	c.rwc.SetReadDeadline(hdrDeadline)
	if d := c.server.WriteTimeout; d != 0 {
		defer func() {
			c.rwc.SetWriteDeadline(time.Now().Add(d))
		}()
	}

	c.r.setReadLimit(c.server.initialReadLimitSize())
	if c.lastMethod == "POST" {
		// RFC 2616 section 4.1 tolerance for old buggy clients.
		peek, _ := c.bufr.Peek(4) // ReadRequest will get err below
		c.bufr.Discard(numLeadingCRorLF(peek))
	}
	req, err := readRequest(c.bufr, keepHostHeader)
	if err != nil {
		if c.r.hitReadLimit() {
			return nil, errTooLarge
		}
		return nil, err
	}

	if !http1ServerSupportsRequest(req) {
		return nil, badRequestError("unsupported protocol version")
	}

	c.lastMethod = req.Method
	c.r.setInfiniteReadLimit()

	hosts, haveHost := req.Header["Host"]
	isH2Upgrade := req.isH2Upgrade()
	if req.ProtoAtLeast(1, 1) && (!haveHost || len(hosts) == 0) && !isH2Upgrade {
		return nil, badRequestError("missing required Host header")
	}
	if len(hosts) > 1 {
		return nil, badRequestError("too many Host headers")
	}
	if len(hosts) == 1 && !httplex.ValidHostHeader(hosts[0]) {
		return nil, badRequestError("malformed Host header")
	}
	for k, vv := range req.Header {
		if !httplex.ValidHeaderFieldName(k) {
			return nil, badRequestError("invalid header name")
		}
		for _, v := range vv {
			if !httplex.ValidHeaderFieldValue(v) {
				return nil, badRequestError("invalid header value")
			}
		}
	}
	delete(req.Header, "Host")

	ctx, cancelCtx := context.WithCancel(ctx)
	req.ctx = ctx
	req.RemoteAddr = c.remoteAddr
	req.TLS = c.tlsState
	if body, ok := req.Body.(*body); ok {
		body.doEarlyClose = true
	}

	// Adjust the read deadline if necessary.
	if !hdrDeadline.Equal(wholeReqDeadline) {
		c.rwc.SetReadDeadline(wholeReqDeadline)
	}

	w = &response{
		conn:          c,
		cancelCtx:     cancelCtx,
		req:           req,
		reqBody:       req.Body,
		handlerHeader: make(Header),
		contentLength: -1,
		closeNotifyCh: make(chan bool, 1),

		// We populate these ahead of time so we're not
		// reading from req.Header after their Handler starts
		// and maybe mutates it (Issue 14940)
		wants10KeepAlive: req.wantsHttp10KeepAlive(),
		wantsClose:       req.wantsClose(),
	}
	if isH2Upgrade {
		w.closeAfterReply = true
	}
	w.cw.res = w
	w.w = newBufioWriterSize(&w.cw, bufferBeforeChunkingSize)
	return w, nil
}

// http1ServerSupportsRequest reports whether Go's HTTP/1.x server
// supports the given request.
func http1ServerSupportsRequest(req *Request) bool {
	if req.ProtoMajor == 1 {
		return true
	}
	// Accept "PRI * HTTP/2.0" upgrade requests, so Handlers can
	// wire up their own HTTP/2 upgrades.
	if req.ProtoMajor == 2 && req.ProtoMinor == 0 &&
		req.Method == "PRI" && req.RequestURI == "*" {
		return true
	}
	// Reject HTTP/0.x, and all other HTTP/2+ requests (which
	// aren't encoded in ASCII anyway).
	return false
}

func (w *response) Header() Header {
	if w.cw.header == nil && w.wroteHeader && !w.cw.wroteHeader {
		// Accessing the header between logically writing it
		// and physically writing it means we need to allocate
		// a clone to snapshot the logically written state.
		w.cw.header = w.handlerHeader.clone()
	}
	w.calledHeader = true
	return w.handlerHeader
}

// maxPostHandlerReadBytes is the max number of Request.Body bytes not
// consumed by a handler that the server will read from the client
// in order to keep a connection alive. If there are more bytes than
// this then the server to be paranoid instead sends a "Connection:
// close" response.
//
// This number is approximately what a typical machine's TCP buffer
// size is anyway.  (if we have the bytes on the machine, we might as
// well read them)
const maxPostHandlerReadBytes = 256 << 10

func (w *response) WriteHeader(code int) {
	if w.conn.hijacked() {
		w.conn.server.logf("http: response.WriteHeader on hijacked connection")
		return
	}
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

// extraHeader is the set of headers sometimes added by chunkWriter.writeHeader.
// This type is used to avoid extra allocations from cloning and/or populating
// the response Header map and all its 1-element slices.
type extraHeader struct {
	contentType      string
	connection       string
	transferEncoding string
	date             []byte // written if not nil
	contentLength    []byte // written if not nil
}

// Sorted the same as extraHeader.Write's loop.
var extraHeaderKeys = [][]byte{
	[]byte("Content-Type"),
	[]byte("Connection"),
	[]byte("Transfer-Encoding"),
}

var (
	headerContentLength = []byte("Content-Length: ")
	headerDate          = []byte("Date: ")
)

// Write writes the headers described in h to w.
//
// This method has a value receiver, despite the somewhat large size
// of h, because it prevents an allocation. The escape analysis isn't
// smart enough to realize this function doesn't mutate h.
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

// writeHeader finalizes the header sent to the client and writes it
// to cw.res.conn.bufw.
//
// p is not written by writeHeader, but is the first chunk of the body
// that will be written. It is sniffed for a Content-Type if none is
// set explicitly. It's also used to set the Content-Length, if the
// total body size was small and the handler has already finished
// running.
func (cw *chunkWriter) writeHeader(p []byte) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true

	w := cw.res
	keepAlivesEnabled := w.conn.server.doKeepAlives()
	isHEAD := w.req.Method == "HEAD"

	// header is written out to w.conn.buf below. Depending on the
	// state of the handler, we either own the map or not. If we
	// don't own it, the exclude map is created lazily for
	// WriteSubset to remove headers. The setHeader struct holds
	// headers we need to add.
	header := cw.header
	owned := header != nil
	if !owned {
		header = w.handlerHeader
	}
	var excludeHeader map[string]bool
	delHeader := func(key string) {
		if owned {
			header.Del(key)
			return
		}
		if _, ok := header[key]; !ok {
			return
		}
		if excludeHeader == nil {
			excludeHeader = make(map[string]bool)
		}
		excludeHeader[key] = true
	}
	var setHeader extraHeader

	// Don't write out the fake "Trailer:foo" keys. See TrailerPrefix.
	trailers := false
	for k := range cw.header {
		if strings.HasPrefix(k, TrailerPrefix) {
			if excludeHeader == nil {
				excludeHeader = make(map[string]bool)
			}
			excludeHeader[k] = true
			trailers = true
		}
	}
	for _, v := range cw.header["Trailer"] {
		trailers = true
		foreachHeaderElement(v, cw.res.declareTrailer)
	}

	te := header.get("Transfer-Encoding")
	hasTE := te != ""

	// If the handler is done but never sent a Content-Length
	// response header and this is our first (and last) write, set
	// it, even to zero. This helps HTTP/1.0 clients keep their
	// "keep-alive" connections alive.
	// Exceptions: 304/204/1xx responses never get Content-Length, and if
	// it was a HEAD request, we don't know the difference between
	// 0 actual bytes and 0 bytes because the handler noticed it
	// was a HEAD request and chose not to write anything. So for
	// HEAD, the handler should either write the Content-Length or
	// write non-zero bytes. If it's actually 0 bytes and the
	// handler never looked at the Request.Method, we just don't
	// send a Content-Length header.
	// Further, we don't send an automatic Content-Length if they
	// set a Transfer-Encoding, because they're generally incompatible.
	if w.handlerDone.isSet() && !trailers && !hasTE && bodyAllowedForStatus(w.status) && header.get("Content-Length") == "" && (!isHEAD || len(p) > 0) {
		w.contentLength = int64(len(p))
		setHeader.contentLength = strconv.AppendInt(cw.res.clenBuf[:0], int64(len(p)), 10)
	}

	// If this was an HTTP/1.0 request with keep-alive and we sent a
	// Content-Length back, we can make this a keep-alive response ...
	if w.wants10KeepAlive && keepAlivesEnabled {
		sentLength := header.get("Content-Length") != ""
		if sentLength && header.get("Connection") == "keep-alive" {
			w.closeAfterReply = false
		}
	}

	// Check for a explicit (and valid) Content-Length header.
	hasCL := w.contentLength != -1

	if w.wants10KeepAlive && (isHEAD || hasCL || !bodyAllowedForStatus(w.status)) {
		_, connectionHeaderSet := header["Connection"]
		if !connectionHeaderSet {
			setHeader.connection = "keep-alive"
		}
	} else if !w.req.ProtoAtLeast(1, 1) || w.wantsClose {
		w.closeAfterReply = true
	}

	if header.get("Connection") == "close" || !keepAlivesEnabled {
		w.closeAfterReply = true
	}

	// If the client wanted a 100-continue but we never sent it to
	// them (or, more strictly: we never finished reading their
	// request body), don't reuse this connection because it's now
	// in an unknown state: we might be sending this response at
	// the same time the client is now sending its request body
	// after a timeout.  (Some HTTP clients send Expect:
	// 100-continue but knowing that some servers don't support
	// it, the clients set a timer and send the body later anyway)
	// If we haven't seen EOF, we can't skip over the unread body
	// because we don't know if the next bytes on the wire will be
	// the body-following-the-timer or the subsequent request.
	// See Issue 11549.
	if ecr, ok := w.req.Body.(*expectContinueReader); ok && !ecr.sawEOF {
		w.closeAfterReply = true
	}

	// Per RFC 2616, we should consume the request body before
	// replying, if the handler hasn't already done so. But we
	// don't want to do an unbounded amount of reading here for
	// DoS reasons, so we only try up to a threshold.
	// TODO(bradfitz): where does RFC 2616 say that? See Issue 15527
	// about HTTP/1.x Handlers concurrently reading and writing, like
	// HTTP/2 handlers can do. Maybe this code should be relaxed?
	if w.req.ContentLength != 0 && !w.closeAfterReply {
		var discard, tooBig bool

		switch bdy := w.req.Body.(type) {
		case *expectContinueReader:
			if bdy.resp.wroteContinue {
				discard = true
			}
		case *body:
			bdy.mu.Lock()
			switch {
			case bdy.closed:
				if !bdy.sawEOF {
					// Body was closed in handler with non-EOF error.
					w.closeAfterReply = true
				}
			case bdy.unreadDataSizeLocked() >= maxPostHandlerReadBytes:
				tooBig = true
			default:
				discard = true
			}
			bdy.mu.Unlock()
		default:
			discard = true
		}

		if discard {
			_, err := io.CopyN(ioutil.Discard, w.reqBody, maxPostHandlerReadBytes+1)
			switch err {
			case nil:
				// There must be even more data left over.
				tooBig = true
			case ErrBodyReadAfterClose:
				// Body was already consumed and closed.
			case io.EOF:
				// The remaining body was just consumed, close it.
				err = w.reqBody.Close()
				if err != nil {
					w.closeAfterReply = true
				}
			default:
				// Some other kind of error occurred, like a read timeout, or
				// corrupt chunked encoding. In any case, whatever remains
				// on the wire must not be parsed as another HTTP request.
				w.closeAfterReply = true
			}
		}

		if tooBig {
			w.requestTooLarge()
			delHeader("Connection")
			setHeader.connection = "close"
		}
	}

	code := w.status
	if bodyAllowedForStatus(code) {
		// If no content type, apply sniffing algorithm to body.
		_, haveType := header["Content-Type"]
		if !haveType && !hasTE {
			setHeader.contentType = DetectContentType(p)
		}
	} else {
		for _, k := range suppressedHeaders(code) {
			delHeader(k)
		}
	}

	if _, ok := header["Date"]; !ok {
		setHeader.date = appendTime(cw.res.dateBuf[:0], time.Now())
	}

	if hasCL && hasTE && te != "identity" {
		// TODO: return an error if WriteHeader gets a return parameter
		// For now just ignore the Content-Length.
		w.conn.server.logf("http: WriteHeader called with both Transfer-Encoding of %q and a Content-Length of %d",
			te, w.contentLength)
		delHeader("Content-Length")
		hasCL = false
	}

	if w.req.Method == "HEAD" || !bodyAllowedForStatus(code) {
		// do nothing
	} else if code == StatusNoContent {
		delHeader("Transfer-Encoding")
	} else if hasCL {
		delHeader("Transfer-Encoding")
	} else if w.req.ProtoAtLeast(1, 1) {
		// HTTP/1.1 or greater: Transfer-Encoding has been set to identity,  and no
		// content-length has been provided. The connection must be closed after the
		// reply is written, and no chunking is to be done. This is the setup
		// recommended in the Server-Sent Events candidate recommendation 11,
		// section 8.
		if hasTE && te == "identity" {
			cw.chunking = false
			w.closeAfterReply = true
		} else {
			// HTTP/1.1 or greater: use chunked transfer encoding
			// to avoid closing the connection at EOF.
			cw.chunking = true
			setHeader.transferEncoding = "chunked"
			if hasTE && te == "chunked" {
				// We will send the chunked Transfer-Encoding header later.
				delHeader("Transfer-Encoding")
			}
		}
	} else {
		// HTTP version < 1.1: cannot do chunked transfer
		// encoding and we don't know the Content-Length so
		// signal EOF by closing connection.
		w.closeAfterReply = true
		delHeader("Transfer-Encoding") // in case already set
	}

	// Cannot use Content-Length with non-identity Transfer-Encoding.
	if cw.chunking {
		delHeader("Content-Length")
	}
	if !w.req.ProtoAtLeast(1, 0) {
		return
	}

	if w.closeAfterReply && (!keepAlivesEnabled || !hasToken(cw.header.get("Connection"), "close")) {
		delHeader("Connection")
		if w.req.ProtoAtLeast(1, 1) {
			setHeader.connection = "close"
		}
	}

	w.conn.bufw.WriteString(statusLine(w.req, code))
	cw.header.WriteSubset(w.conn.bufw, excludeHeader)
	setHeader.Write(w.conn.bufw)
	w.conn.bufw.Write(crlf)
}

// foreachHeaderElement splits v according to the "#rule" construction
// in RFC 2616 section 2.1 and calls fn for each non-empty element.
func foreachHeaderElement(v string, fn func(string)) {
	v = textproto.TrimString(v)
	if v == "" {
		return
	}
	if !strings.Contains(v, ",") {
		fn(v)
		return
	}
	for _, f := range strings.Split(v, ",") {
		if f = textproto.TrimString(f); f != "" {
			fn(f)
		}
	}
}

// statusLines is a cache of Status-Line strings, keyed by code (for
// HTTP/1.1) or negative code (for HTTP/1.0). This is faster than a
// map keyed by struct of two fields. This map's max size is bounded
// by 2*len(statusText), two protocol types for each known official
// status code in the statusText map.
var (
	statusMu    sync.RWMutex
	statusLines = make(map[int]string)
)

// statusLine returns a response Status-Line (RFC 2616 Section 6.1)
// for the given request and response status code.
func statusLine(req *Request, code int) string {
	// Fast path:
	key := code
	proto11 := req.ProtoAtLeast(1, 1)
	if !proto11 {
		key = -key
	}
	statusMu.RLock()
	line, ok := statusLines[key]
	statusMu.RUnlock()
	if ok {
		return line
	}

	// Slow path:
	proto := "HTTP/1.0"
	if proto11 {
		proto = "HTTP/1.1"
	}
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

// bodyAllowed reports whether a Write is allowed for this response type.
// It's illegal to call this before the header has been flushed.
func (w *response) bodyAllowed() bool {
	if !w.wroteHeader {
		panic("")
	}
	return bodyAllowedForStatus(w.status)
}

// The Life Of A Write is like this:
//
// Handler starts. No header has been sent. The handler can either
// write a header, or just start writing. Writing before sending a header
// sends an implicitly empty 200 OK header.
//
// If the handler didn't declare a Content-Length up front, we either
// go into chunking mode or, if the handler finishes running before
// the chunking buffer size, we compute a Content-Length and send that
// in the header instead.
//
// Likewise, if the handler didn't set a Content-Type, we sniff that
// from the initial chunk of output.
//
// The Writers are wired together like:
//
// 1. *response (the ResponseWriter) ->
// 2. (*response).w, a *bufio.Writer of bufferBeforeChunkingSize bytes
// 3. chunkWriter.Writer (whose writeHeader finalizes Content-Length/Type)
//    and which writes the chunk headers, if needed.
// 4. conn.buf, a bufio.Writer of default (4kB) bytes, writing to ->
// 5. checkConnErrorWriter{c}, which notes any non-nil error on Write
//    and populates c.werr with it if so. but otherwise writes to:
// 6. the rwc, the net.Conn.
//
// TODO(bradfitz): short-circuit some of the buffering when the
// initial header contains both a Content-Type and Content-Length.
// Also short-circuit in (1) when the header's been sent and not in
// chunking mode, writing directly to (4) instead, if (2) has no
// buffered data. More generally, we could short-circuit from (1) to
// (3) even in chunking mode if the write size from (1) is over some
// threshold and nothing is in (2).  The answer might be mostly making
// bufferBeforeChunkingSize smaller and having bufio's fast-paths deal
// with this instead.
func (w *response) Write(data []byte) (n int, err error) {
	return w.write(len(data), data, "")
}

func (w *response) WriteString(data string) (n int, err error) {
	return w.write(len(data), nil, data)
}

// either dataB or dataS is non-zero.
func (w *response) write(lenData int, dataB []byte, dataS string) (n int, err error) {
	if w.conn.hijacked() {
		if lenData > 0 {
			w.conn.server.logf("http: response.Write on hijacked connection")
		}
		return 0, ErrHijacked
	}
	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}
	if lenData == 0 {
		return 0, nil
	}
	if !w.bodyAllowed() {
		return 0, ErrBodyNotAllowed
	}

	w.written += int64(lenData) // ignoring errors, for errorKludge
	if w.contentLength != -1 && w.written > w.contentLength {
		return 0, ErrContentLength
	}
	if dataB != nil {
		return w.w.Write(dataB)
	} else {
		return w.w.WriteString(dataS)
	}
}

func (w *response) finishRequest() {
	w.handlerDone.setTrue()

	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}

	w.w.Flush()
	putBufioWriter(w.w)
	w.cw.close()
	w.conn.bufw.Flush()

	w.conn.r.abortPendingRead()

	// Close the body (regardless of w.closeAfterReply) so we can
	// re-use its bufio.Reader later safely.
	w.reqBody.Close()

	if w.req.MultipartForm != nil {
		w.req.MultipartForm.RemoveAll()
	}
}

// shouldReuseConnection reports whether the underlying TCP connection can be reused.
// It must only be called after the handler is done executing.
func (w *response) shouldReuseConnection() bool {
	if w.closeAfterReply {
		// The request or something set while executing the
		// handler indicated we shouldn't reuse this
		// connection.
		return false
	}

	if w.req.Method != "HEAD" && w.contentLength != -1 && w.bodyAllowed() && w.contentLength != w.written {
		// Did not write enough. Avoid getting out of sync.
		return false
	}

	// There was some error writing to the underlying connection
	// during the request, so don't re-use this conn.
	if w.conn.werr != nil {
		return false
	}

	if w.closedRequestBodyEarly() {
		return false
	}

	return true
}

func (w *response) closedRequestBodyEarly() bool {
	body, ok := w.req.Body.(*body)
	return ok && body.didEarlyClose()
}

func (w *response) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(StatusOK)
	}
	w.w.Flush()
	w.cw.flush()
}

func (c *conn) finalFlush() {
	if c.bufr != nil {
		// Steal the bufio.Reader (~4KB worth of memory) and its associated
		// reader for a future connection.
		putBufioReader(c.bufr)
		c.bufr = nil
	}

	if c.bufw != nil {
		c.bufw.Flush()
		// Steal the bufio.Writer (~4KB worth of memory) and its associated
		// writer for a future connection.
		putBufioWriter(c.bufw)
		c.bufw = nil
	}
}

// Close the connection.
func (c *conn) close() {
	c.finalFlush()
	c.rwc.Close()
}

// rstAvoidanceDelay is the amount of time we sleep after closing the
// write side of a TCP connection before closing the entire socket.
// By sleeping, we increase the chances that the client sees our FIN
// and processes its final data before they process the subsequent RST
// from closing a connection with known unread data.
// This RST seems to occur mostly on BSD systems. (And Windows?)
// This timeout is somewhat arbitrary (~latency around the planet).
const rstAvoidanceDelay = 500 * time.Millisecond

type closeWriter interface {
	CloseWrite() error
}

var _ closeWriter = (*net.TCPConn)(nil)

// closeWrite flushes any outstanding data and sends a FIN packet (if
// client is connected via TCP), signalling that we're done. We then
// pause for a bit, hoping the client processes it before any
// subsequent RST.
//
// See https://golang.org/issue/3595
func (c *conn) closeWriteAndWait() {
	c.finalFlush()
	if tcp, ok := c.rwc.(closeWriter); ok {
		tcp.CloseWrite()
	}
	time.Sleep(rstAvoidanceDelay)
}

// validNPN reports whether the proto is not a blacklisted Next
// Protocol Negotiation protocol. Empty and built-in protocol types
// are blacklisted and can't be overridden with alternate
// implementations.
func validNPN(proto string) bool {
	switch proto {
	case "", "http/1.1", "http/1.0":
		return false
	}
	return true
}

func (c *conn) setState(nc net.Conn, state ConnState) {
	srv := c.server
	switch state {
	case StateNew:
		srv.trackConn(c, true)
	case StateHijacked, StateClosed:
		srv.trackConn(c, false)
	}
	c.curState.Store(connStateInterface[state])
	if hook := srv.ConnState; hook != nil {
		hook(nc, state)
	}
}

// connStateInterface is an array of the interface{} versions of
// ConnState values, so we can use them in atomic.Values later without
// paying the cost of shoving their integers in an interface{}.
var connStateInterface = [...]interface{}{
	StateNew:      StateNew,
	StateActive:   StateActive,
	StateIdle:     StateIdle,
	StateHijacked: StateHijacked,
	StateClosed:   StateClosed,
}

// badRequestError is a literal string (used by in the server in HTML,
// unescaped) to tell the user why their request was bad. It should
// be plain text without user info or other embedded errors.
type badRequestError string

func (e badRequestError) Error() string { return "Bad Request: " + string(e) }

// ErrAbortHandler is a sentinel panic value to abort a handler.
// While any panic from ServeHTTP aborts the response to the client,
// panicking with ErrAbortHandler also suppresses logging of a stack
// trace to the server's error log.
var ErrAbortHandler = errors.New("net/http: abort Handler")

// isCommonNetReadError reports whether err is a common error
// encountered during reading a request off the network when the
// client has gone away or had its read fail somehow. This is used to
// determine which logs are interesting enough to log about.
func isCommonNetReadError(err error) bool {
	if err == io.EOF {
		return true
	}
	if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
		return true
	}
	if oe, ok := err.(*net.OpError); ok && oe.Op == "read" {
		return true
	}
	return false
}

// Serve a new connection.
func (c *conn) serve(ctx context.Context) {
	c.remoteAddr = c.rwc.RemoteAddr().String()
	defer func() {
		if err := recover(); err != nil && err != ErrAbortHandler {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			c.server.logf("http: panic serving %v: %v\n%s", c.remoteAddr, err, buf)
		}
		if !c.hijacked() {
			c.close()
			c.setState(c.rwc, StateClosed)
		}
	}()

	if tlsConn, ok := c.rwc.(*tls.Conn); ok {
		if d := c.server.ReadTimeout; d != 0 {
			c.rwc.SetReadDeadline(time.Now().Add(d))
		}
		if d := c.server.WriteTimeout; d != 0 {
			c.rwc.SetWriteDeadline(time.Now().Add(d))
		}
		if err := tlsConn.Handshake(); err != nil {
			c.server.logf("http: TLS handshake error from %s: %v", c.rwc.RemoteAddr(), err)
			return
		}
		c.tlsState = new(tls.ConnectionState)
		*c.tlsState = tlsConn.ConnectionState()
		if proto := c.tlsState.NegotiatedProtocol; validNPN(proto) {
			if fn := c.server.TLSNextProto[proto]; fn != nil {
				h := initNPNRequest{tlsConn, serverHandler{c.server}}
				fn(c.server, tlsConn, h)
			}
			return
		}
	}

	// HTTP/1.x from here on.

	ctx, cancelCtx := context.WithCancel(ctx)
	c.cancelCtx = cancelCtx
	defer cancelCtx()

	c.r = &connReader{conn: c}
	c.bufr = newBufioReader(c.r)
	c.bufw = newBufioWriterSize(checkConnErrorWriter{c}, 4<<10)

	for {
		w, err := c.readRequest(ctx)
		if c.r.remain != c.server.initialReadLimitSize() {
			// If we read any bytes off the wire, we're active.
			c.setState(c.rwc, StateActive)
		}
		if err != nil {
			const errorHeaders = "\r\nContent-Type: text/plain; charset=utf-8\r\nConnection: close\r\n\r\n"

			if err == errTooLarge {
				// Their HTTP client may or may not be
				// able to read this if we're
				// responding to them and hanging up
				// while they're still writing their
				// request. Undefined behavior.
				const publicErr = "431 Request Header Fields Too Large"
				fmt.Fprintf(c.rwc, "HTTP/1.1 "+publicErr+errorHeaders+publicErr)
				c.closeWriteAndWait()
				return
			}
			if isCommonNetReadError(err) {
				return // don't reply
			}

			publicErr := "400 Bad Request"
			if v, ok := err.(badRequestError); ok {
				publicErr = publicErr + ": " + string(v)
			}

			fmt.Fprintf(c.rwc, "HTTP/1.1 "+publicErr+errorHeaders+publicErr)
			return
		}

		// Expect 100 Continue support
		req := w.req
		if req.expectsContinue() {
			if req.ProtoAtLeast(1, 1) && req.ContentLength != 0 {
				// Wrap the Body reader with one that replies on the connection
				req.Body = &expectContinueReader{readCloser: req.Body, resp: w}
			}
		} else if req.Header.get("Expect") != "" {
			w.sendExpectationFailed()
			return
		}

		c.curReq.Store(w)

		if requestBodyRemains(req.Body) {
			registerOnHitEOF(req.Body, w.conn.r.startBackgroundRead)
		} else {
			if w.conn.bufr.Buffered() > 0 {
				w.conn.r.closeNotifyFromPipelinedRequest()
			}
			w.conn.r.startBackgroundRead()
		}

		// HTTP cannot have multiple simultaneous active requests.[*]
		// Until the server replies to this request, it can't read another,
		// so we might as well run the handler in this goroutine.
		// [*] Not strictly true: HTTP pipelining. We could let them all process
		// in parallel even if their responses need to be serialized.
		// But we're not going to implement HTTP pipelining because it
		// was never deployed in the wild and the answer is HTTP/2.
		serverHandler{c.server}.ServeHTTP(w, w.req)
		w.cancelCtx()
		if c.hijacked() {
			return
		}
		w.finishRequest()
		if !w.shouldReuseConnection() {
			if w.requestBodyLimitHit || w.closedRequestBodyEarly() {
				c.closeWriteAndWait()
			}
			return
		}
		c.setState(c.rwc, StateIdle)
		c.curReq.Store((*response)(nil))

		if !w.conn.server.doKeepAlives() {
			// We're in shutdown mode. We might've replied
			// to the user without "Connection: close" and
			// they might think they can send another
			// request, but such is life with HTTP/1.1.
			return
		}

		if d := c.server.idleTimeout(); d != 0 {
			c.rwc.SetReadDeadline(time.Now().Add(d))
			if _, err := c.bufr.Peek(4); err != nil {
				return
			}
		}
		c.rwc.SetReadDeadline(time.Time{})
	}
}

func (w *response) sendExpectationFailed() {
	// TODO(bradfitz): let ServeHTTP handlers handle
	// requests with non-standard expectation[s]? Seems
	// theoretical at best, and doesn't fit into the
	// current ServeHTTP model anyway. We'd need to
	// make the ResponseWriter an optional
	// "ExpectReplier" interface or something.
	//
	// For now we'll just obey RFC 2616 14.20 which says
	// "If a server receives a request containing an
	// Expect field that includes an expectation-
	// extension that it does not support, it MUST
	// respond with a 417 (Expectation Failed) status."
	w.Header().Set("Connection", "close")
	w.WriteHeader(StatusExpectationFailed)
	w.finishRequest()
}

// Hijack implements the Hijacker.Hijack method. Our response is both a ResponseWriter
// and a Hijacker.
func (w *response) Hijack() (rwc net.Conn, buf *bufio.ReadWriter, err error) {
	if w.handlerDone.isSet() {
		panic("net/http: Hijack called after ServeHTTP finished")
	}
	if w.wroteHeader {
		w.cw.flush()
	}

	c := w.conn
	c.mu.Lock()
	defer c.mu.Unlock()

	// Release the bufioWriter that writes to the chunk writer, it is not
	// used after a connection has been hijacked.
	rwc, buf, err = c.hijackLocked()
	if err == nil {
		putBufioWriter(w.w)
		w.w = nil
	}
	return rwc, buf, err
}

func (w *response) CloseNotify() <-chan bool {
	if w.handlerDone.isSet() {
		panic("net/http: CloseNotify called after ServeHTTP finished")
	}
	return w.closeNotifyCh
}

func registerOnHitEOF(rc io.ReadCloser, fn func()) {
	switch v := rc.(type) {
	case *expectContinueReader:
		registerOnHitEOF(v.readCloser, fn)
	case *body:
		v.registerOnHitEOF(fn)
	default:
		panic("unexpected type " + fmt.Sprintf("%T", rc))
	}
}

// requestBodyRemains reports whether future calls to Read
// on rc might yield more data.
func requestBodyRemains(rc io.ReadCloser) bool {
	if rc == NoBody {
		return false
	}
	switch v := rc.(type) {
	case *expectContinueReader:
		return requestBodyRemains(v.readCloser)
	case *body:
		return v.bodyRemains()
	default:
		panic("unexpected type " + fmt.Sprintf("%T", rc))
	}
}

// The HandlerFunc type is an adapter to allow the use of
// ordinary functions as HTTP handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// Handler that calls f.
type HandlerFunc func(ResponseWriter, *Request)

// ServeHTTP calls f(w, r).
func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) {
	f(w, r)
}

// Helper handlers

// Error replies to the request with the specified error message and HTTP code.
// It does not otherwise end the request; the caller should ensure no further
// writes are done to w.
// The error message should be plain text.
func Error(w ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}

// NotFound replies to the request with an HTTP 404 not found error.
func NotFound(w ResponseWriter, r *Request) { Error(w, "404 page not found", StatusNotFound) }

// NotFoundHandler returns a simple request handler
// that replies to each request with a ``404 page not found'' reply.
func NotFoundHandler() Handler { return HandlerFunc(NotFound) }

// StripPrefix returns a handler that serves HTTP requests
// by removing the given prefix from the request URL's Path
// and invoking the handler h. StripPrefix handles a
// request for a path that doesn't begin with prefix by
// replying with an HTTP 404 not found error.
func StripPrefix(prefix string, h Handler) Handler {
	if prefix == "" {
		return h
	}
	return HandlerFunc(func(w ResponseWriter, r *Request) {
		if p := strings.TrimPrefix(r.URL.Path, prefix); len(p) < len(r.URL.Path) {
			r.URL.Path = p
			h.ServeHTTP(w, r)
		} else {
			NotFound(w, r)
		}
	})
}

// Redirect replies to the request with a redirect to url,
// which may be a path relative to the request path.
//
// The provided code should be in the 3xx range and is usually
// StatusMovedPermanently, StatusFound or StatusSeeOther.
func Redirect(w ResponseWriter, r *Request, urlStr string, code int) {
	if u, err := url.Parse(urlStr); err == nil {
		// If url was relative, make absolute by
		// combining with request path.
		// The browser would probably do this for us,
		// but doing it ourselves is more reliable.

		// NOTE(rsc): RFC 2616 says that the Location
		// line must be an absolute URI, like
		// "http://www.google.com/redirect/",
		// not a path like "/redirect/".
		// Unfortunately, we don't know what to
		// put in the host name section to get the
		// client to connect to us again, so we can't
		// know the right absolute URI to send back.
		// Because of this problem, no one pays attention
		// to the RFC; they all send back just a new path.
		// So do we.
		if u.Scheme == "" && u.Host == "" {
			oldpath := r.URL.Path
			if oldpath == "" { // should not happen, but avoid a crash if it does
				oldpath = "/"
			}

			// no leading http://server
			if urlStr == "" || urlStr[0] != '/' {
				// make relative path absolute
				olddir, _ := path.Split(oldpath)
				urlStr = olddir + urlStr
			}

			var query string
			if i := strings.Index(urlStr, "?"); i != -1 {
				urlStr, query = urlStr[:i], urlStr[i:]
			}

			// clean up but preserve trailing slash
			trailing := strings.HasSuffix(urlStr, "/")
			urlStr = path.Clean(urlStr)
			if trailing && !strings.HasSuffix(urlStr, "/") {
				urlStr += "/"
			}
			urlStr += query
		}
	}

	w.Header().Set("Location", hexEscapeNonASCII(urlStr))
	w.WriteHeader(code)

	// RFC 2616 recommends that a short note "SHOULD" be included in the
	// response because older user agents may not understand 301/307.
	// Shouldn't send the response for POST or HEAD; that leaves GET.
	if r.Method == "GET" {
		note := "<a href=\"" + htmlEscape(urlStr) + "\">" + statusText[code] + "</a>.\n"
		fmt.Fprintln(w, note)
	}
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
)

func htmlEscape(s string) string {
	return htmlReplacer.Replace(s)
}

// Redirect to a fixed URL
type redirectHandler struct {
	url  string
	code int
}

func (rh *redirectHandler) ServeHTTP(w ResponseWriter, r *Request) {
	Redirect(w, r, rh.url, rh.code)
}

// RedirectHandler returns a request handler that redirects
// each request it receives to the given url using the given
// status code.
//
// The provided code should be in the 3xx range and is usually
// StatusMovedPermanently, StatusFound or StatusSeeOther.
func RedirectHandler(url string, code int) Handler {
	return &redirectHandler{url, code}
}

// ServeMux is an HTTP request multiplexer.
// It matches the URL of each incoming request against a list of registered
// patterns and calls the handler for the pattern that
// most closely matches the URL.
//
// Patterns name fixed, rooted paths, like "/favicon.ico",
// or rooted subtrees, like "/images/" (note the trailing slash).
// Longer patterns take precedence over shorter ones, so that
// if there are handlers registered for both "/images/"
// and "/images/thumbnails/", the latter handler will be
// called for paths beginning "/images/thumbnails/" and the
// former will receive requests for any other paths in the
// "/images/" subtree.
//
// Note that since a pattern ending in a slash names a rooted subtree,
// the pattern "/" matches all paths not matched by other registered
// patterns, not just the URL with Path == "/".
//
// If a subtree has been registered and a request is received naming the
// subtree root without its trailing slash, ServeMux redirects that
// request to the subtree root (adding the trailing slash). This behavior can
// be overridden with a separate registration for the path without
// the trailing slash. For example, registering "/images/" causes ServeMux
// to redirect a request for "/images" to "/images/", unless "/images" has
// been registered separately.
//
// Patterns may optionally begin with a host name, restricting matches to
// URLs on that host only. Host-specific patterns take precedence over
// general patterns, so that a handler might register for the two patterns
// "/codesearch" and "codesearch.google.com/" without also taking over
// requests for "http://www.google.com/".
//
// ServeMux also takes care of sanitizing the URL request path,
// redirecting any request containing . or .. elements or repeated slashes
// to an equivalent, cleaner URL.
type ServeMux struct {
	mu    sync.RWMutex
	m     map[string]muxEntry
	hosts bool // whether any patterns contain hostnames
}

type muxEntry struct {
	explicit bool
	h        Handler
	pattern  string
}

// NewServeMux allocates and returns a new ServeMux.
func NewServeMux() *ServeMux { return new(ServeMux) }

// DefaultServeMux is the default ServeMux used by Serve.
var DefaultServeMux = &defaultServeMux

var defaultServeMux ServeMux

// Does path match pattern?
func pathMatch(pattern, path string) bool {
	if len(pattern) == 0 {
		// should not happen
		return false
	}
	n := len(pattern)
	if pattern[n-1] != '/' {
		return pattern == path
	}
	return len(path) >= n && path[0:n] == pattern
}

// Return the canonical path for p, eliminating . and .. elements.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	// path.Clean removes trailing slash except for root;
	// put the trailing slash back if necessary.
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// Find a handler on a handler map given a path string
// Most-specific (longest) pattern wins
func (mux *ServeMux) match(path string) (h Handler, pattern string) {
	var n = 0
	for k, v := range mux.m {
		if !pathMatch(k, path) {
			continue
		}
		if h == nil || len(k) > n {
			n = len(k)
			h = v.h
			pattern = v.pattern
		}
	}
	return
}

// Handler returns the handler to use for the given request,
// consulting r.Method, r.Host, and r.URL.Path. It always returns
// a non-nil handler. If the path is not in its canonical form, the
// handler will be an internally-generated handler that redirects
// to the canonical path.
//
// Handler also returns the registered pattern that matches the
// request or, in the case of internally-generated redirects,
// the pattern that will match after following the redirect.
//
// If there is no registered handler that applies to the request,
// Handler returns a ``page not found'' handler and an empty pattern.
func (mux *ServeMux) Handler(r *Request) (h Handler, pattern string) {
	if r.Method != "CONNECT" {
		if p := cleanPath(r.URL.Path); p != r.URL.Path {
			_, pattern = mux.handler(r.Host, p)
			url := *r.URL
			url.Path = p
			return RedirectHandler(url.String(), StatusMovedPermanently), pattern
		}
	}

	return mux.handler(r.Host, r.URL.Path)
}

// handler is the main implementation of Handler.
// The path is known to be in canonical form, except for CONNECT methods.
func (mux *ServeMux) handler(host, path string) (h Handler, pattern string) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	// Host-specific pattern takes precedence over generic ones
	if mux.hosts {
		h, pattern = mux.match(host + path)
	}
	if h == nil {
		h, pattern = mux.match(path)
	}
	if h == nil {
		h, pattern = NotFoundHandler(), ""
	}
	return
}

// ServeHTTP dispatches the request to the handler whose
// pattern most closely matches the request URL.
func (mux *ServeMux) ServeHTTP(w ResponseWriter, r *Request) {
	if r.RequestURI == "*" {
		if r.ProtoAtLeast(1, 1) {
			w.Header().Set("Connection", "close")
		}
		w.WriteHeader(StatusBadRequest)
		return
	}
	h, _ := mux.Handler(r)
	h.ServeHTTP(w, r)
}

// Handle registers the handler for the given pattern.
// If a handler already exists for pattern, Handle panics.
func (mux *ServeMux) Handle(pattern string, handler Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if pattern == "" {
		panic("http: invalid pattern " + pattern)
	}
	if handler == nil {
		panic("http: nil handler")
	}
	if mux.m[pattern].explicit {
		panic("http: multiple registrations for " + pattern)
	}

	if mux.m == nil {
		mux.m = make(map[string]muxEntry)
	}
	mux.m[pattern] = muxEntry{explicit: true, h: handler, pattern: pattern}

	if pattern[0] != '/' {
		mux.hosts = true
	}

	// Helpful behavior:
	// If pattern is /tree/, insert an implicit permanent redirect for /tree.
	// It can be overridden by an explicit registration.
	n := len(pattern)
	if n > 0 && pattern[n-1] == '/' && !mux.m[pattern[0:n-1]].explicit {
		// If pattern contains a host name, strip it and use remaining
		// path for redirect.
		path := pattern
		if pattern[0] != '/' {
			// In pattern, at least the last character is a '/', so
			// strings.Index can't be -1.
			path = pattern[strings.Index(pattern, "/"):]
		}
		url := &url.URL{Path: path}
		mux.m[pattern[0:n-1]] = muxEntry{h: RedirectHandler(url.String(), StatusMovedPermanently), pattern: pattern}
	}
}

// HandleFunc registers the handler function for the given pattern.
func (mux *ServeMux) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	mux.Handle(pattern, HandlerFunc(handler))
}

// Handle registers the handler for the given pattern
// in the DefaultServeMux.
// The documentation for ServeMux explains how patterns are matched.
func Handle(pattern string, handler Handler) { DefaultServeMux.Handle(pattern, handler) }

// HandleFunc registers the handler function for the given pattern
// in the DefaultServeMux.
// The documentation for ServeMux explains how patterns are matched.
func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

// Serve accepts incoming HTTP connections on the listener l,
// creating a new service goroutine for each. The service goroutines
// read requests and then call handler to reply to them.
// Handler is typically nil, in which case the DefaultServeMux is used.
func Serve(l net.Listener, handler Handler) error {
	srv := &Server{Handler: handler}
	return srv.Serve(l)
}

// A Server defines parameters for running an HTTP server.
// The zero value for Server is a valid configuration.
type Server struct {
	Addr      string      // TCP address to listen on, ":http" if empty
	Handler   Handler     // handler to invoke, http.DefaultServeMux if nil
	TLSConfig *tls.Config // optional TLS config, used by ListenAndServeTLS

	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	//
	// Because ReadTimeout does not let Handlers make per-request
	// decisions on each request body's acceptable deadline or
	// upload rate, most users will prefer to use
	// ReadHeaderTimeout. It is valid to use them both.
	ReadTimeout time.Duration

	// ReadHeaderTimeout is the amount of time allowed to read
	// request headers. The connection's read deadline is reset
	// after reading the headers and the Handler can decide what
	// is considered too slow for the body.
	ReadHeaderTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset whenever a new
	// request's header is read. Like ReadTimeout, it does not
	// let Handlers make decisions on a per-request basis.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, there is no timeout.
	IdleTimeout time.Duration

	// MaxHeaderBytes controls the maximum number of bytes the
	// server will read parsing the request header's keys and
	// values, including the request line. It does not limit the
	// size of the request body.
	// If zero, DefaultMaxHeaderBytes is used.
	MaxHeaderBytes int

	// TLSNextProto optionally specifies a function to take over
	// ownership of the provided TLS connection when an NPN/ALPN
	// protocol upgrade has occurred. The map key is the protocol
	// name negotiated. The Handler argument should be used to
	// handle HTTP requests and will initialize the Request's TLS
	// and RemoteAddr if not already set. The connection is
	// automatically closed when the function returns.
	// If TLSNextProto is not nil, HTTP/2 support is not enabled
	// automatically.
	TLSNextProto map[string]func(*Server, *tls.Conn, Handler)

	// ConnState specifies an optional callback function that is
	// called when a client connection changes state. See the
	// ConnState type and associated constants for details.
	ConnState func(net.Conn, ConnState)

	// ErrorLog specifies an optional logger for errors accepting
	// connections and unexpected behavior from handlers.
	// If nil, logging goes to os.Stderr via the log package's
	// standard logger.
	ErrorLog *log.Logger

	disableKeepAlives int32     // accessed atomically.
	inShutdown        int32     // accessed atomically (non-zero means we're in Shutdown)
	nextProtoOnce     sync.Once // guards setupHTTP2_* init
	nextProtoErr      error     // result of http2.ConfigureServer if used

	mu         sync.Mutex
	listeners  map[net.Listener]struct{}
	activeConn map[*conn]struct{}
	doneChan   chan struct{}
}

func (s *Server) getDoneChan() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getDoneChanLocked()
}

func (s *Server) getDoneChanLocked() chan struct{} {
	if s.doneChan == nil {
		s.doneChan = make(chan struct{})
	}
	return s.doneChan
}

func (s *Server) closeDoneChanLocked() {
	ch := s.getDoneChanLocked()
	select {
	case <-ch:
		// Already closed. Don't close again.
	default:
		// Safe to close here. We're the only closer, guarded
		// by s.mu.
		close(ch)
	}
}

// Close immediately closes all active net.Listeners and any
// connections in state StateNew, StateActive, or StateIdle. For a
// graceful shutdown, use Shutdown.
//
// Close does not attempt to close (and does not even know about)
// any hijacked connections, such as WebSockets.
//
// Close returns any error returned from closing the Server's
// underlying Listener(s).
func (srv *Server) Close() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.closeDoneChanLocked()
	err := srv.closeListenersLocked()
	for c := range srv.activeConn {
		c.rwc.Close()
		delete(srv.activeConn, c)
	}
	return err
}

// shutdownPollInterval is how often we poll for quiescence
// during Server.Shutdown. This is lower during tests, to
// speed up tests.
// Ideally we could find a solution that doesn't involve polling,
// but which also doesn't have a high runtime cost (and doesn't
// involve any contentious mutexes), but that is left as an
// exercise for the reader.
var shutdownPollInterval = 500 * time.Millisecond

// Shutdown gracefully shuts down the server without interrupting any
// active connections. Shutdown works by first closing all open
// listeners, then closing all idle connections, and then waiting
// indefinitely for connections to return to idle and then shut down.
// If the provided context expires before the shutdown is complete,
// then the context's error is returned.
//
// Shutdown does not attempt to close nor wait for hijacked
// connections such as WebSockets. The caller of Shutdown should
// separately notify such long-lived connections of shutdown and wait
// for them to close, if desired.
func (srv *Server) Shutdown(ctx context.Context) error {
	atomic.AddInt32(&srv.inShutdown, 1)
	defer atomic.AddInt32(&srv.inShutdown, -1)

	srv.mu.Lock()
	lnerr := srv.closeListenersLocked()
	srv.closeDoneChanLocked()
	srv.mu.Unlock()

	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	for {
		if srv.closeIdleConns() {
			return lnerr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// closeIdleConns closes all idle connections and reports whether the
// server is quiescent.
func (s *Server) closeIdleConns() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	quiescent := true
	for c := range s.activeConn {
		st, ok := c.curState.Load().(ConnState)
		if !ok || st != StateIdle {
			quiescent = false
			continue
		}
		c.rwc.Close()
		delete(s.activeConn, c)
	}
	return quiescent
}

func (s *Server) closeListenersLocked() error {
	var err error
	for ln := range s.listeners {
		if cerr := ln.Close(); cerr != nil && err == nil {
			err = cerr
		}
		delete(s.listeners, ln)
	}
	return err
}

// A ConnState represents the state of a client connection to a server.
// It's used by the optional Server.ConnState hook.
type ConnState int

const (
	// StateNew represents a new connection that is expected to
	// send a request immediately. Connections begin at this
	// state and then transition to either StateActive or
	// StateClosed.
	StateNew ConnState = iota

	// StateActive represents a connection that has read 1 or more
	// bytes of a request. The Server.ConnState hook for
	// StateActive fires before the request has entered a handler
	// and doesn't fire again until the request has been
	// handled. After the request is handled, the state
	// transitions to StateClosed, StateHijacked, or StateIdle.
	// For HTTP/2, StateActive fires on the transition from zero
	// to one active request, and only transitions away once all
	// active requests are complete. That means that ConnState
	// cannot be used to do per-request work; ConnState only notes
	// the overall state of the connection.
	StateActive

	// StateIdle represents a connection that has finished
	// handling a request and is in the keep-alive state, waiting
	// for a new request. Connections transition from StateIdle
	// to either StateActive or StateClosed.
	StateIdle

	// StateHijacked represents a hijacked connection.
	// This is a terminal state. It does not transition to StateClosed.
	StateHijacked

	// StateClosed represents a closed connection.
	// This is a terminal state. Hijacked connections do not
	// transition to StateClosed.
	StateClosed
)

var stateName = map[ConnState]string{
	StateNew:      "new",
	StateActive:   "active",
	StateIdle:     "idle",
	StateHijacked: "hijacked",
	StateClosed:   "closed",
}

func (c ConnState) String() string {
	return stateName[c]
}

// serverHandler delegates to either the server's Handler or
// DefaultServeMux and also handles "OPTIONS *" requests.
type serverHandler struct {
	srv *Server
}

func (sh serverHandler) ServeHTTP(rw ResponseWriter, req *Request) {
	handler := sh.srv.Handler
	if handler == nil {
		handler = DefaultServeMux
	}
	if req.RequestURI == "*" && req.Method == "OPTIONS" {
		handler = globalOptionsHandler{}
	}
	handler.ServeHTTP(rw, req)
}

// ListenAndServe listens on the TCP network address srv.Addr and then
// calls Serve to handle requests on incoming connections.
// Accepted connections are configured to enable TCP keep-alives.
// If srv.Addr is blank, ":http" is used.
// ListenAndServe always returns a non-nil error.
func (srv *Server) ListenAndServe() error {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
}

var testHookServerServe func(*Server, net.Listener) // used if non-nil

// shouldDoServeHTTP2 reports whether Server.Serve should configure
// automatic HTTP/2. (which sets up the srv.TLSNextProto map)
func (srv *Server) shouldConfigureHTTP2ForServe() bool {
	if srv.TLSConfig == nil {
		// Compatibility with Go 1.6:
		// If there's no TLSConfig, it's possible that the user just
		// didn't set it on the http.Server, but did pass it to
		// tls.NewListener and passed that listener to Serve.
		// So we should configure HTTP/2 (to set up srv.TLSNextProto)
		// in case the listener returns an "h2" *tls.Conn.
		return true
	}
	// The user specified a TLSConfig on their http.Server.
	// In this, case, only configure HTTP/2 if their tls.Config
	// explicitly mentions "h2". Otherwise http2.ConfigureServer
	// would modify the tls.Config to add it, but they probably already
	// passed this tls.Config to tls.NewListener. And if they did,
	// it's too late anyway to fix it. It would only be potentially racy.
	// See Issue 15908.
	return strSliceContains(srv.TLSConfig.NextProtos, http2NextProtoTLS)
}

var ErrServerClosed = errors.New("http: Server closed")

// Serve accepts incoming connections on the Listener l, creating a
// new service goroutine for each. The service goroutines read requests and
// then call srv.Handler to reply to them.
//
// For HTTP/2 support, srv.TLSConfig should be initialized to the
// provided listener's TLS Config before calling Serve. If
// srv.TLSConfig is non-nil and doesn't include the string "h2" in
// Config.NextProtos, HTTP/2 support is not enabled.
//
// Serve always returns a non-nil error. After Shutdown or Close, the
// returned error is ErrServerClosed.
func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()
	if fn := testHookServerServe; fn != nil {
		fn(srv, l)
	}
	var tempDelay time.Duration // how long to sleep on accept failure

	if err := srv.setupHTTP2_Serve(); err != nil {
		return err
	}

	srv.trackListener(l, true)
	defer srv.trackListener(l, false)

	baseCtx := context.Background() // base is always background, per Issue 16220
	ctx := context.WithValue(baseCtx, ServerContextKey, srv)
	ctx = context.WithValue(ctx, LocalAddrContextKey, l.Addr())
	for {
		rw, e := l.Accept()
		if e != nil {
			select {
			case <-srv.getDoneChan():
				return ErrServerClosed
			default:
			}
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				srv.logf("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c := srv.newConn(rw)
		c.setState(c.rwc, StateNew) // before Serve can return
		go c.serve(ctx)
	}
}

func (s *Server) trackListener(ln net.Listener, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listeners == nil {
		s.listeners = make(map[net.Listener]struct{})
	}
	if add {
		// If the *Server is being reused after a previous
		// Close or Shutdown, reset its doneChan:
		if len(s.listeners) == 0 && len(s.activeConn) == 0 {
			s.doneChan = nil
		}
		s.listeners[ln] = struct{}{}
	} else {
		delete(s.listeners, ln)
	}
}

func (s *Server) trackConn(c *conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeConn == nil {
		s.activeConn = make(map[*conn]struct{})
	}
	if add {
		s.activeConn[c] = struct{}{}
	} else {
		delete(s.activeConn, c)
	}
}

func (s *Server) idleTimeout() time.Duration {
	if s.IdleTimeout != 0 {
		return s.IdleTimeout
	}
	return s.ReadTimeout
}

func (s *Server) readHeaderTimeout() time.Duration {
	if s.ReadHeaderTimeout != 0 {
		return s.ReadHeaderTimeout
	}
	return s.ReadTimeout
}

func (s *Server) doKeepAlives() bool {
	return atomic.LoadInt32(&s.disableKeepAlives) == 0 && !s.shuttingDown()
}

func (s *Server) shuttingDown() bool {
	return atomic.LoadInt32(&s.inShutdown) != 0
}

// SetKeepAlivesEnabled controls whether HTTP keep-alives are enabled.
// By default, keep-alives are always enabled. Only very
// resource-constrained environments or servers in the process of
// shutting down should disable them.
func (srv *Server) SetKeepAlivesEnabled(v bool) {
	if v {
		atomic.StoreInt32(&srv.disableKeepAlives, 0)
		return
	}
	atomic.StoreInt32(&srv.disableKeepAlives, 1)

	// Close idle HTTP/1 conns:
	srv.closeIdleConns()

	// Close HTTP/2 conns, as soon as they become idle, but reset
	// the chan so future conns (if the listener is still active)
	// still work and don't get a GOAWAY immediately, before their
	// first request:
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.closeDoneChanLocked() // closes http2 conns
	srv.doneChan = nil
}

func (s *Server) logf(format string, args ...interface{}) {
	if s.ErrorLog != nil {
		s.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// ListenAndServe listens on the TCP network address addr
// and then calls Serve with handler to handle requests
// on incoming connections.
// Accepted connections are configured to enable TCP keep-alives.
// Handler is typically nil, in which case the DefaultServeMux is
// used.
//
// A trivial example server is:
//
//	package main
//
//	import (
//		"io"
//		"net/http"
//		"log"
//	)
//
//	// hello world, the web server
//	func HelloServer(w http.ResponseWriter, req *http.Request) {
//		io.WriteString(w, "hello, world!\n")
//	}
//
//	func main() {
//		http.HandleFunc("/hello", HelloServer)
//		log.Fatal(http.ListenAndServe(":12345", nil))
//	}
//
// ListenAndServe always returns a non-nil error.
func ListenAndServe(addr string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

// ListenAndServeTLS acts identically to ListenAndServe, except that it
// expects HTTPS connections. Additionally, files containing a certificate and
// matching private key for the server must be provided. If the certificate
// is signed by a certificate authority, the certFile should be the concatenation
// of the server's certificate, any intermediates, and the CA's certificate.
//
// A trivial example server is:
//
//	import (
//		"log"
//		"net/http"
//	)
//
//	func handler(w http.ResponseWriter, req *http.Request) {
//		w.Header().Set("Content-Type", "text/plain")
//		w.Write([]byte("This is an example server.\n"))
//	}
//
//	func main() {
//		http.HandleFunc("/", handler)
//		log.Printf("About to listen on 10443. Go to https://127.0.0.1:10443/")
//		err := http.ListenAndServeTLS(":10443", "cert.pem", "key.pem", nil)
//		log.Fatal(err)
//	}
//
// One can use generate_cert.go in crypto/tls to generate cert.pem and key.pem.
//
// ListenAndServeTLS always returns a non-nil error.
func ListenAndServeTLS(addr, certFile, keyFile string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServeTLS(certFile, keyFile)
}

// ListenAndServeTLS listens on the TCP network address srv.Addr and
// then calls Serve to handle requests on incoming TLS connections.
// Accepted connections are configured to enable TCP keep-alives.
//
// Filenames containing a certificate and matching private key for the
// server must be provided if neither the Server's TLSConfig.Certificates
// nor TLSConfig.GetCertificate are populated. If the certificate is
// signed by a certificate authority, the certFile should be the
// concatenation of the server's certificate, any intermediates, and
// the CA's certificate.
//
// If srv.Addr is blank, ":https" is used.
//
// ListenAndServeTLS always returns a non-nil error.
func (srv *Server) ListenAndServeTLS(certFile, keyFile string) error {
	addr := srv.Addr
	if addr == "" {
		addr = ":https"
	}

	// Setup HTTP/2 before srv.Serve, to initialize srv.TLSConfig
	// before we clone it and create the TLS Listener.
	if err := srv.setupHTTP2_ListenAndServeTLS(); err != nil {
		return err
	}

	config := cloneTLSConfig(srv.TLSConfig)
	if !strSliceContains(config.NextProtos, "http/1.1") {
		config.NextProtos = append(config.NextProtos, "http/1.1")
	}

	configHasCert := len(config.Certificates) > 0 || config.GetCertificate != nil
	if !configHasCert || certFile != "" || keyFile != "" {
		var err error
		config.Certificates = make([]tls.Certificate, 1)
		config.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return err
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	tlsListener := tls.NewListener(tcpKeepAliveListener{ln.(*net.TCPListener)}, config)
	return srv.Serve(tlsListener)
}

// setupHTTP2_ListenAndServeTLS conditionally configures HTTP/2 on
// srv and returns whether there was an error setting it up. If it is
// not configured for policy reasons, nil is returned.
func (srv *Server) setupHTTP2_ListenAndServeTLS() error {
	srv.nextProtoOnce.Do(srv.onceSetNextProtoDefaults)
	return srv.nextProtoErr
}

// setupHTTP2_Serve is called from (*Server).Serve and conditionally
// configures HTTP/2 on srv using a more conservative policy than
// setupHTTP2_ListenAndServeTLS because Serve may be called
// concurrently.
//
// The tests named TestTransportAutomaticHTTP2* and
// TestConcurrentServerServe in server_test.go demonstrate some
// of the supported use cases and motivations.
func (srv *Server) setupHTTP2_Serve() error {
	srv.nextProtoOnce.Do(srv.onceSetNextProtoDefaults_Serve)
	return srv.nextProtoErr
}

func (srv *Server) onceSetNextProtoDefaults_Serve() {
	if srv.shouldConfigureHTTP2ForServe() {
		srv.onceSetNextProtoDefaults()
	}
}

// onceSetNextProtoDefaults configures HTTP/2, if the user hasn't
// configured otherwise. (by setting srv.TLSNextProto non-nil)
// It must only be called via srv.nextProtoOnce (use srv.setupHTTP2_*).
func (srv *Server) onceSetNextProtoDefaults() {
	if strings.Contains(os.Getenv("GODEBUG"), "http2server=0") {
		return
	}
	// Enable HTTP/2 by default if the user hasn't otherwise
	// configured their TLSNextProto map.
	if srv.TLSNextProto == nil {
		srv.nextProtoErr = http2ConfigureServer(srv, nil)
	}
}

// TimeoutHandler returns a Handler that runs h with the given time limit.
//
// The new Handler calls h.ServeHTTP to handle each request, but if a
// call runs for longer than its time limit, the handler responds with
// a 503 Service Unavailable error and the given message in its body.
// (If msg is empty, a suitable default message will be sent.)
// After such a timeout, writes by h to its ResponseWriter will return
// ErrHandlerTimeout.
//
// TimeoutHandler buffers all Handler writes to memory and does not
// support the Hijacker or Flusher interfaces.
func TimeoutHandler(h Handler, dt time.Duration, msg string) Handler {
	return &timeoutHandler{
		handler: h,
		body:    msg,
		dt:      dt,
	}
}

// ErrHandlerTimeout is returned on ResponseWriter Write calls
// in handlers which have timed out.
var ErrHandlerTimeout = errors.New("http: Handler timeout")

type timeoutHandler struct {
	handler Handler
	body    string
	dt      time.Duration

	// When set, no timer will be created and this channel will
	// be used instead.
	testTimeout <-chan time.Time
}

func (h *timeoutHandler) errorBody() string {
	if h.body != "" {
		return h.body
	}
	return "<html><head><title>Timeout</title></head><body><h1>Timeout</h1></body></html>"
}

func (h *timeoutHandler) ServeHTTP(w ResponseWriter, r *Request) {
	var t *time.Timer
	timeout := h.testTimeout
	if timeout == nil {
		t = time.NewTimer(h.dt)
		timeout = t.C
	}
	done := make(chan struct{})
	tw := &timeoutWriter{
		w: w,
		h: make(Header),
	}
	go func() {
		h.handler.ServeHTTP(tw, r)
		close(done)
	}()
	select {
	case <-done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		dst := w.Header()
		for k, vv := range tw.h {
			dst[k] = vv
		}
		if !tw.wroteHeader {
			tw.code = StatusOK
		}
		w.WriteHeader(tw.code)
		w.Write(tw.wbuf.Bytes())
		if t != nil {
			t.Stop()
		}
	case <-timeout:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		w.WriteHeader(StatusServiceUnavailable)
		io.WriteString(w, h.errorBody())
		tw.timedOut = true
		return
	}
}

type timeoutWriter struct {
	w    ResponseWriter
	h    Header
	wbuf bytes.Buffer

	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

func (tw *timeoutWriter) Header() Header { return tw.h }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, ErrHandlerTimeout
	}
	if !tw.wroteHeader {
		tw.writeHeader(StatusOK)
	}
	return tw.wbuf.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.wroteHeader {
		return
	}
	tw.writeHeader(code)
}

func (tw *timeoutWriter) writeHeader(code int) {
	tw.wroteHeader = true
	tw.code = code
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
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

// globalOptionsHandler responds to "OPTIONS *" requests.
type globalOptionsHandler struct{}

func (globalOptionsHandler) ServeHTTP(w ResponseWriter, r *Request) {
	w.Header().Set("Content-Length", "0")
	if r.ContentLength != 0 {
		// Read up to 4KB of OPTIONS body (as mentioned in the
		// spec as being reserved for future use), but anything
		// over that is considered a waste of server resources
		// (or an attack) and we abort and close the connection,
		// courtesy of MaxBytesReader's EOF behavior.
		mb := MaxBytesReader(w, r.Body, 4<<10)
		io.Copy(ioutil.Discard, mb)
	}
}

// initNPNRequest is an HTTP handler that initializes certain
// uninitialized fields in its *Request. Such partially-initialized
// Requests come from NPN protocol handlers.
type initNPNRequest struct {
	c *tls.Conn
	h serverHandler
}

func (h initNPNRequest) ServeHTTP(rw ResponseWriter, req *Request) {
	if req.TLS == nil {
		req.TLS = &tls.ConnectionState{}
		*req.TLS = h.c.ConnectionState()
	}
	if req.Body == nil {
		req.Body = NoBody
	}
	if req.RemoteAddr == "" {
		req.RemoteAddr = h.c.RemoteAddr().String()
	}
	h.h.ServeHTTP(rw, req)
}

// loggingConn is used for debugging.
type loggingConn struct {
	name string
	net.Conn
}

var (
	uniqNameMu   sync.Mutex
	uniqNameNext = make(map[string]int)
)

func newLoggingConn(baseName string, c net.Conn) net.Conn {
	uniqNameMu.Lock()
	defer uniqNameMu.Unlock()
	uniqNameNext[baseName]++
	return &loggingConn{
		name: fmt.Sprintf("%s-%d", baseName, uniqNameNext[baseName]),
		Conn: c,
	}
}

func (c *loggingConn) Write(p []byte) (n int, err error) {
	log.Printf("%s.Write(%d) = ....", c.name, len(p))
	n, err = c.Conn.Write(p)
	log.Printf("%s.Write(%d) = %d, %v", c.name, len(p), n, err)
	return
}

func (c *loggingConn) Read(p []byte) (n int, err error) {
	log.Printf("%s.Read(%d) = ....", c.name, len(p))
	n, err = c.Conn.Read(p)
	log.Printf("%s.Read(%d) = %d, %v", c.name, len(p), n, err)
	return
}

func (c *loggingConn) Close() (err error) {
	log.Printf("%s.Close() = ...", c.name)
	err = c.Conn.Close()
	log.Printf("%s.Close() = %v", c.name, err)
	return
}

// checkConnErrorWriter writes to c.rwc and records any write errors to c.werr.
// It only contains one field (and a pointer field at that), so it
// fits in an interface value without an extra allocation.
type checkConnErrorWriter struct {
	c *conn
}

func (w checkConnErrorWriter) Write(p []byte) (n int, err error) {
	n, err = w.c.rwc.Write(p)
	if err != nil && w.c.werr == nil {
		w.c.werr = err
		w.c.cancelCtx()
	}
	return
}

func numLeadingCRorLF(v []byte) (n int) {
	for _, b := range v {
		if b == '\r' || b == '\n' {
			n++
			continue
		}
		break
	}
	return

}

func strSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// DefaultTransport is the default implementation of Transport and is
// used by DefaultClient. It establishes network connections as needed
// and caches them for reuse by subsequent calls. It uses HTTP proxies
// as directed by the $HTTP_PROXY and $NO_PROXY (or $http_proxy and
// $no_proxy) environment variables.
var DefaultTransport RoundTripper = &Transport{
	Proxy: ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}).DialContext,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

// DefaultMaxIdleConnsPerHost is the default value of Transport's
// MaxIdleConnsPerHost.
const DefaultMaxIdleConnsPerHost = 2

// Transport is an implementation of RoundTripper that supports HTTP,
// HTTPS, and HTTP proxies (for either HTTP or HTTPS with CONNECT).
//
// By default, Transport caches connections for future re-use.
// This may leave many open connections when accessing many hosts.
// This behavior can be managed using Transport's CloseIdleConnections method
// and the MaxIdleConnsPerHost and DisableKeepAlives fields.
//
// Transports should be reused instead of created as needed.
// Transports are safe for concurrent use by multiple goroutines.
//
// A Transport is a low-level primitive for making HTTP and HTTPS requests.
// For high-level functionality, such as cookies and redirects, see Client.
//
// Transport uses HTTP/1.1 for HTTP URLs and either HTTP/1.1 or HTTP/2
// for HTTPS URLs, depending on whether the server supports HTTP/2,
// and how the Transport is configured. The DefaultTransport supports HTTP/2.
// To explicitly enable HTTP/2 on a transport, use golang.org/x/net/http2
// and call ConfigureTransport. See the package docs for more about HTTP/2.
type Transport struct {
	idleMu     sync.Mutex
	wantIdle   bool                                // user has requested to close all idle conns
	idleConn   map[connectMethodKey][]*persistConn // most recently used at end
	idleConnCh map[connectMethodKey]chan *persistConn
	idleLRU    connLRU

	reqMu       sync.Mutex
	reqCanceler map[*Request]func(error)

	altMu    sync.Mutex   // guards changing altProto only
	altProto atomic.Value // of nil or map[string]RoundTripper, key is URI scheme

	// Proxy specifies a function to return a proxy for a given
	// Request. If the function returns a non-nil error, the
	// request is aborted with the provided error.
	// If Proxy is nil or returns a nil *URL, no proxy is used.
	Proxy func(*Request) (*url.URL, error)

	// DialContext specifies the dial function for creating unencrypted TCP connections.
	// If DialContext is nil (and the deprecated Dial below is also nil),
	// then the transport dials using package net.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// Dial specifies the dial function for creating unencrypted TCP connections.
	//
	// Deprecated: Use DialContext instead, which allows the transport
	// to cancel dials as soon as they are no longer needed.
	// If both are set, DialContext takes priority.
	Dial func(network, addr string) (net.Conn, error)

	// DialTLS specifies an optional dial function for creating
	// TLS connections for non-proxied HTTPS requests.
	//
	// If DialTLS is nil, Dial and TLSClientConfig are used.
	//
	// If DialTLS is set, the Dial hook is not used for HTTPS
	// requests and the TLSClientConfig and TLSHandshakeTimeout
	// are ignored. The returned net.Conn is assumed to already be
	// past the TLS handshake.
	DialTLS func(network, addr string) (net.Conn, error)

	// TLSClientConfig specifies the TLS configuration to use with
	// tls.Client.
	// If nil, the default configuration is used.
	// If non-nil, HTTP/2 support may not be enabled by default.
	TLSClientConfig *tls.Config

	// TLSHandshakeTimeout specifies the maximum amount of time waiting to
	// wait for a TLS handshake. Zero means no timeout.
	TLSHandshakeTimeout time.Duration

	// DisableKeepAlives, if true, prevents re-use of TCP connections
	// between different HTTP requests.
	DisableKeepAlives bool

	// DisableCompression, if true, prevents the Transport from
	// requesting compression with an "Accept-Encoding: gzip"
	// request header when the Request contains no existing
	// Accept-Encoding value. If the Transport requests gzip on
	// its own and gets a gzipped response, it's transparently
	// decoded in the Response.Body. However, if the user
	// explicitly requested gzip it is not automatically
	// uncompressed.
	DisableCompression bool

	// MaxIdleConns controls the maximum number of idle (keep-alive)
	// connections across all hosts. Zero means no limit.
	MaxIdleConns int

	// MaxIdleConnsPerHost, if non-zero, controls the maximum idle
	// (keep-alive) connections to keep per-host. If zero,
	// DefaultMaxIdleConnsPerHost is used.
	MaxIdleConnsPerHost int

	// IdleConnTimeout is the maximum amount of time an idle
	// (keep-alive) connection will remain idle before closing
	// itself.
	// Zero means no limit.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout, if non-zero, specifies the amount of
	// time to wait for a server's response headers after fully
	// writing the request (including its body, if any). This
	// time does not include the time to read the response body.
	ResponseHeaderTimeout time.Duration

	// ExpectContinueTimeout, if non-zero, specifies the amount of
	// time to wait for a server's first response headers after fully
	// writing the request headers if the request has an
	// "Expect: 100-continue" header. Zero means no timeout and
	// causes the body to be sent immediately, without
	// waiting for the server to approve.
	// This time does not include the time to send the request header.
	ExpectContinueTimeout time.Duration

	// TLSNextProto specifies how the Transport switches to an
	// alternate protocol (such as HTTP/2) after a TLS NPN/ALPN
	// protocol negotiation. If Transport dials an TLS connection
	// with a non-empty protocol name and TLSNextProto contains a
	// map entry for that key (such as "h2"), then the func is
	// called with the request's authority (such as "example.com"
	// or "example.com:1234") and the TLS connection. The function
	// must return a RoundTripper that then handles the request.
	// If TLSNextProto is not nil, HTTP/2 support is not enabled
	// automatically.
	TLSNextProto map[string]func(authority string, c *tls.Conn) RoundTripper

	// ProxyConnectHeader optionally specifies headers to send to
	// proxies during CONNECT requests.
	ProxyConnectHeader Header

	// MaxResponseHeaderBytes specifies a limit on how many
	// response bytes are allowed in the server's response
	// header.
	//
	// Zero means to use a default limit.
	MaxResponseHeaderBytes int64

	// nextProtoOnce guards initialization of TLSNextProto and
	// h2transport (via onceSetNextProtoDefaults)
	nextProtoOnce sync.Once
	h2transport   *http2Transport // non-nil if http2 wired up

	// TODO: tunable on max per-host TCP dials in flight (Issue 13957)
}

// onceSetNextProtoDefaults initializes TLSNextProto.
// It must be called via t.nextProtoOnce.Do.
func (t *Transport) onceSetNextProtoDefaults() {
	if strings.Contains(os.Getenv("GODEBUG"), "http2client=0") {
		return
	}
	if t.TLSNextProto != nil {
		// This is the documented way to disable http2 on a
		// Transport.
		return
	}
	if t.TLSClientConfig != nil || t.Dial != nil || t.DialTLS != nil {
		// Be conservative and don't automatically enable
		// http2 if they've specified a custom TLS config or
		// custom dialers. Let them opt-in themselves via
		// http2.ConfigureTransport so we don't surprise them
		// by modifying their tls.Config. Issue 14275.
		return
	}
	t2, err := http2configureTransport(t)
	if err != nil {
		log.Printf("Error enabling Transport HTTP/2 support: %v", err)
		return
	}
	t.h2transport = t2

	// Auto-configure the http2.Transport's MaxHeaderListSize from
	// the http.Transport's MaxResponseHeaderBytes. They don't
	// exactly mean the same thing, but they're close.
	//
	// TODO: also add this to x/net/http2.Configure Transport, behind
	// a +build go1.7 build tag:
	if limit1 := t.MaxResponseHeaderBytes; limit1 != 0 && t2.MaxHeaderListSize == 0 {
		const h2max = 1<<32 - 1
		if limit1 >= h2max {
			t2.MaxHeaderListSize = h2max
		} else {
			t2.MaxHeaderListSize = uint32(limit1)
		}
	}
}

// ProxyFromEnvironment returns the URL of the proxy to use for a
// given request, as indicated by the environment variables
// HTTP_PROXY, HTTPS_PROXY and NO_PROXY (or the lowercase versions
// thereof). HTTPS_PROXY takes precedence over HTTP_PROXY for https
// requests.
//
// The environment values may be either a complete URL or a
// "host[:port]", in which case the "http" scheme is assumed.
// An error is returned if the value is a different form.
//
// A nil URL and nil error are returned if no proxy is defined in the
// environment, or a proxy should not be used for the given request,
// as defined by NO_PROXY.
//
// As a special case, if req.URL.Host is "localhost" (with or without
// a port number), then a nil URL and nil error will be returned.
func ProxyFromEnvironment(req *Request) (*url.URL, error) {
	var proxy string
	if req.URL.Scheme == "https" {
		proxy = httpsProxyEnv.Get()
	}
	if proxy == "" {
		proxy = httpProxyEnv.Get()
		if proxy != "" && os.Getenv("REQUEST_METHOD") != "" {
			return nil, errors.New("net/http: refusing to use HTTP_PROXY value in CGI environment; see golang.org/s/cgihttpproxy")
		}
	}
	if proxy == "" {
		return nil, nil
	}
	if !useProxy(canonicalAddr(req.URL)) {
		return nil, nil
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil || !strings.HasPrefix(proxyURL.Scheme, "http") {
		// proxy was bogus. Try prepending "http://" to it and
		// see if that parses correctly. If not, we fall
		// through and complain about the original one.
		if proxyURL, err := url.Parse("http://" + proxy); err == nil {
			return proxyURL, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("invalid proxy address %q: %v", proxy, err)
	}
	return proxyURL, nil
}

// ProxyURL returns a proxy function (for use in a Transport)
// that always returns the same URL.
func ProxyURL(fixedURL *url.URL) func(*Request) (*url.URL, error) {
	return func(*Request) (*url.URL, error) {
		return fixedURL, nil
	}
}

// transportRequest is a wrapper around a *Request that adds
// optional extra headers to write.
type transportRequest struct {
	*Request                        // original request, not to be mutated
	extra    Header                 // extra headers to write, or nil
	trace    *httptrace.ClientTrace // optional
}

func (tr *transportRequest) extraHeaders() Header {
	if tr.extra == nil {
		tr.extra = make(Header)
	}
	return tr.extra
}

// RoundTrip implements the RoundTripper interface.
//
// For higher-level HTTP client support (such as handling of cookies
// and redirects), see Get, Post, and the Client type.
func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)
	ctx := req.Context()
	trace := httptrace.ContextClientTrace(ctx)

	if req.URL == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.URL")
	}
	if req.Header == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.Header")
	}
	scheme := req.URL.Scheme
	isHTTP := scheme == "http" || scheme == "https"
	if isHTTP {
		for k, vv := range req.Header {
			if !httplex.ValidHeaderFieldName(k) {
				return nil, fmt.Errorf("net/http: invalid header field name %q", k)
			}
			for _, v := range vv {
				if !httplex.ValidHeaderFieldValue(v) {
					return nil, fmt.Errorf("net/http: invalid header field value %q for key %v", v, k)
				}
			}
		}
	}

	altProto, _ := t.altProto.Load().(map[string]RoundTripper)
	if altRT := altProto[scheme]; altRT != nil {
		if resp, err := altRT.RoundTrip(req); err != ErrSkipAltProtocol {
			return resp, err
		}
	}
	if !isHTTP {
		req.closeBody()
		return nil, &badStringError{"unsupported protocol scheme", scheme}
	}
	if req.Method != "" && !validMethod(req.Method) {
		return nil, fmt.Errorf("net/http: invalid method %q", req.Method)
	}
	if req.URL.Host == "" {
		req.closeBody()
		return nil, errors.New("http: no Host in request URL")
	}

	for {
		// treq gets modified by roundTrip, so we need to recreate for each retry.
		treq := &transportRequest{Request: req, trace: trace}
		cm, err := t.connectMethodForRequest(treq)
		if err != nil {
			req.closeBody()
			return nil, err
		}

		// Get the cached or newly-created connection to either the
		// host (for http or https), the http proxy, or the http proxy
		// pre-CONNECTed to https server. In any case, we'll be ready
		// to send it requests.
		pconn, err := t.getConn(treq, cm)
		if err != nil {
			t.setReqCanceler(req, nil)
			req.closeBody()
			return nil, err
		}

		var resp *Response
		if pconn.alt != nil {
			// HTTP/2 path.
			t.setReqCanceler(req, nil) // not cancelable with CancelRequest
			resp, err = pconn.alt.RoundTrip(req)
		} else {
			resp, err = pconn.roundTrip(treq)
		}
		if err == nil {
			return resp, nil
		}
		if !pconn.shouldRetryRequest(req, err) {
			// Issue 16465: return underlying net.Conn.Read error from peek,
			// as we've historically done.
			if e, ok := err.(transportReadFromServerError); ok {
				err = e.err
			}
			return nil, err
		}
		testHookRoundTripRetried()
	}
}

// shouldRetryRequest reports whether we should retry sending a failed
// HTTP request on a new connection. The non-nil input error is the
// error from roundTrip.
func (pc *persistConn) shouldRetryRequest(req *Request, err error) bool {
	if err == http2ErrNoCachedConn {
		// Issue 16582: if the user started a bunch of
		// requests at once, they can all pick the same conn
		// and violate the server's max concurrent streams.
		// Instead, match the HTTP/1 behavior for now and dial
		// again to get a new TCP connection, rather than failing
		// this request.
		return true
	}
	if err == errMissingHost {
		// User error.
		return false
	}
	if !pc.isReused() {
		// This was a fresh connection. There's no reason the server
		// should've hung up on us.
		//
		// Also, if we retried now, we could loop forever
		// creating new connections and retrying if the server
		// is just hanging up on us because it doesn't like
		// our request (as opposed to sending an error).
		return false
	}
	if _, ok := err.(nothingWrittenError); ok {
		// We never wrote anything, so it's safe to retry.
		return true
	}
	if !req.isReplayable() {
		// Don't retry non-idempotent requests.
		return false
	}
	if _, ok := err.(transportReadFromServerError); ok {
		// We got some non-EOF net.Conn.Read failure reading
		// the 1st response byte from the server.
		return true
	}
	if err == errServerClosedIdle {
		// The server replied with io.EOF while we were trying to
		// read the response. Probably an unfortunately keep-alive
		// timeout, just as the client was writing a request.
		return true
	}
	return false // conservatively
}

// ErrSkipAltProtocol is a sentinel error value defined by Transport.RegisterProtocol.
var ErrSkipAltProtocol = errors.New("net/http: skip alternate protocol")

// RegisterProtocol registers a new protocol with scheme.
// The Transport will pass requests using the given scheme to rt.
// It is rt's responsibility to simulate HTTP request semantics.
//
// RegisterProtocol can be used by other packages to provide
// implementations of protocol schemes like "ftp" or "file".
//
// If rt.RoundTrip returns ErrSkipAltProtocol, the Transport will
// handle the RoundTrip itself for that one request, as if the
// protocol were not registered.
func (t *Transport) RegisterProtocol(scheme string, rt RoundTripper) {
	t.altMu.Lock()
	defer t.altMu.Unlock()
	oldMap, _ := t.altProto.Load().(map[string]RoundTripper)
	if _, exists := oldMap[scheme]; exists {
		panic("protocol " + scheme + " already registered")
	}
	newMap := make(map[string]RoundTripper)
	for k, v := range oldMap {
		newMap[k] = v
	}
	newMap[scheme] = rt
	t.altProto.Store(newMap)
}

// CloseIdleConnections closes any connections which were previously
// connected from previous requests but are now sitting idle in
// a "keep-alive" state. It does not interrupt any connections currently
// in use.
func (t *Transport) CloseIdleConnections() {
	t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)
	t.idleMu.Lock()
	m := t.idleConn
	t.idleConn = nil
	t.idleConnCh = nil
	t.wantIdle = true
	t.idleLRU = connLRU{}
	t.idleMu.Unlock()
	for _, conns := range m {
		for _, pconn := range conns {
			pconn.close(errCloseIdleConns)
		}
	}
	if t2 := t.h2transport; t2 != nil {
		t2.CloseIdleConnections()
	}
}

// CancelRequest cancels an in-flight request by closing its connection.
// CancelRequest should only be called after RoundTrip has returned.
//
// Deprecated: Use Request.WithContext to create a request with a
// cancelable context instead. CancelRequest cannot cancel HTTP/2
// requests.
func (t *Transport) CancelRequest(req *Request) {
	t.cancelRequest(req, errRequestCanceled)
}

// Cancel an in-flight request, recording the error value.
func (t *Transport) cancelRequest(req *Request, err error) {
	t.reqMu.Lock()
	cancel := t.reqCanceler[req]
	delete(t.reqCanceler, req)
	t.reqMu.Unlock()
	if cancel != nil {
		cancel(err)
	}
}

//
// Private implementation past this point.
//

var (
	httpProxyEnv = &envOnce{
		names: []string{"HTTP_PROXY", "http_proxy"},
	}
	httpsProxyEnv = &envOnce{
		names: []string{"HTTPS_PROXY", "https_proxy"},
	}
	noProxyEnv = &envOnce{
		names: []string{"NO_PROXY", "no_proxy"},
	}
)

// envOnce looks up an environment variable (optionally by multiple
// names) once. It mitigates expensive lookups on some platforms
// (e.g. Windows).
type envOnce struct {
	names []string
	once  sync.Once
	val   string
}

func (e *envOnce) Get() string {
	e.once.Do(e.init)
	return e.val
}

func (e *envOnce) init() {
	for _, n := range e.names {
		e.val = os.Getenv(n)
		if e.val != "" {
			return
		}
	}
}

// reset is used by tests
func (e *envOnce) reset() {
	e.once = sync.Once{}
	e.val = ""
}

func (t *Transport) connectMethodForRequest(treq *transportRequest) (cm connectMethod, err error) {
	if port := treq.URL.Port(); !validPort(port) {
		return cm, fmt.Errorf("invalid URL port %q", port)
	}
	cm.targetScheme = treq.URL.Scheme
	cm.targetAddr = canonicalAddr(treq.URL)
	if t.Proxy != nil {
		cm.proxyURL, err = t.Proxy(treq.Request)
		if err == nil && cm.proxyURL != nil {
			if port := cm.proxyURL.Port(); !validPort(port) {
				return cm, fmt.Errorf("invalid proxy URL port %q", port)
			}
		}
	}
	return cm, err
}

// proxyAuth returns the Proxy-Authorization header to set
// on requests, if applicable.
func (cm *connectMethod) proxyAuth() string {
	if cm.proxyURL == nil {
		return ""
	}
	if u := cm.proxyURL.User; u != nil {
		username := u.Username()
		password, _ := u.Password()
		return "Basic " + basicAuth(username, password)
	}
	return ""
}

// error values for debugging and testing, not seen by users.
var (
	errKeepAlivesDisabled = errors.New("http: putIdleConn: keep alives disabled")
	errConnBroken         = errors.New("http: putIdleConn: connection is in bad state")
	errWantIdle           = errors.New("http: putIdleConn: CloseIdleConnections was called")
	errTooManyIdle        = errors.New("http: putIdleConn: too many idle connections")
	errTooManyIdleHost    = errors.New("http: putIdleConn: too many idle connections for host")
	errCloseIdleConns     = errors.New("http: CloseIdleConnections called")
	errReadLoopExiting    = errors.New("http: persistConn.readLoop exiting")
	errServerClosedIdle   = errors.New("http: server closed idle connection")
	errIdleConnTimeout    = errors.New("http: idle connection timeout")
	errNotCachingH2Conn   = errors.New("http: not caching alternate protocol's connections")
)

// transportReadFromServerError is used by Transport.readLoop when the
// 1 byte peek read fails and we're actually anticipating a response.
// Usually this is just due to the inherent keep-alive shut down race,
// where the server closed the connection at the same time the client
// wrote. The underlying err field is usually io.EOF or some
// ECONNRESET sort of thing which varies by platform. But it might be
// the user's custom net.Conn.Read error too, so we carry it along for
// them to return from Transport.RoundTrip.
type transportReadFromServerError struct {
	err error
}

func (e transportReadFromServerError) Error() string {
	return fmt.Sprintf("net/http: Transport failed to read from server: %v", e.err)
}

func (t *Transport) putOrCloseIdleConn(pconn *persistConn) {
	if err := t.tryPutIdleConn(pconn); err != nil {
		pconn.close(err)
	}
}

func (t *Transport) maxIdleConnsPerHost() int {
	if v := t.MaxIdleConnsPerHost; v != 0 {
		return v
	}
	return DefaultMaxIdleConnsPerHost
}

// tryPutIdleConn adds pconn to the list of idle persistent connections awaiting
// a new request.
// If pconn is no longer needed or not in a good state, tryPutIdleConn returns
// an error explaining why it wasn't registered.
// tryPutIdleConn does not close pconn. Use putOrCloseIdleConn instead for that.
func (t *Transport) tryPutIdleConn(pconn *persistConn) error {
	if t.DisableKeepAlives || t.MaxIdleConnsPerHost < 0 {
		return errKeepAlivesDisabled
	}
	if pconn.isBroken() {
		return errConnBroken
	}
	if pconn.alt != nil {
		return errNotCachingH2Conn
	}
	pconn.markReused()
	key := pconn.cacheKey

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	waitingDialer := t.idleConnCh[key]
	select {
	case waitingDialer <- pconn:
		// We're done with this pconn and somebody else is
		// currently waiting for a conn of this type (they're
		// actively dialing, but this conn is ready
		// first). Chrome calls this socket late binding. See
		// https://insouciant.org/tech/connection-management-in-chromium/
		return nil
	default:
		if waitingDialer != nil {
			// They had populated this, but their dial won
			// first, so we can clean up this map entry.
			delete(t.idleConnCh, key)
		}
	}
	if t.wantIdle {
		return errWantIdle
	}
	if t.idleConn == nil {
		t.idleConn = make(map[connectMethodKey][]*persistConn)
	}
	idles := t.idleConn[key]
	if len(idles) >= t.maxIdleConnsPerHost() {
		return errTooManyIdleHost
	}
	for _, exist := range idles {
		if exist == pconn {
			log.Fatalf("dup idle pconn %p in freelist", pconn)
		}
	}
	t.idleConn[key] = append(idles, pconn)
	t.idleLRU.add(pconn)
	if t.MaxIdleConns != 0 && t.idleLRU.len() > t.MaxIdleConns {
		oldest := t.idleLRU.removeOldest()
		oldest.close(errTooManyIdle)
		t.removeIdleConnLocked(oldest)
	}
	if t.IdleConnTimeout > 0 {
		if pconn.idleTimer != nil {
			pconn.idleTimer.Reset(t.IdleConnTimeout)
		} else {
			pconn.idleTimer = time.AfterFunc(t.IdleConnTimeout, pconn.closeConnIfStillIdle)
		}
	}
	pconn.idleAt = time.Now()
	return nil
}

// getIdleConnCh returns a channel to receive and return idle
// persistent connection for the given connectMethod.
// It may return nil, if persistent connections are not being used.
func (t *Transport) getIdleConnCh(cm connectMethod) chan *persistConn {
	if t.DisableKeepAlives {
		return nil
	}
	key := cm.key()
	t.idleMu.Lock()
	defer t.idleMu.Unlock()
	t.wantIdle = false
	if t.idleConnCh == nil {
		t.idleConnCh = make(map[connectMethodKey]chan *persistConn)
	}
	ch, ok := t.idleConnCh[key]
	if !ok {
		ch = make(chan *persistConn)
		t.idleConnCh[key] = ch
	}
	return ch
}

func (t *Transport) getIdleConn(cm connectMethod) (pconn *persistConn, idleSince time.Time) {
	key := cm.key()
	t.idleMu.Lock()
	defer t.idleMu.Unlock()
	for {
		pconns, ok := t.idleConn[key]
		if !ok {
			return nil, time.Time{}
		}
		if len(pconns) == 1 {
			pconn = pconns[0]
			delete(t.idleConn, key)
		} else {
			// 2 or more cached connections; use the most
			// recently used one at the end.
			pconn = pconns[len(pconns)-1]
			t.idleConn[key] = pconns[:len(pconns)-1]
		}
		t.idleLRU.remove(pconn)
		if pconn.isBroken() {
			// There is a tiny window where this is
			// possible, between the connecting dying and
			// the persistConn readLoop calling
			// Transport.removeIdleConn. Just skip it and
			// carry on.
			continue
		}
		if pconn.idleTimer != nil && !pconn.idleTimer.Stop() {
			// We picked this conn at the ~same time it
			// was expiring and it's trying to close
			// itself in another goroutine. Don't use it.
			continue
		}
		return pconn, pconn.idleAt
	}
}

// removeIdleConn marks pconn as dead.
func (t *Transport) removeIdleConn(pconn *persistConn) {
	t.idleMu.Lock()
	defer t.idleMu.Unlock()
	t.removeIdleConnLocked(pconn)
}

// t.idleMu must be held.
func (t *Transport) removeIdleConnLocked(pconn *persistConn) {
	if pconn.idleTimer != nil {
		pconn.idleTimer.Stop()
	}
	t.idleLRU.remove(pconn)
	key := pconn.cacheKey
	pconns, _ := t.idleConn[key]
	switch len(pconns) {
	case 0:
		// Nothing
	case 1:
		if pconns[0] == pconn {
			delete(t.idleConn, key)
		}
	default:
		for i, v := range pconns {
			if v != pconn {
				continue
			}
			// Slide down, keeping most recently-used
			// conns at the end.
			copy(pconns[i:], pconns[i+1:])
			t.idleConn[key] = pconns[:len(pconns)-1]
			break
		}
	}
}

func (t *Transport) setReqCanceler(r *Request, fn func(error)) {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	if t.reqCanceler == nil {
		t.reqCanceler = make(map[*Request]func(error))
	}
	if fn != nil {
		t.reqCanceler[r] = fn
	} else {
		delete(t.reqCanceler, r)
	}
}

// replaceReqCanceler replaces an existing cancel function. If there is no cancel function
// for the request, we don't set the function and return false.
// Since CancelRequest will clear the canceler, we can use the return value to detect if
// the request was canceled since the last setReqCancel call.
func (t *Transport) replaceReqCanceler(r *Request, fn func(error)) bool {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	_, ok := t.reqCanceler[r]
	if !ok {
		return false
	}
	if fn != nil {
		t.reqCanceler[r] = fn
	} else {
		delete(t.reqCanceler, r)
	}
	return true
}

var zeroDialer net.Dialer

func (t *Transport) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if t.DialContext != nil {
		return t.DialContext(ctx, network, addr)
	}
	if t.Dial != nil {
		c, err := t.Dial(network, addr)
		if c == nil && err == nil {
			err = errors.New("net/http: Transport.Dial hook returned (nil, nil)")
		}
		return c, err
	}
	return zeroDialer.DialContext(ctx, network, addr)
}

// getConn dials and creates a new persistConn to the target as
// specified in the connectMethod. This includes doing a proxy CONNECT
// and/or setting up TLS.  If this doesn't return an error, the persistConn
// is ready to write requests to.
func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (*persistConn, error) {
	req := treq.Request
	trace := treq.trace
	ctx := req.Context()
	if trace != nil && trace.GetConn != nil {
		trace.GetConn(cm.addr())
	}
	if pc, idleSince := t.getIdleConn(cm); pc != nil {
		if trace != nil && trace.GotConn != nil {
			trace.GotConn(pc.gotIdleConnTrace(idleSince))
		}
		// set request canceler to some non-nil function so we
		// can detect whether it was cleared between now and when
		// we enter roundTrip
		t.setReqCanceler(req, func(error) {})
		return pc, nil
	}

	type dialRes struct {
		pc  *persistConn
		err error
	}
	dialc := make(chan dialRes)

	// Copy these hooks so we don't race on the postPendingDial in
	// the goroutine we launch. Issue 11136.
	testHookPrePendingDial := testHookPrePendingDial
	testHookPostPendingDial := testHookPostPendingDial

	handlePendingDial := func() {
		testHookPrePendingDial()
		go func() {
			if v := <-dialc; v.err == nil {
				t.putOrCloseIdleConn(v.pc)
			}
			testHookPostPendingDial()
		}()
	}

	cancelc := make(chan error, 1)
	t.setReqCanceler(req, func(err error) { cancelc <- err })

	go func() {
		pc, err := t.dialConn(ctx, cm)
		dialc <- dialRes{pc, err}
	}()

	idleConnCh := t.getIdleConnCh(cm)
	select {
	case v := <-dialc:
		// Our dial finished.
		if v.pc != nil {
			if trace != nil && trace.GotConn != nil && v.pc.alt == nil {
				trace.GotConn(httptrace.GotConnInfo{Conn: v.pc.conn})
			}
			return v.pc, nil
		}
		// Our dial failed. See why to return a nicer error
		// value.
		select {
		case <-req.Cancel:
			// It was an error due to cancelation, so prioritize that
			// error value. (Issue 16049)
			return nil, errRequestCanceledConn
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case err := <-cancelc:
			if err == errRequestCanceled {
				err = errRequestCanceledConn
			}
			return nil, err
		default:
			// It wasn't an error due to cancelation, so
			// return the original error message:
			return nil, v.err
		}
	case pc := <-idleConnCh:
		// Another request finished first and its net.Conn
		// became available before our dial. Or somebody
		// else's dial that they didn't use.
		// But our dial is still going, so give it away
		// when it finishes:
		handlePendingDial()
		if trace != nil && trace.GotConn != nil {
			trace.GotConn(httptrace.GotConnInfo{Conn: pc.conn, Reused: pc.isReused()})
		}
		return pc, nil
	case <-req.Cancel:
		handlePendingDial()
		return nil, errRequestCanceledConn
	case <-req.Context().Done():
		handlePendingDial()
		return nil, req.Context().Err()
	case err := <-cancelc:
		handlePendingDial()
		if err == errRequestCanceled {
			err = errRequestCanceledConn
		}
		return nil, err
	}
}

func (t *Transport) dialConn(ctx context.Context, cm connectMethod) (*persistConn, error) {
	pconn := &persistConn{
		t:             t,
		cacheKey:      cm.key(),
		reqch:         make(chan requestAndChan, 1),
		writech:       make(chan writeRequest, 1),
		closech:       make(chan struct{}),
		writeErrCh:    make(chan error, 1),
		writeLoopDone: make(chan struct{}),
	}
	trace := httptrace.ContextClientTrace(ctx)
	tlsDial := t.DialTLS != nil && cm.targetScheme == "https" && cm.proxyURL == nil
	if tlsDial {
		var err error
		pconn.conn, err = t.DialTLS("tcp", cm.addr())
		if err != nil {
			return nil, err
		}
		if pconn.conn == nil {
			return nil, errors.New("net/http: Transport.DialTLS returned (nil, nil)")
		}
		if tc, ok := pconn.conn.(*tls.Conn); ok {
			// Handshake here, in case DialTLS didn't. TLSNextProto below
			// depends on it for knowing the connection state.
			if trace != nil && trace.TLSHandshakeStart != nil {
				trace.TLSHandshakeStart()
			}
			if err := tc.Handshake(); err != nil {
				go pconn.conn.Close()
				if trace != nil && trace.TLSHandshakeDone != nil {
					trace.TLSHandshakeDone(tls.ConnectionState{}, err)
				}
				return nil, err
			}
			cs := tc.ConnectionState()
			if trace != nil && trace.TLSHandshakeDone != nil {
				trace.TLSHandshakeDone(cs, nil)
			}
			pconn.tlsState = &cs
		}
	} else {
		conn, err := t.dial(ctx, "tcp", cm.addr())
		if err != nil {
			if cm.proxyURL != nil {
				// Return a typed error, per Issue 16997:
				err = &net.OpError{Op: "proxyconnect", Net: "tcp", Err: err}
			}
			return nil, err
		}
		pconn.conn = conn
	}

	// Proxy setup.
	switch {
	case cm.proxyURL == nil:
		// Do nothing. Not using a proxy.
	case cm.targetScheme == "http":
		pconn.isProxy = true
		if pa := cm.proxyAuth(); pa != "" {
			pconn.mutateHeaderFunc = func(h Header) {
				h.Set("Proxy-Authorization", pa)
			}
		}
	case cm.targetScheme == "https":
		conn := pconn.conn
		hdr := t.ProxyConnectHeader
		if hdr == nil {
			hdr = make(Header)
		}
		connectReq := &Request{
			Method: "CONNECT",
			URL:    &url.URL{Opaque: cm.targetAddr},
			Host:   cm.targetAddr,
			Header: hdr,
		}
		if pa := cm.proxyAuth(); pa != "" {
			connectReq.Header.Set("Proxy-Authorization", pa)
		}
		connectReq.Write(conn)

		// Read response.
		// Okay to use and discard buffered reader here, because
		// TLS server will not speak until spoken to.
		br := bufio.NewReader(conn)
		resp, err := ReadResponse(br, connectReq)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if resp.StatusCode != 200 {
			f := strings.SplitN(resp.Status, " ", 2)
			conn.Close()
			return nil, errors.New(f[1])
		}
	}

	if cm.targetScheme == "https" && !tlsDial {
		// Initiate TLS and check remote host name against certificate.
		cfg := cloneTLSConfig(t.TLSClientConfig)
		if cfg.ServerName == "" {
			cfg.ServerName = cm.tlsHost()
		}
		plainConn := pconn.conn
		tlsConn := tls.Client(plainConn, cfg)
		errc := make(chan error, 2)
		var timer *time.Timer // for canceling TLS handshake
		if d := t.TLSHandshakeTimeout; d != 0 {
			timer = time.AfterFunc(d, func() {
				errc <- tlsHandshakeTimeoutError{}
			})
		}
		go func() {
			if trace != nil && trace.TLSHandshakeStart != nil {
				trace.TLSHandshakeStart()
			}
			err := tlsConn.Handshake()
			if timer != nil {
				timer.Stop()
			}
			errc <- err
		}()
		if err := <-errc; err != nil {
			plainConn.Close()
			if trace != nil && trace.TLSHandshakeDone != nil {
				trace.TLSHandshakeDone(tls.ConnectionState{}, err)
			}
			return nil, err
		}
		if !cfg.InsecureSkipVerify {
			if err := tlsConn.VerifyHostname(cfg.ServerName); err != nil {
				plainConn.Close()
				return nil, err
			}
		}
		cs := tlsConn.ConnectionState()
		if trace != nil && trace.TLSHandshakeDone != nil {
			trace.TLSHandshakeDone(cs, nil)
		}
		pconn.tlsState = &cs
		pconn.conn = tlsConn
	}

	if s := pconn.tlsState; s != nil && s.NegotiatedProtocolIsMutual && s.NegotiatedProtocol != "" {
		if next, ok := t.TLSNextProto[s.NegotiatedProtocol]; ok {
			return &persistConn{alt: next(cm.targetAddr, pconn.conn.(*tls.Conn))}, nil
		}
	}

	pconn.br = bufio.NewReader(pconn)
	pconn.bw = bufio.NewWriter(persistConnWriter{pconn})
	go pconn.readLoop()
	go pconn.writeLoop()
	return pconn, nil
}

// persistConnWriter is the io.Writer written to by pc.bw.
// It accumulates the number of bytes written to the underlying conn,
// so the retry logic can determine whether any bytes made it across
// the wire.
// This is exactly 1 pointer field wide so it can go into an interface
// without allocation.
type persistConnWriter struct {
	pc *persistConn
}

func (w persistConnWriter) Write(p []byte) (n int, err error) {
	n, err = w.pc.conn.Write(p)
	w.pc.nwrite += int64(n)
	return
}

// useProxy reports whether requests to addr should use a proxy,
// according to the NO_PROXY or no_proxy environment variable.
// addr is always a canonicalAddr with a host and port.
func useProxy(addr string) bool {
	if len(addr) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return false
		}
	}

	no_proxy := noProxyEnv.Get()
	if no_proxy == "*" {
		return false
	}

	addr = strings.ToLower(strings.TrimSpace(addr))
	if hasPort(addr) {
		addr = addr[:strings.LastIndex(addr, ":")]
	}

	for _, p := range strings.Split(no_proxy, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if len(p) == 0 {
			continue
		}
		if hasPort(p) {
			p = p[:strings.LastIndex(p, ":")]
		}
		if addr == p {
			return false
		}
		if p[0] == '.' && (strings.HasSuffix(addr, p) || addr == p[1:]) {
			// no_proxy ".foo.com" matches "bar.foo.com" or "foo.com"
			return false
		}
		if p[0] != '.' && strings.HasSuffix(addr, p) && addr[len(addr)-len(p)-1] == '.' {
			// no_proxy "foo.com" matches "bar.foo.com"
			return false
		}
	}
	return true
}

// connectMethod is the map key (in its String form) for keeping persistent
// TCP connections alive for subsequent HTTP requests.
//
// A connect method may be of the following types:
//
// Cache key form                Description
// -----------------             -------------------------
// |http|foo.com                 http directly to server, no proxy
// |https|foo.com                https directly to server, no proxy
// http://proxy.com|https|foo.com  http to proxy, then CONNECT to foo.com
// http://proxy.com|http           http to proxy, http to anywhere after that
//
// Note: no support to https to the proxy yet.
//
type connectMethod struct {
	proxyURL     *url.URL // nil for no proxy, else full proxy URL
	targetScheme string   // "http" or "https"
	targetAddr   string   // Not used if proxy + http targetScheme (4th example in table)
}

func (cm *connectMethod) key() connectMethodKey {
	proxyStr := ""
	targetAddr := cm.targetAddr
	if cm.proxyURL != nil {
		proxyStr = cm.proxyURL.String()
		if cm.targetScheme == "http" {
			targetAddr = ""
		}
	}
	return connectMethodKey{
		proxy:  proxyStr,
		scheme: cm.targetScheme,
		addr:   targetAddr,
	}
}

// addr returns the first hop "host:port" to which we need to TCP connect.
func (cm *connectMethod) addr() string {
	if cm.proxyURL != nil {
		return canonicalAddr(cm.proxyURL)
	}
	return cm.targetAddr
}

// tlsHost returns the host name to match against the peer's
// TLS certificate.
func (cm *connectMethod) tlsHost() string {
	h := cm.targetAddr
	if hasPort(h) {
		h = h[:strings.LastIndex(h, ":")]
	}
	return h
}

// connectMethodKey is the map key version of connectMethod, with a
// stringified proxy URL (or the empty string) instead of a pointer to
// a URL.
type connectMethodKey struct {
	proxy, scheme, addr string
}

func (k connectMethodKey) String() string {
	// Only used by tests.
	return fmt.Sprintf("%s|%s|%s", k.proxy, k.scheme, k.addr)
}

// persistConn wraps a connection, usually a persistent one
// (but may be used for non-keep-alive requests as well)
type persistConn struct {
	// alt optionally specifies the TLS NextProto RoundTripper.
	// This is used for HTTP/2 today and future protocols later.
	// If it's non-nil, the rest of the fields are unused.
	alt RoundTripper

	t         *Transport
	cacheKey  connectMethodKey
	conn      net.Conn
	tlsState  *tls.ConnectionState
	br        *bufio.Reader       // from conn
	bw        *bufio.Writer       // to conn
	nwrite    int64               // bytes written
	reqch     chan requestAndChan // written by roundTrip; read by readLoop
	writech   chan writeRequest   // written by roundTrip; read by writeLoop
	closech   chan struct{}       // closed when conn closed
	isProxy   bool
	sawEOF    bool  // whether we've seen EOF from conn; owned by readLoop
	readLimit int64 // bytes allowed to be read; owned by readLoop
	// writeErrCh passes the request write error (usually nil)
	// from the writeLoop goroutine to the readLoop which passes
	// it off to the res.Body reader, which then uses it to decide
	// whether or not a connection can be reused. Issue 7569.
	writeErrCh chan error

	writeLoopDone chan struct{} // closed when write loop ends

	// Both guarded by Transport.idleMu:
	idleAt    time.Time   // time it last become idle
	idleTimer *time.Timer // holding an AfterFunc to close it

	mu                   sync.Mutex // guards following fields
	numExpectedResponses int
	closed               error // set non-nil when conn is closed, before closech is closed
	canceledErr          error // set non-nil if conn is canceled
	broken               bool  // an error has happened on this connection; marked broken so it's not reused.
	reused               bool  // whether conn has had successful request/response and is being reused.
	// mutateHeaderFunc is an optional func to modify extra
	// headers on each outbound request before it's written. (the
	// original Request given to RoundTrip is not modified)
	mutateHeaderFunc func(Header)
}

func (pc *persistConn) maxHeaderResponseSize() int64 {
	if v := pc.t.MaxResponseHeaderBytes; v != 0 {
		return v
	}
	return 10 << 20 // conservative default; same as http2
}

func (pc *persistConn) Read(p []byte) (n int, err error) {
	if pc.readLimit <= 0 {
		return 0, fmt.Errorf("read limit of %d bytes exhausted", pc.maxHeaderResponseSize())
	}
	if int64(len(p)) > pc.readLimit {
		p = p[:pc.readLimit]
	}
	n, err = pc.conn.Read(p)
	if err == io.EOF {
		pc.sawEOF = true
	}
	pc.readLimit -= int64(n)
	return
}

// isBroken reports whether this connection is in a known broken state.
func (pc *persistConn) isBroken() bool {
	pc.mu.Lock()
	b := pc.closed != nil
	pc.mu.Unlock()
	return b
}

// canceled returns non-nil if the connection was closed due to
// CancelRequest or due to context cancelation.
func (pc *persistConn) canceled() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.canceledErr
}

// isReused reports whether this connection is in a known broken state.
func (pc *persistConn) isReused() bool {
	pc.mu.Lock()
	r := pc.reused
	pc.mu.Unlock()
	return r
}

func (pc *persistConn) gotIdleConnTrace(idleAt time.Time) (t httptrace.GotConnInfo) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	t.Reused = pc.reused
	t.Conn = pc.conn
	t.WasIdle = true
	if !idleAt.IsZero() {
		t.IdleTime = time.Since(idleAt)
	}
	return
}

func (pc *persistConn) cancelRequest(err error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.canceledErr = err
	pc.closeLocked(errRequestCanceled)
}

// closeConnIfStillIdle closes the connection if it's still sitting idle.
// This is what's called by the persistConn's idleTimer, and is run in its
// own goroutine.
func (pc *persistConn) closeConnIfStillIdle() {
	t := pc.t
	t.idleMu.Lock()
	defer t.idleMu.Unlock()
	if _, ok := t.idleLRU.m[pc]; !ok {
		// Not idle.
		return
	}
	t.removeIdleConnLocked(pc)
	pc.close(errIdleConnTimeout)
}

// mapRoundTripErrorFromReadLoop maps the provided readLoop error into
// the error value that should be returned from persistConn.roundTrip.
//
// The startBytesWritten value should be the value of pc.nwrite before the roundTrip
// started writing the request.
func (pc *persistConn) mapRoundTripErrorFromReadLoop(req *Request, startBytesWritten int64, err error) (out error) {
	if err == nil {
		return nil
	}
	if err := pc.canceled(); err != nil {
		return err
	}
	if err == errServerClosedIdle {
		return err
	}
	if _, ok := err.(transportReadFromServerError); ok {
		return err
	}
	if pc.isBroken() {
		<-pc.writeLoopDone
		if pc.nwrite == startBytesWritten && req.outgoingLength() == 0 {
			return nothingWrittenError{err}
		}
	}
	return err
}

// mapRoundTripErrorAfterClosed returns the error value to be propagated
// up to Transport.RoundTrip method when persistConn.roundTrip sees
// its pc.closech channel close, indicating the persistConn is dead.
// (after closech is closed, pc.closed is valid).
func (pc *persistConn) mapRoundTripErrorAfterClosed(req *Request, startBytesWritten int64) error {
	if err := pc.canceled(); err != nil {
		return err
	}
	err := pc.closed
	if err == errServerClosedIdle {
		// Don't decorate
		return err
	}
	if _, ok := err.(transportReadFromServerError); ok {
		// Don't decorate
		return err
	}

	// Wait for the writeLoop goroutine to terminated, and then
	// see if we actually managed to write anything. If not, we
	// can retry the request.
	<-pc.writeLoopDone
	if pc.nwrite == startBytesWritten && req.outgoingLength() == 0 {
		return nothingWrittenError{err}
	}

	return fmt.Errorf("net/http: HTTP/1.x transport connection broken: %v", err)

}

func (pc *persistConn) readLoop() {
	closeErr := errReadLoopExiting // default value, if not changed below
	defer func() {
		pc.close(closeErr)
		pc.t.removeIdleConn(pc)
	}()

	tryPutIdleConn := func(trace *httptrace.ClientTrace) bool {
		if err := pc.t.tryPutIdleConn(pc); err != nil {
			closeErr = err
			if trace != nil && trace.PutIdleConn != nil && err != errKeepAlivesDisabled {
				trace.PutIdleConn(err)
			}
			return false
		}
		if trace != nil && trace.PutIdleConn != nil {
			trace.PutIdleConn(nil)
		}
		return true
	}

	// eofc is used to block caller goroutines reading from Response.Body
	// at EOF until this goroutines has (potentially) added the connection
	// back to the idle pool.
	eofc := make(chan struct{})
	defer close(eofc) // unblock reader on errors

	// Read this once, before loop starts. (to avoid races in tests)
	testHookMu.Lock()
	testHookReadLoopBeforeNextRead := testHookReadLoopBeforeNextRead
	testHookMu.Unlock()

	alive := true
	for alive {
		pc.readLimit = pc.maxHeaderResponseSize()
		_, err := pc.br.Peek(1)

		pc.mu.Lock()
		if pc.numExpectedResponses == 0 {
			pc.readLoopPeekFailLocked(err)
			pc.mu.Unlock()
			return
		}
		pc.mu.Unlock()

		rc := <-pc.reqch
		trace := httptrace.ContextClientTrace(rc.req.Context())

		var resp *Response
		if err == nil {
			resp, err = pc.readResponse(rc, trace)
		} else {
			err = transportReadFromServerError{err}
			closeErr = err
		}

		if err != nil {
			if pc.readLimit <= 0 {
				err = fmt.Errorf("net/http: server response headers exceeded %d bytes; aborted", pc.maxHeaderResponseSize())
			}

			// If we won't be able to retry this request later (from the
			// roundTrip goroutine), mark it as done now.
			// BEFORE the send on rc.ch, as the client might re-use the
			// same *Request pointer, and we don't want to set call
			// t.setReqCanceler from this persistConn while the Transport
			// potentially spins up a different persistConn for the
			// caller's subsequent request.
			if !pc.shouldRetryRequest(rc.req, err) {
				pc.t.setReqCanceler(rc.req, nil)
			}
			select {
			case rc.ch <- responseAndError{err: err}:
			case <-rc.callerGone:
				return
			}
			return
		}
		pc.readLimit = maxInt64 // effictively no limit for response bodies

		pc.mu.Lock()
		pc.numExpectedResponses--
		pc.mu.Unlock()

		hasBody := rc.req.Method != "HEAD" && resp.ContentLength != 0

		if resp.Close || rc.req.Close || resp.StatusCode <= 199 {
			// Don't do keep-alive on error if either party requested a close
			// or we get an unexpected informational (1xx) response.
			// StatusCode 100 is already handled above.
			alive = false
		}

		if !hasBody {
			pc.t.setReqCanceler(rc.req, nil)

			// Put the idle conn back into the pool before we send the response
			// so if they process it quickly and make another request, they'll
			// get this same conn. But we use the unbuffered channel 'rc'
			// to guarantee that persistConn.roundTrip got out of its select
			// potentially waiting for this persistConn to close.
			// but after
			alive = alive &&
				!pc.sawEOF &&
				pc.wroteRequest() &&
				tryPutIdleConn(trace)

			select {
			case rc.ch <- responseAndError{res: resp}:
			case <-rc.callerGone:
				return
			}

			// Now that they've read from the unbuffered channel, they're safely
			// out of the select that also waits on this goroutine to die, so
			// we're allowed to exit now if needed (if alive is false)
			testHookReadLoopBeforeNextRead()
			continue
		}

		waitForBodyRead := make(chan bool, 2)
		body := &bodyEOFSignal{
			body: resp.Body,
			earlyCloseFn: func() error {
				waitForBodyRead <- false
				return nil

			},
			fn: func(err error) error {
				isEOF := err == io.EOF
				waitForBodyRead <- isEOF
				if isEOF {
					<-eofc // see comment above eofc declaration
				} else if err != nil {
					if cerr := pc.canceled(); cerr != nil {
						return cerr
					}
				}
				return err
			},
		}

		resp.Body = body
		if rc.addedGzip && resp.Header.Get("Content-Encoding") == "gzip" {
			resp.Body = &gzipReader{body: body}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
			resp.Uncompressed = true
		}

		select {
		case rc.ch <- responseAndError{res: resp}:
		case <-rc.callerGone:
			return
		}

		// Before looping back to the top of this function and peeking on
		// the bufio.Reader, wait for the caller goroutine to finish
		// reading the response body. (or for cancelation or death)
		select {
		case bodyEOF := <-waitForBodyRead:
			pc.t.setReqCanceler(rc.req, nil) // before pc might return to idle pool
			alive = alive &&
				bodyEOF &&
				!pc.sawEOF &&
				pc.wroteRequest() &&
				tryPutIdleConn(trace)
			if bodyEOF {
				eofc <- struct{}{}
			}
		case <-rc.req.Cancel:
			alive = false
			pc.t.CancelRequest(rc.req)
		case <-rc.req.Context().Done():
			alive = false
			pc.t.cancelRequest(rc.req, rc.req.Context().Err())
		case <-pc.closech:
			alive = false
		}

		testHookReadLoopBeforeNextRead()
	}
}

func (pc *persistConn) readLoopPeekFailLocked(peekErr error) {
	if pc.closed != nil {
		return
	}
	if n := pc.br.Buffered(); n > 0 {
		buf, _ := pc.br.Peek(n)
		log.Printf("Unsolicited response received on idle HTTP channel starting with %q; err=%v", buf, peekErr)
	}
	if peekErr == io.EOF {
		// common case.
		pc.closeLocked(errServerClosedIdle)
	} else {
		pc.closeLocked(fmt.Errorf("readLoopPeekFailLocked: %v", peekErr))
	}
}

// readResponse reads an HTTP response (or two, in the case of "Expect:
// 100-continue") from the server. It returns the final non-100 one.
// trace is optional.
func (pc *persistConn) readResponse(rc requestAndChan, trace *httptrace.ClientTrace) (resp *Response, err error) {
	if trace != nil && trace.GotFirstResponseByte != nil {
		if peek, err := pc.br.Peek(1); err == nil && len(peek) == 1 {
			trace.GotFirstResponseByte()
		}
	}
	resp, err = ReadResponse(pc.br, rc.req)
	if err != nil {
		return
	}
	if rc.continueCh != nil {
		if resp.StatusCode == 100 {
			if trace != nil && trace.Got100Continue != nil {
				trace.Got100Continue()
			}
			rc.continueCh <- struct{}{}
		} else {
			close(rc.continueCh)
		}
	}
	if resp.StatusCode == 100 {
		pc.readLimit = pc.maxHeaderResponseSize() // reset the limit
		resp, err = ReadResponse(pc.br, rc.req)
		if err != nil {
			return
		}
	}
	resp.TLS = pc.tlsState
	return
}

// waitForContinue returns the function to block until
// any response, timeout or connection close. After any of them,
// the function returns a bool which indicates if the body should be sent.
func (pc *persistConn) waitForContinue(continueCh <-chan struct{}) func() bool {
	if continueCh == nil {
		return nil
	}
	return func() bool {
		timer := time.NewTimer(pc.t.ExpectContinueTimeout)
		defer timer.Stop()

		select {
		case _, ok := <-continueCh:
			return ok
		case <-timer.C:
			return true
		case <-pc.closech:
			return false
		}
	}
}

// nothingWrittenError wraps a write errors which ended up writing zero bytes.
type nothingWrittenError struct {
	error
}

func (pc *persistConn) writeLoop() {
	defer close(pc.writeLoopDone)
	for {
		select {
		case wr := <-pc.writech:
			startBytesWritten := pc.nwrite
			err := wr.req.Request.write(pc.bw, pc.isProxy, wr.req.extra, pc.waitForContinue(wr.continueCh))
			if err == nil {
				err = pc.bw.Flush()
			}
			if err != nil {
				wr.req.Request.closeBody()
				if pc.nwrite == startBytesWritten && wr.req.outgoingLength() == 0 {
					err = nothingWrittenError{err}
				}
			}
			pc.writeErrCh <- err // to the body reader, which might recycle us
			wr.ch <- err         // to the roundTrip function
			if err != nil {
				pc.close(err)
				return
			}
		case <-pc.closech:
			return
		}
	}
}

// wroteRequest is a check before recycling a connection that the previous write
// (from writeLoop above) happened and was successful.
func (pc *persistConn) wroteRequest() bool {
	select {
	case err := <-pc.writeErrCh:
		// Common case: the write happened well before the response, so
		// avoid creating a timer.
		return err == nil
	default:
		// Rare case: the request was written in writeLoop above but
		// before it could send to pc.writeErrCh, the reader read it
		// all, processed it, and called us here. In this case, give the
		// write goroutine a bit of time to finish its send.
		//
		// Less rare case: We also get here in the legitimate case of
		// Issue 7569, where the writer is still writing (or stalled),
		// but the server has already replied. In this case, we don't
		// want to wait too long, and we want to return false so this
		// connection isn't re-used.
		select {
		case err := <-pc.writeErrCh:
			return err == nil
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}
}

// responseAndError is how the goroutine reading from an HTTP/1 server
// communicates with the goroutine doing the RoundTrip.
type responseAndError struct {
	res *Response // else use this response (see res method)
	err error
}

type requestAndChan struct {
	req *Request
	ch  chan responseAndError // unbuffered; always send in select on callerGone

	// whether the Transport (as opposed to the user client code)
	// added the Accept-Encoding gzip header. If the Transport
	// set it, only then do we transparently decode the gzip.
	addedGzip bool

	// Optional blocking chan for Expect: 100-continue (for send).
	// If the request has an "Expect: 100-continue" header and
	// the server responds 100 Continue, readLoop send a value
	// to writeLoop via this chan.
	continueCh chan<- struct{}

	callerGone <-chan struct{} // closed when roundTrip caller has returned
}

// A writeRequest is sent by the readLoop's goroutine to the
// writeLoop's goroutine to write a request while the read loop
// concurrently waits on both the write response and the server's
// reply.
type writeRequest struct {
	req *transportRequest
	ch  chan<- error

	// Optional blocking chan for Expect: 100-continue (for receive).
	// If not nil, writeLoop blocks sending request body until
	// it receives from this chan.
	continueCh <-chan struct{}
}

type httpError struct {
	err     string
	timeout bool
}

func (e *httpError) Error() string   { return e.err }
func (e *httpError) Timeout() bool   { return e.timeout }
func (e *httpError) Temporary() bool { return true }

var errTimeout error = &httpError{err: "net/http: timeout awaiting response headers", timeout: true}
var errRequestCanceled = errors.New("net/http: request canceled")
var errRequestCanceledConn = errors.New("net/http: request canceled while waiting for connection") // TODO: unify?

func nop() {}

// testHooks. Always non-nil.
var (
	testHookEnterRoundTrip   = nop
	testHookWaitResLoop      = nop
	testHookRoundTripRetried = nop
	testHookPrePendingDial   = nop
	testHookPostPendingDial  = nop

	testHookMu                     sync.Locker = fakeLocker{} // guards following
	testHookReadLoopBeforeNextRead             = nop
)

func (pc *persistConn) roundTrip(req *transportRequest) (resp *Response, err error) {
	testHookEnterRoundTrip()
	if !pc.t.replaceReqCanceler(req.Request, pc.cancelRequest) {
		pc.t.putOrCloseIdleConn(pc)
		return nil, errRequestCanceled
	}
	pc.mu.Lock()
	pc.numExpectedResponses++
	headerFn := pc.mutateHeaderFunc
	pc.mu.Unlock()

	if headerFn != nil {
		headerFn(req.extraHeaders())
	}

	// Ask for a compressed version if the caller didn't set their
	// own value for Accept-Encoding. We only attempt to
	// uncompress the gzip stream if we were the layer that
	// requested it.
	requestedGzip := false
	if !pc.t.DisableCompression &&
		req.Header.Get("Accept-Encoding") == "" &&
		req.Header.Get("Range") == "" &&
		req.Method != "HEAD" {
		// Request gzip only, not deflate. Deflate is ambiguous and
		// not as universally supported anyway.
		// See: http://www.gzip.org/zlib/zlib_faq.html#faq38
		//
		// Note that we don't request this for HEAD requests,
		// due to a bug in nginx:
		//   http://trac.nginx.org/nginx/ticket/358
		//   https://golang.org/issue/5522
		//
		// We don't request gzip if the request is for a range, since
		// auto-decoding a portion of a gzipped document will just fail
		// anyway. See https://golang.org/issue/8923
		requestedGzip = true
		req.extraHeaders().Set("Accept-Encoding", "gzip")
	}

	var continueCh chan struct{}
	if req.ProtoAtLeast(1, 1) && req.Body != nil && req.expectsContinue() {
		continueCh = make(chan struct{}, 1)
	}

	if pc.t.DisableKeepAlives {
		req.extraHeaders().Set("Connection", "close")
	}

	gone := make(chan struct{})
	defer close(gone)

	// Write the request concurrently with waiting for a response,
	// in case the server decides to reply before reading our full
	// request body.
	startBytesWritten := pc.nwrite
	writeErrCh := make(chan error, 1)
	pc.writech <- writeRequest{req, writeErrCh, continueCh}

	resc := make(chan responseAndError)
	pc.reqch <- requestAndChan{
		req:        req.Request,
		ch:         resc,
		addedGzip:  requestedGzip,
		continueCh: continueCh,
		callerGone: gone,
	}

	var re responseAndError
	var respHeaderTimer <-chan time.Time
	cancelChan := req.Request.Cancel
	ctxDoneChan := req.Context().Done()
WaitResponse:
	for {
		testHookWaitResLoop()
		select {
		case err := <-writeErrCh:
			if err != nil {
				if cerr := pc.canceled(); cerr != nil {
					err = cerr
				}
				re = responseAndError{err: err}
				pc.close(fmt.Errorf("write error: %v", err))
				break WaitResponse
			}
			if d := pc.t.ResponseHeaderTimeout; d > 0 {
				timer := time.NewTimer(d)
				defer timer.Stop() // prevent leaks
				respHeaderTimer = timer.C
			}
		case <-pc.closech:
			re = responseAndError{err: pc.mapRoundTripErrorAfterClosed(req.Request, startBytesWritten)}
			break WaitResponse
		case <-respHeaderTimer:
			pc.close(errTimeout)
			re = responseAndError{err: errTimeout}
			break WaitResponse
		case re = <-resc:
			re.err = pc.mapRoundTripErrorFromReadLoop(req.Request, startBytesWritten, re.err)
			break WaitResponse
		case <-cancelChan:
			pc.t.CancelRequest(req.Request)
			cancelChan = nil
		case <-ctxDoneChan:
			pc.t.cancelRequest(req.Request, req.Context().Err())
			cancelChan = nil
			ctxDoneChan = nil
		}
	}

	if re.err != nil {
		pc.t.setReqCanceler(req.Request, nil)
	}
	if (re.res == nil) == (re.err == nil) {
		panic("internal error: exactly one of res or err should be set")
	}
	return re.res, re.err
}

// markReused marks this connection as having been successfully used for a
// request and response.
func (pc *persistConn) markReused() {
	pc.mu.Lock()
	pc.reused = true
	pc.mu.Unlock()
}

// close closes the underlying TCP connection and closes
// the pc.closech channel.
//
// The provided err is only for testing and debugging; in normal
// circumstances it should never be seen by users.
func (pc *persistConn) close(err error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.closeLocked(err)
}

func (pc *persistConn) closeLocked(err error) {
	if err == nil {
		panic("nil error")
	}
	pc.broken = true
	if pc.closed == nil {
		pc.closed = err
		if pc.alt != nil {
			// Do nothing; can only get here via getConn's
			// handlePendingDial's putOrCloseIdleConn when
			// it turns out the abandoned connection in
			// flight ended up negotiating an alternate
			// protocol. We don't use the connection
			// freelist for http2. That's done by the
			// alternate protocol's RoundTripper.
		} else {
			pc.conn.Close()
			close(pc.closech)
		}
	}
	pc.mutateHeaderFunc = nil
}

var portMap = map[string]string{
	"http":  "80",
	"https": "443",
}

// canonicalAddr returns url.Host but always with a ":port" suffix
func canonicalAddr(url *url.URL) string {
	addr := url.Hostname()
	if v, err := idnaASCII(addr); err == nil {
		addr = v
	}
	port := url.Port()
	if port == "" {
		port = portMap[url.Scheme]
	}
	return net.JoinHostPort(addr, port)
}

// bodyEOFSignal is used by the HTTP/1 transport when reading response
// bodies to make sure we see the end of a response body before
// proceeding and reading on the connection again.
//
// It wraps a ReadCloser but runs fn (if non-nil) at most
// once, right before its final (error-producing) Read or Close call
// returns. fn should return the new error to return from Read or Close.
//
// If earlyCloseFn is non-nil and Close is called before io.EOF is
// seen, earlyCloseFn is called instead of fn, and its return value is
// the return value from Close.
type bodyEOFSignal struct {
	body         io.ReadCloser
	mu           sync.Mutex        // guards following 4 fields
	closed       bool              // whether Close has been called
	rerr         error             // sticky Read error
	fn           func(error) error // err will be nil on Read io.EOF
	earlyCloseFn func() error      // optional alt Close func used if io.EOF not seen
}

var errReadOnClosedResBody = errors.New("http: read on closed response body")

func (es *bodyEOFSignal) Read(p []byte) (n int, err error) {
	es.mu.Lock()
	closed, rerr := es.closed, es.rerr
	es.mu.Unlock()
	if closed {
		return 0, errReadOnClosedResBody
	}
	if rerr != nil {
		return 0, rerr
	}

	n, err = es.body.Read(p)
	if err != nil {
		es.mu.Lock()
		defer es.mu.Unlock()
		if es.rerr == nil {
			es.rerr = err
		}
		err = es.condfn(err)
	}
	return
}

func (es *bodyEOFSignal) Close() error {
	es.mu.Lock()
	defer es.mu.Unlock()
	if es.closed {
		return nil
	}
	es.closed = true
	if es.earlyCloseFn != nil && es.rerr != io.EOF {
		return es.earlyCloseFn()
	}
	err := es.body.Close()
	return es.condfn(err)
}

// caller must hold es.mu.
func (es *bodyEOFSignal) condfn(err error) error {
	if es.fn == nil {
		return err
	}
	err = es.fn(err)
	es.fn = nil
	return err
}

// gzipReader wraps a response body so it can lazily
// call gzip.NewReader on the first call to Read
type gzipReader struct {
	body *bodyEOFSignal // underlying HTTP/1 response body framing
	zr   *gzip.Reader   // lazily-initialized gzip reader
	zerr error          // any error from gzip.NewReader; sticky
}

func (gz *gzipReader) Read(p []byte) (n int, err error) {
	if gz.zr == nil {
		if gz.zerr == nil {
			gz.zr, gz.zerr = gzip.NewReader(gz.body)
		}
		if gz.zerr != nil {
			return 0, gz.zerr
		}
	}

	gz.body.mu.Lock()
	if gz.body.closed {
		err = errReadOnClosedResBody
	}
	gz.body.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return gz.zr.Read(p)
}

func (gz *gzipReader) Close() error {
	return gz.body.Close()
}

type readerAndCloser struct {
	io.Reader
	io.Closer
}

type tlsHandshakeTimeoutError struct{}

func (tlsHandshakeTimeoutError) Timeout() bool   { return true }
func (tlsHandshakeTimeoutError) Temporary() bool { return true }
func (tlsHandshakeTimeoutError) Error() string   { return "net/http: TLS handshake timeout" }

// fakeLocker is a sync.Locker which does nothing. It's used to guard
// test-only fields when not under test, to avoid runtime atomic
// overhead.
type fakeLocker struct{}

func (fakeLocker) Lock()   {}
func (fakeLocker) Unlock() {}

// clneTLSConfig returns a shallow clone of cfg, or a new zero tls.Config if
// cfg is nil. This is safe to call even if cfg is in active use by a TLS
// client or server.
func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return &tls.Config{}
	}
	return cfg.Clone()
}

type connLRU struct {
	ll *list.List // list.Element.Value type is of *persistConn
	m  map[*persistConn]*list.Element
}

// add adds pc to the head of the linked list.
func (cl *connLRU) add(pc *persistConn) {
	if cl.ll == nil {
		cl.ll = list.New()
		cl.m = make(map[*persistConn]*list.Element)
	}
	ele := cl.ll.PushFront(pc)
	if _, ok := cl.m[pc]; ok {
		panic("persistConn was already in LRU")
	}
	cl.m[pc] = ele
}

func (cl *connLRU) removeOldest() *persistConn {
	ele := cl.ll.Back()
	pc := ele.Value.(*persistConn)
	cl.ll.Remove(ele)
	delete(cl.m, pc)
	return pc
}

// remove removes pc from cl.
func (cl *connLRU) remove(pc *persistConn) {
	if ele, ok := cl.m[pc]; ok {
		cl.ll.Remove(ele)
		delete(cl.m, pc)
	}
}

// len returns the number of items in the cache.
func (cl *connLRU) len() int {
	return len(cl.m)
}

// validPort reports whether p (without the colon) is a valid port in
// a URL, per RFC 3986 Section 3.2.3, which says the port may be
// empty, or only contain digits.
func validPort(p string) bool {
	for _, r := range []byte(p) {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Code generated by golang.org/x/tools/cmd/bundle. DO NOT EDIT.
//go:generate bundle -o h2_bundle.go -prefix http2 -underscore golang.org/x/net/http2

// Package http2 implements the HTTP/2 protocol.
//
// This package is low-level and intended to be used directly by very
// few people. Most users will use it indirectly through the automatic
// use by the net/http package (from Go 1.6 and later).
// For use in earlier Go versions see ConfigureServer. (Transport support
// requires Go 1.6 or later)
//
// See https://http2.github.io/ for more information on HTTP/2.
//
// See https://http2.golang.org/ for a test server running this code.
//

package http

import (
"bufio"
"bytes"
"compress/gzip"
"context"
"crypto/rand"
"crypto/tls"
"encoding/binary"
"errors"
"fmt"
"io"
"io/ioutil"
"log"
"math"
"net"
"net/http/httptrace"
"net/textproto"
"net/url"
"os"
"reflect"
"runtime"
"sort"
"strconv"
"strings"
"sync"
"time"

"golang_org/x/net/http2/hpack"
"golang_org/x/net/idna"
"golang_org/x/net/lex/httplex"
)

// ClientConnPool manages a pool of HTTP/2 client connections.
type http2ClientConnPool interface {
	GetClientConn(req *Request, addr string) (*http2ClientConn, error)
	MarkDead(*http2ClientConn)
}

// clientConnPoolIdleCloser is the interface implemented by ClientConnPool
// implementations which can close their idle connections.
type http2clientConnPoolIdleCloser interface {
	http2ClientConnPool
	closeIdleConnections()
}

var (
	_ http2clientConnPoolIdleCloser = (*http2clientConnPool)(nil)
	_ http2clientConnPoolIdleCloser = http2noDialClientConnPool{}
)

// TODO: use singleflight for dialing and addConnCalls?
type http2clientConnPool struct {
	t *http2Transport

	mu sync.Mutex // TODO: maybe switch to RWMutex
	// TODO: add support for sharing conns based on cert names
	// (e.g. share conn for googleapis.com and appspot.com)
	conns        map[string][]*http2ClientConn // key is host:port
	dialing      map[string]*http2dialCall     // currently in-flight dials
	keys         map[*http2ClientConn][]string
	addConnCalls map[string]*http2addConnCall // in-flight addConnIfNeede calls
}

func (p *http2clientConnPool) GetClientConn(req *Request, addr string) (*http2ClientConn, error) {
	return p.getClientConn(req, addr, http2dialOnMiss)
}

const (
	http2dialOnMiss   = true
	http2noDialOnMiss = false
)

func (p *http2clientConnPool) getClientConn(req *Request, addr string, dialOnMiss bool) (*http2ClientConn, error) {
	if http2isConnectionCloseRequest(req) && dialOnMiss {
		// It gets its own connection.
		const singleUse = true
		cc, err := p.t.dialClientConn(addr, singleUse)
		if err != nil {
			return nil, err
		}
		return cc, nil
	}
	p.mu.Lock()
	for _, cc := range p.conns[addr] {
		if cc.CanTakeNewRequest() {
			p.mu.Unlock()
			return cc, nil
		}
	}
	if !dialOnMiss {
		p.mu.Unlock()
		return nil, http2ErrNoCachedConn
	}
	call := p.getStartDialLocked(addr)
	p.mu.Unlock()
	<-call.done
	return call.res, call.err
}

// dialCall is an in-flight Transport dial call to a host.
type http2dialCall struct {
	p    *http2clientConnPool
	done chan struct{}    // closed when done
	res  *http2ClientConn // valid after done is closed
	err  error            // valid after done is closed
}

// requires p.mu is held.
func (p *http2clientConnPool) getStartDialLocked(addr string) *http2dialCall {
	if call, ok := p.dialing[addr]; ok {

		return call
	}
	call := &http2dialCall{p: p, done: make(chan struct{})}
	if p.dialing == nil {
		p.dialing = make(map[string]*http2dialCall)
	}
	p.dialing[addr] = call
	go call.dial(addr)
	return call
}

// run in its own goroutine.
func (c *http2dialCall) dial(addr string) {
	const singleUse = false // shared conn
	c.res, c.err = c.p.t.dialClientConn(addr, singleUse)
	close(c.done)

	c.p.mu.Lock()
	delete(c.p.dialing, addr)
	if c.err == nil {
		c.p.addConnLocked(addr, c.res)
	}
	c.p.mu.Unlock()
}

// addConnIfNeeded makes a NewClientConn out of c if a connection for key doesn't
// already exist. It coalesces concurrent calls with the same key.
// This is used by the http1 Transport code when it creates a new connection. Because
// the http1 Transport doesn't de-dup TCP dials to outbound hosts (because it doesn't know
// the protocol), it can get into a situation where it has multiple TLS connections.
// This code decides which ones live or die.
// The return value used is whether c was used.
// c is never closed.
func (p *http2clientConnPool) addConnIfNeeded(key string, t *http2Transport, c *tls.Conn) (used bool, err error) {
	p.mu.Lock()
	for _, cc := range p.conns[key] {
		if cc.CanTakeNewRequest() {
			p.mu.Unlock()
			return false, nil
		}
	}
	call, dup := p.addConnCalls[key]
	if !dup {
		if p.addConnCalls == nil {
			p.addConnCalls = make(map[string]*http2addConnCall)
		}
		call = &http2addConnCall{
			p:    p,
			done: make(chan struct{}),
		}
		p.addConnCalls[key] = call
		go call.run(t, key, c)
	}
	p.mu.Unlock()

	<-call.done
	if call.err != nil {
		return false, call.err
	}
	return !dup, nil
}

type http2addConnCall struct {
	p    *http2clientConnPool
	done chan struct{} // closed when done
	err  error
}

func (c *http2addConnCall) run(t *http2Transport, key string, tc *tls.Conn) {
	cc, err := t.NewClientConn(tc)

	p := c.p
	p.mu.Lock()
	if err != nil {
		c.err = err
	} else {
		p.addConnLocked(key, cc)
	}
	delete(p.addConnCalls, key)
	p.mu.Unlock()
	close(c.done)
}

func (p *http2clientConnPool) addConn(key string, cc *http2ClientConn) {
	p.mu.Lock()
	p.addConnLocked(key, cc)
	p.mu.Unlock()
}

// p.mu must be held
func (p *http2clientConnPool) addConnLocked(key string, cc *http2ClientConn) {
	for _, v := range p.conns[key] {
		if v == cc {
			return
		}
	}
	if p.conns == nil {
		p.conns = make(map[string][]*http2ClientConn)
	}
	if p.keys == nil {
		p.keys = make(map[*http2ClientConn][]string)
	}
	p.conns[key] = append(p.conns[key], cc)
	p.keys[cc] = append(p.keys[cc], key)
}

func (p *http2clientConnPool) MarkDead(cc *http2ClientConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, key := range p.keys[cc] {
		vv, ok := p.conns[key]
		if !ok {
			continue
		}
		newList := http2filterOutClientConn(vv, cc)
		if len(newList) > 0 {
			p.conns[key] = newList
		} else {
			delete(p.conns, key)
		}
	}
	delete(p.keys, cc)
}

func (p *http2clientConnPool) closeIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, vv := range p.conns {
		for _, cc := range vv {
			cc.closeIfIdle()
		}
	}
}

func http2filterOutClientConn(in []*http2ClientConn, exclude *http2ClientConn) []*http2ClientConn {
	out := in[:0]
	for _, v := range in {
		if v != exclude {
			out = append(out, v)
		}
	}

	if len(in) != len(out) {
		in[len(in)-1] = nil
	}
	return out
}

// noDialClientConnPool is an implementation of http2.ClientConnPool
// which never dials.  We let the HTTP/1.1 client dial and use its TLS
// connection instead.
type http2noDialClientConnPool struct{ *http2clientConnPool }

func (p http2noDialClientConnPool) GetClientConn(req *Request, addr string) (*http2ClientConn, error) {
	return p.getClientConn(req, addr, http2noDialOnMiss)
}

func http2configureTransport(t1 *Transport) (*http2Transport, error) {
	connPool := new(http2clientConnPool)
	t2 := &http2Transport{
		ConnPool: http2noDialClientConnPool{connPool},
		t1:       t1,
	}
	connPool.t = t2
	if err := http2registerHTTPSProtocol(t1, http2noDialH2RoundTripper{t2}); err != nil {
		return nil, err
	}
	if t1.TLSClientConfig == nil {
		t1.TLSClientConfig = new(tls.Config)
	}
	if !http2strSliceContains(t1.TLSClientConfig.NextProtos, "h2") {
		t1.TLSClientConfig.NextProtos = append([]string{"h2"}, t1.TLSClientConfig.NextProtos...)
	}
	if !http2strSliceContains(t1.TLSClientConfig.NextProtos, "http/1.1") {
		t1.TLSClientConfig.NextProtos = append(t1.TLSClientConfig.NextProtos, "http/1.1")
	}
	upgradeFn := func(authority string, c *tls.Conn) RoundTripper {
		addr := http2authorityAddr("https", authority)
		if used, err := connPool.addConnIfNeeded(addr, t2, c); err != nil {
			go c.Close()
			return http2erringRoundTripper{err}
		} else if !used {

			go c.Close()
		}
		return t2
	}
	if m := t1.TLSNextProto; len(m) == 0 {
		t1.TLSNextProto = map[string]func(string, *tls.Conn) RoundTripper{
			"h2": upgradeFn,
		}
	} else {
		m["h2"] = upgradeFn
	}
	return t2, nil
}

// registerHTTPSProtocol calls Transport.RegisterProtocol but
// convering panics into errors.
func http2registerHTTPSProtocol(t *Transport, rt RoundTripper) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()
	t.RegisterProtocol("https", rt)
	return nil
}

// noDialH2RoundTripper is a RoundTripper which only tries to complete the request
// if there's already has a cached connection to the host.
type http2noDialH2RoundTripper struct{ t *http2Transport }

func (rt http2noDialH2RoundTripper) RoundTrip(req *Request) (*Response, error) {
	res, err := rt.t.RoundTrip(req)
	if err == http2ErrNoCachedConn {
		return nil, ErrSkipAltProtocol
	}
	return res, err
}

// An ErrCode is an unsigned 32-bit error code as defined in the HTTP/2 spec.
type http2ErrCode uint32

const (
	http2ErrCodeNo                 http2ErrCode = 0x0
	http2ErrCodeProtocol           http2ErrCode = 0x1
	http2ErrCodeInternal           http2ErrCode = 0x2
	http2ErrCodeFlowControl        http2ErrCode = 0x3
	http2ErrCodeSettingsTimeout    http2ErrCode = 0x4
	http2ErrCodeStreamClosed       http2ErrCode = 0x5
	http2ErrCodeFrameSize          http2ErrCode = 0x6
	http2ErrCodeRefusedStream      http2ErrCode = 0x7
	http2ErrCodeCancel             http2ErrCode = 0x8
	http2ErrCodeCompression        http2ErrCode = 0x9
	http2ErrCodeConnect            http2ErrCode = 0xa
	http2ErrCodeEnhanceYourCalm    http2ErrCode = 0xb
	http2ErrCodeInadequateSecurity http2ErrCode = 0xc
	http2ErrCodeHTTP11Required     http2ErrCode = 0xd
)

var http2errCodeName = map[http2ErrCode]string{
	http2ErrCodeNo:                 "NO_ERROR",
	http2ErrCodeProtocol:           "PROTOCOL_ERROR",
	http2ErrCodeInternal:           "INTERNAL_ERROR",
	http2ErrCodeFlowControl:        "FLOW_CONTROL_ERROR",
	http2ErrCodeSettingsTimeout:    "SETTINGS_TIMEOUT",
	http2ErrCodeStreamClosed:       "STREAM_CLOSED",
	http2ErrCodeFrameSize:          "FRAME_SIZE_ERROR",
	http2ErrCodeRefusedStream:      "REFUSED_STREAM",
	http2ErrCodeCancel:             "CANCEL",
	http2ErrCodeCompression:        "COMPRESSION_ERROR",
	http2ErrCodeConnect:            "CONNECT_ERROR",
	http2ErrCodeEnhanceYourCalm:    "ENHANCE_YOUR_CALM",
	http2ErrCodeInadequateSecurity: "INADEQUATE_SECURITY",
	http2ErrCodeHTTP11Required:     "HTTP_1_1_REQUIRED",
}

func (e http2ErrCode) String() string {
	if s, ok := http2errCodeName[e]; ok {
		return s
	}
	return fmt.Sprintf("unknown error code 0x%x", uint32(e))
}

// ConnectionError is an error that results in the termination of the
// entire connection.
type http2ConnectionError http2ErrCode

func (e http2ConnectionError) Error() string {
	return fmt.Sprintf("connection error: %s", http2ErrCode(e))
}

// StreamError is an error that only affects one stream within an
// HTTP/2 connection.
type http2StreamError struct {
	StreamID uint32
	Code     http2ErrCode
	Cause    error // optional additional detail
}

func http2streamError(id uint32, code http2ErrCode) http2StreamError {
	return http2StreamError{StreamID: id, Code: code}
}

func (e http2StreamError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("stream error: stream ID %d; %v; %v", e.StreamID, e.Code, e.Cause)
	}
	return fmt.Sprintf("stream error: stream ID %d; %v", e.StreamID, e.Code)
}

// 6.9.1 The Flow Control Window
// "If a sender receives a WINDOW_UPDATE that causes a flow control
// window to exceed this maximum it MUST terminate either the stream
// or the connection, as appropriate. For streams, [...]; for the
// connection, a GOAWAY frame with a FLOW_CONTROL_ERROR code."
type http2goAwayFlowError struct{}

func (http2goAwayFlowError) Error() string { return "connection exceeded flow control window size" }

// Errors of this type are only returned by the frame parser functions
// and converted into ConnectionError(ErrCodeProtocol).
type http2connError struct {
	Code   http2ErrCode
	Reason string
}

func (e http2connError) Error() string {
	return fmt.Sprintf("http2: connection error: %v: %v", e.Code, e.Reason)
}

type http2pseudoHeaderError string

func (e http2pseudoHeaderError) Error() string {
	return fmt.Sprintf("invalid pseudo-header %q", string(e))
}

type http2duplicatePseudoHeaderError string

func (e http2duplicatePseudoHeaderError) Error() string {
	return fmt.Sprintf("duplicate pseudo-header %q", string(e))
}

type http2headerFieldNameError string

func (e http2headerFieldNameError) Error() string {
	return fmt.Sprintf("invalid header field name %q", string(e))
}

type http2headerFieldValueError string

func (e http2headerFieldValueError) Error() string {
	return fmt.Sprintf("invalid header field value %q", string(e))
}

var (
	http2errMixPseudoHeaderTypes = errors.New("mix of request and response pseudo headers")
	http2errPseudoAfterRegular   = errors.New("pseudo header field after regular")
)

// fixedBuffer is an io.ReadWriter backed by a fixed size buffer.
// It never allocates, but moves old data as new data is written.
type http2fixedBuffer struct {
	buf  []byte
	r, w int
}

var (
	http2errReadEmpty = errors.New("read from empty fixedBuffer")
	http2errWriteFull = errors.New("write on full fixedBuffer")
)

// Read copies bytes from the buffer into p.
// It is an error to read when no data is available.
func (b *http2fixedBuffer) Read(p []byte) (n int, err error) {
	if b.r == b.w {
		return 0, http2errReadEmpty
	}
	n = copy(p, b.buf[b.r:b.w])
	b.r += n
	if b.r == b.w {
		b.r = 0
		b.w = 0
	}
	return n, nil
}

// Len returns the number of bytes of the unread portion of the buffer.
func (b *http2fixedBuffer) Len() int {
	return b.w - b.r
}

// Write copies bytes from p into the buffer.
// It is an error to write more data than the buffer can hold.
func (b *http2fixedBuffer) Write(p []byte) (n int, err error) {

	if b.r > 0 && len(p) > len(b.buf)-b.w {
		copy(b.buf, b.buf[b.r:b.w])
		b.w -= b.r
		b.r = 0
	}

	n = copy(b.buf[b.w:], p)
	b.w += n
	if n < len(p) {
		err = http2errWriteFull
	}
	return n, err
}

// flow is the flow control window's size.
type http2flow struct {
	// n is the number of DATA bytes we're allowed to send.
	// A flow is kept both on a conn and a per-stream.
	n int32

	// conn points to the shared connection-level flow that is
	// shared by all streams on that conn. It is nil for the flow
	// that's on the conn directly.
	conn *http2flow
}

func (f *http2flow) setConnFlow(cf *http2flow) { f.conn = cf }

func (f *http2flow) available() int32 {
	n := f.n
	if f.conn != nil && f.conn.n < n {
		n = f.conn.n
	}
	return n
}

func (f *http2flow) take(n int32) {
	if n > f.available() {
		panic("internal error: took too much")
	}
	f.n -= n
	if f.conn != nil {
		f.conn.n -= n
	}
}

// add adds n bytes (positive or negative) to the flow control window.
// It returns false if the sum would exceed 2^31-1.
func (f *http2flow) add(n int32) bool {
	remain := (1<<31 - 1) - f.n
	if n > remain {
		return false
	}
	f.n += n
	return true
}

const http2frameHeaderLen = 9

var http2padZeros = make([]byte, 255) // zeros for padding

// A FrameType is a registered frame type as defined in
// http://http2.github.io/http2-spec/#rfc.section.11.2
type http2FrameType uint8

const (
	http2FrameData         http2FrameType = 0x0
	http2FrameHeaders      http2FrameType = 0x1
	http2FramePriority     http2FrameType = 0x2
	http2FrameRSTStream    http2FrameType = 0x3
	http2FrameSettings     http2FrameType = 0x4
	http2FramePushPromise  http2FrameType = 0x5
	http2FramePing         http2FrameType = 0x6
	http2FrameGoAway       http2FrameType = 0x7
	http2FrameWindowUpdate http2FrameType = 0x8
	http2FrameContinuation http2FrameType = 0x9
)

var http2frameName = map[http2FrameType]string{
	http2FrameData:         "DATA",
	http2FrameHeaders:      "HEADERS",
	http2FramePriority:     "PRIORITY",
	http2FrameRSTStream:    "RST_STREAM",
	http2FrameSettings:     "SETTINGS",
	http2FramePushPromise:  "PUSH_PROMISE",
	http2FramePing:         "PING",
	http2FrameGoAway:       "GOAWAY",
	http2FrameWindowUpdate: "WINDOW_UPDATE",
	http2FrameContinuation: "CONTINUATION",
}

func (t http2FrameType) String() string {
	if s, ok := http2frameName[t]; ok {
		return s
	}
	return fmt.Sprintf("UNKNOWN_FRAME_TYPE_%d", uint8(t))
}

// Flags is a bitmask of HTTP/2 flags.
// The meaning of flags varies depending on the frame type.
type http2Flags uint8

// Has reports whether f contains all (0 or more) flags in v.
func (f http2Flags) Has(v http2Flags) bool {
	return (f & v) == v
}

// Frame-specific FrameHeader flag bits.
const (
	// Data Frame
	http2FlagDataEndStream http2Flags = 0x1
	http2FlagDataPadded    http2Flags = 0x8

	// Headers Frame
	http2FlagHeadersEndStream  http2Flags = 0x1
	http2FlagHeadersEndHeaders http2Flags = 0x4
	http2FlagHeadersPadded     http2Flags = 0x8
	http2FlagHeadersPriority   http2Flags = 0x20

	// Settings Frame
	http2FlagSettingsAck http2Flags = 0x1

	// Ping Frame
	http2FlagPingAck http2Flags = 0x1

	// Continuation Frame
	http2FlagContinuationEndHeaders http2Flags = 0x4

	http2FlagPushPromiseEndHeaders http2Flags = 0x4
	http2FlagPushPromisePadded     http2Flags = 0x8
)

var http2flagName = map[http2FrameType]map[http2Flags]string{
	http2FrameData: {
		http2FlagDataEndStream: "END_STREAM",
		http2FlagDataPadded:    "PADDED",
	},
	http2FrameHeaders: {
		http2FlagHeadersEndStream:  "END_STREAM",
		http2FlagHeadersEndHeaders: "END_HEADERS",
		http2FlagHeadersPadded:     "PADDED",
		http2FlagHeadersPriority:   "PRIORITY",
	},
	http2FrameSettings: {
		http2FlagSettingsAck: "ACK",
	},
	http2FramePing: {
		http2FlagPingAck: "ACK",
	},
	http2FrameContinuation: {
		http2FlagContinuationEndHeaders: "END_HEADERS",
	},
	http2FramePushPromise: {
		http2FlagPushPromiseEndHeaders: "END_HEADERS",
		http2FlagPushPromisePadded:     "PADDED",
	},
}

// a frameParser parses a frame given its FrameHeader and payload
// bytes. The length of payload will always equal fh.Length (which
// might be 0).
type http2frameParser func(fh http2FrameHeader, payload []byte) (http2Frame, error)

var http2frameParsers = map[http2FrameType]http2frameParser{
	http2FrameData:         http2parseDataFrame,
	http2FrameHeaders:      http2parseHeadersFrame,
	http2FramePriority:     http2parsePriorityFrame,
	http2FrameRSTStream:    http2parseRSTStreamFrame,
	http2FrameSettings:     http2parseSettingsFrame,
	http2FramePushPromise:  http2parsePushPromise,
	http2FramePing:         http2parsePingFrame,
	http2FrameGoAway:       http2parseGoAwayFrame,
	http2FrameWindowUpdate: http2parseWindowUpdateFrame,
	http2FrameContinuation: http2parseContinuationFrame,
}

func http2typeFrameParser(t http2FrameType) http2frameParser {
	if f := http2frameParsers[t]; f != nil {
		return f
	}
	return http2parseUnknownFrame
}

// A FrameHeader is the 9 byte header of all HTTP/2 frames.
//
// See http://http2.github.io/http2-spec/#FrameHeader
type http2FrameHeader struct {
	valid bool // caller can access []byte fields in the Frame

	// Type is the 1 byte frame type. There are ten standard frame
	// types, but extension frame types may be written by WriteRawFrame
	// and will be returned by ReadFrame (as UnknownFrame).
	Type http2FrameType

	// Flags are the 1 byte of 8 potential bit flags per frame.
	// They are specific to the frame type.
	Flags http2Flags

	// Length is the length of the frame, not including the 9 byte header.
	// The maximum size is one byte less than 16MB (uint24), but only
	// frames up to 16KB are allowed without peer agreement.
	Length uint32

	// StreamID is which stream this frame is for. Certain frames
	// are not stream-specific, in which case this field is 0.
	StreamID uint32
}

// Header returns h. It exists so FrameHeaders can be embedded in other
// specific frame types and implement the Frame interface.
func (h http2FrameHeader) Header() http2FrameHeader { return h }

func (h http2FrameHeader) String() string {
	var buf bytes.Buffer
	buf.WriteString("[FrameHeader ")
	h.writeDebug(&buf)
	buf.WriteByte(']')
	return buf.String()
}

func (h http2FrameHeader) writeDebug(buf *bytes.Buffer) {
	buf.WriteString(h.Type.String())
	if h.Flags != 0 {
		buf.WriteString(" flags=")
		set := 0
		for i := uint8(0); i < 8; i++ {
			if h.Flags&(1<<i) == 0 {
				continue
			}
			set++
			if set > 1 {
				buf.WriteByte('|')
			}
			name := http2flagName[h.Type][http2Flags(1<<i)]
			if name != "" {
				buf.WriteString(name)
			} else {
				fmt.Fprintf(buf, "0x%x", 1<<i)
			}
		}
	}
	if h.StreamID != 0 {
		fmt.Fprintf(buf, " stream=%d", h.StreamID)
	}
	fmt.Fprintf(buf, " len=%d", h.Length)
}

func (h *http2FrameHeader) checkValid() {
	if !h.valid {
		panic("Frame accessor called on non-owned Frame")
	}
}

func (h *http2FrameHeader) invalidate() { h.valid = false }

// frame header bytes.
// Used only by ReadFrameHeader.
var http2fhBytes = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, http2frameHeaderLen)
		return &buf
	},
}

// ReadFrameHeader reads 9 bytes from r and returns a FrameHeader.
// Most users should use Framer.ReadFrame instead.
func http2ReadFrameHeader(r io.Reader) (http2FrameHeader, error) {
	bufp := http2fhBytes.Get().(*[]byte)
	defer http2fhBytes.Put(bufp)
	return http2readFrameHeader(*bufp, r)
}

func http2readFrameHeader(buf []byte, r io.Reader) (http2FrameHeader, error) {
	_, err := io.ReadFull(r, buf[:http2frameHeaderLen])
	if err != nil {
		return http2FrameHeader{}, err
	}
	return http2FrameHeader{
		Length:   (uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])),
		Type:     http2FrameType(buf[3]),
		Flags:    http2Flags(buf[4]),
		StreamID: binary.BigEndian.Uint32(buf[5:]) & (1<<31 - 1),
		valid:    true,
	}, nil
}

// A Frame is the base interface implemented by all frame types.
// Callers will generally type-assert the specific frame type:
// *HeadersFrame, *SettingsFrame, *WindowUpdateFrame, etc.
//
// Frames are only valid until the next call to Framer.ReadFrame.
type http2Frame interface {
	Header() http2FrameHeader

	// invalidate is called by Framer.ReadFrame to make this
	// frame's buffers as being invalid, since the subsequent
	// frame will reuse them.
	invalidate()
}

// A Framer reads and writes Frames.
type http2Framer struct {
	r         io.Reader
	lastFrame http2Frame
	errDetail error

	// lastHeaderStream is non-zero if the last frame was an
	// unfinished HEADERS/CONTINUATION.
	lastHeaderStream uint32

	maxReadSize uint32
	headerBuf   [http2frameHeaderLen]byte

	// TODO: let getReadBuf be configurable, and use a less memory-pinning
	// allocator in server.go to minimize memory pinned for many idle conns.
	// Will probably also need to make frame invalidation have a hook too.
	getReadBuf func(size uint32) []byte
	readBuf    []byte // cache for default getReadBuf

	maxWriteSize uint32 // zero means unlimited; TODO: implement

	w    io.Writer
	wbuf []byte

	// AllowIllegalWrites permits the Framer's Write methods to
	// write frames that do not conform to the HTTP/2 spec. This
	// permits using the Framer to test other HTTP/2
	// implementations' conformance to the spec.
	// If false, the Write methods will prefer to return an error
	// rather than comply.
	AllowIllegalWrites bool

	// AllowIllegalReads permits the Framer's ReadFrame method
	// to return non-compliant frames or frame orders.
	// This is for testing and permits using the Framer to test
	// other HTTP/2 implementations' conformance to the spec.
	// It is not compatible with ReadMetaHeaders.
	AllowIllegalReads bool

	// ReadMetaHeaders if non-nil causes ReadFrame to merge
	// HEADERS and CONTINUATION frames together and return
	// MetaHeadersFrame instead.
	ReadMetaHeaders *hpack.Decoder

	// MaxHeaderListSize is the http2 MAX_HEADER_LIST_SIZE.
	// It's used only if ReadMetaHeaders is set; 0 means a sane default
	// (currently 16MB)
	// If the limit is hit, MetaHeadersFrame.Truncated is set true.
	MaxHeaderListSize uint32

	logReads, logWrites bool

	debugFramer       *http2Framer // only use for logging written writes
	debugFramerBuf    *bytes.Buffer
	debugReadLoggerf  func(string, ...interface{})
	debugWriteLoggerf func(string, ...interface{})
}

func (fr *http2Framer) maxHeaderListSize() uint32 {
	if fr.MaxHeaderListSize == 0 {
		return 16 << 20
	}
	return fr.MaxHeaderListSize
}

func (f *http2Framer) startWrite(ftype http2FrameType, flags http2Flags, streamID uint32) {

	f.wbuf = append(f.wbuf[:0],
		0,
		0,
		0,
		byte(ftype),
		byte(flags),
		byte(streamID>>24),
		byte(streamID>>16),
		byte(streamID>>8),
		byte(streamID))
}

func (f *http2Framer) endWrite() error {

	length := len(f.wbuf) - http2frameHeaderLen
	if length >= (1 << 24) {
		return http2ErrFrameTooLarge
	}
	_ = append(f.wbuf[:0],
		byte(length>>16),
		byte(length>>8),
		byte(length))
	if f.logWrites {
		f.logWrite()
	}

	n, err := f.w.Write(f.wbuf)
	if err == nil && n != len(f.wbuf) {
		err = io.ErrShortWrite
	}
	return err
}

func (f *http2Framer) logWrite() {
	if f.debugFramer == nil {
		f.debugFramerBuf = new(bytes.Buffer)
		f.debugFramer = http2NewFramer(nil, f.debugFramerBuf)
		f.debugFramer.logReads = false

		f.debugFramer.AllowIllegalReads = true
	}
	f.debugFramerBuf.Write(f.wbuf)
	fr, err := f.debugFramer.ReadFrame()
	if err != nil {
		f.debugWriteLoggerf("http2: Framer %p: failed to decode just-written frame", f)
		return
	}
	f.debugWriteLoggerf("http2: Framer %p: wrote %v", f, http2summarizeFrame(fr))
}

func (f *http2Framer) writeByte(v byte) { f.wbuf = append(f.wbuf, v) }

func (f *http2Framer) writeBytes(v []byte) { f.wbuf = append(f.wbuf, v...) }

func (f *http2Framer) writeUint16(v uint16) { f.wbuf = append(f.wbuf, byte(v>>8), byte(v)) }

func (f *http2Framer) writeUint32(v uint32) {
	f.wbuf = append(f.wbuf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

const (
	http2minMaxFrameSize = 1 << 14
	http2maxFrameSize    = 1<<24 - 1
)

// NewFramer returns a Framer that writes frames to w and reads them from r.
func http2NewFramer(w io.Writer, r io.Reader) *http2Framer {
	fr := &http2Framer{
		w:                 w,
		r:                 r,
		logReads:          http2logFrameReads,
		logWrites:         http2logFrameWrites,
		debugReadLoggerf:  log.Printf,
		debugWriteLoggerf: log.Printf,
	}
	fr.getReadBuf = func(size uint32) []byte {
		if cap(fr.readBuf) >= int(size) {
			return fr.readBuf[:size]
		}
		fr.readBuf = make([]byte, size)
		return fr.readBuf
	}
	fr.SetMaxReadFrameSize(http2maxFrameSize)
	return fr
}

// SetMaxReadFrameSize sets the maximum size of a frame
// that will be read by a subsequent call to ReadFrame.
// It is the caller's responsibility to advertise this
// limit with a SETTINGS frame.
func (fr *http2Framer) SetMaxReadFrameSize(v uint32) {
	if v > http2maxFrameSize {
		v = http2maxFrameSize
	}
	fr.maxReadSize = v
}

// ErrorDetail returns a more detailed error of the last error
// returned by Framer.ReadFrame. For instance, if ReadFrame
// returns a StreamError with code PROTOCOL_ERROR, ErrorDetail
// will say exactly what was invalid. ErrorDetail is not guaranteed
// to return a non-nil value and like the rest of the http2 package,
// its return value is not protected by an API compatibility promise.
// ErrorDetail is reset after the next call to ReadFrame.
func (fr *http2Framer) ErrorDetail() error {
	return fr.errDetail
}

// ErrFrameTooLarge is returned from Framer.ReadFrame when the peer
// sends a frame that is larger than declared with SetMaxReadFrameSize.
var http2ErrFrameTooLarge = errors.New("http2: frame too large")

// terminalReadFrameError reports whether err is an unrecoverable
// error from ReadFrame and no other frames should be read.
func http2terminalReadFrameError(err error) bool {
	if _, ok := err.(http2StreamError); ok {
		return false
	}
	return err != nil
}

// ReadFrame reads a single frame. The returned Frame is only valid
// until the next call to ReadFrame.
//
// If the frame is larger than previously set with SetMaxReadFrameSize, the
// returned error is ErrFrameTooLarge. Other errors may be of type
// ConnectionError, StreamError, or anything else from the underlying
// reader.
func (fr *http2Framer) ReadFrame() (http2Frame, error) {
	fr.errDetail = nil
	if fr.lastFrame != nil {
		fr.lastFrame.invalidate()
	}
	fh, err := http2readFrameHeader(fr.headerBuf[:], fr.r)
	if err != nil {
		return nil, err
	}
	if fh.Length > fr.maxReadSize {
		return nil, http2ErrFrameTooLarge
	}
	payload := fr.getReadBuf(fh.Length)
	if _, err := io.ReadFull(fr.r, payload); err != nil {
		return nil, err
	}
	f, err := http2typeFrameParser(fh.Type)(fh, payload)
	if err != nil {
		if ce, ok := err.(http2connError); ok {
			return nil, fr.connError(ce.Code, ce.Reason)
		}
		return nil, err
	}
	if err := fr.checkFrameOrder(f); err != nil {
		return nil, err
	}
	if fr.logReads {
		fr.debugReadLoggerf("http2: Framer %p: read %v", fr, http2summarizeFrame(f))
	}
	if fh.Type == http2FrameHeaders && fr.ReadMetaHeaders != nil {
		return fr.readMetaFrame(f.(*http2HeadersFrame))
	}
	return f, nil
}

// connError returns ConnectionError(code) but first
// stashes away a public reason to the caller can optionally relay it
// to the peer before hanging up on them. This might help others debug
// their implementations.
func (fr *http2Framer) connError(code http2ErrCode, reason string) error {
	fr.errDetail = errors.New(reason)
	return http2ConnectionError(code)
}

// checkFrameOrder reports an error if f is an invalid frame to return
// next from ReadFrame. Mostly it checks whether HEADERS and
// CONTINUATION frames are contiguous.
func (fr *http2Framer) checkFrameOrder(f http2Frame) error {
	last := fr.lastFrame
	fr.lastFrame = f
	if fr.AllowIllegalReads {
		return nil
	}

	fh := f.Header()
	if fr.lastHeaderStream != 0 {
		if fh.Type != http2FrameContinuation {
			return fr.connError(http2ErrCodeProtocol,
				fmt.Sprintf("got %s for stream %d; expected CONTINUATION following %s for stream %d",
					fh.Type, fh.StreamID,
					last.Header().Type, fr.lastHeaderStream))
		}
		if fh.StreamID != fr.lastHeaderStream {
			return fr.connError(http2ErrCodeProtocol,
				fmt.Sprintf("got CONTINUATION for stream %d; expected stream %d",
					fh.StreamID, fr.lastHeaderStream))
		}
	} else if fh.Type == http2FrameContinuation {
		return fr.connError(http2ErrCodeProtocol, fmt.Sprintf("unexpected CONTINUATION for stream %d", fh.StreamID))
	}

	switch fh.Type {
	case http2FrameHeaders, http2FrameContinuation:
		if fh.Flags.Has(http2FlagHeadersEndHeaders) {
			fr.lastHeaderStream = 0
		} else {
			fr.lastHeaderStream = fh.StreamID
		}
	}

	return nil
}

// A DataFrame conveys arbitrary, variable-length sequences of octets
// associated with a stream.
// See http://http2.github.io/http2-spec/#rfc.section.6.1
type http2DataFrame struct {
	http2FrameHeader
	data []byte
}

func (f *http2DataFrame) StreamEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagDataEndStream)
}

// Data returns the frame's data octets, not including any padding
// size byte or padding suffix bytes.
// The caller must not retain the returned memory past the next
// call to ReadFrame.
func (f *http2DataFrame) Data() []byte {
	f.checkValid()
	return f.data
}

func http2parseDataFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if fh.StreamID == 0 {

		return nil, http2connError{http2ErrCodeProtocol, "DATA frame with stream ID 0"}
	}
	f := &http2DataFrame{
		http2FrameHeader: fh,
	}
	var padSize byte
	if fh.Flags.Has(http2FlagDataPadded) {
		var err error
		payload, padSize, err = http2readByte(payload)
		if err != nil {
			return nil, err
		}
	}
	if int(padSize) > len(payload) {

		return nil, http2connError{http2ErrCodeProtocol, "pad size larger than data payload"}
	}
	f.data = payload[:len(payload)-int(padSize)]
	return f, nil
}

var (
	http2errStreamID    = errors.New("invalid stream ID")
	http2errDepStreamID = errors.New("invalid dependent stream ID")
	http2errPadLength   = errors.New("pad length too large")
)

func http2validStreamIDOrZero(streamID uint32) bool {
	return streamID&(1<<31) == 0
}

func http2validStreamID(streamID uint32) bool {
	return streamID != 0 && streamID&(1<<31) == 0
}

// WriteData writes a DATA frame.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility not to violate the maximum frame size
// and to not call other Write methods concurrently.
func (f *http2Framer) WriteData(streamID uint32, endStream bool, data []byte) error {
	return f.WriteDataPadded(streamID, endStream, data, nil)
}

// WriteData writes a DATA frame with optional padding.
//
// If pad is nil, the padding bit is not sent.
// The length of pad must not exceed 255 bytes.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility not to violate the maximum frame size
// and to not call other Write methods concurrently.
func (f *http2Framer) WriteDataPadded(streamID uint32, endStream bool, data, pad []byte) error {
	if !http2validStreamID(streamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	if len(pad) > 255 {
		return http2errPadLength
	}
	var flags http2Flags
	if endStream {
		flags |= http2FlagDataEndStream
	}
	if pad != nil {
		flags |= http2FlagDataPadded
	}
	f.startWrite(http2FrameData, flags, streamID)
	if pad != nil {
		f.wbuf = append(f.wbuf, byte(len(pad)))
	}
	f.wbuf = append(f.wbuf, data...)
	f.wbuf = append(f.wbuf, pad...)
	return f.endWrite()
}

// A SettingsFrame conveys configuration parameters that affect how
// endpoints communicate, such as preferences and constraints on peer
// behavior.
//
// See http://http2.github.io/http2-spec/#SETTINGS
type http2SettingsFrame struct {
	http2FrameHeader
	p []byte
}

func http2parseSettingsFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.Flags.Has(http2FlagSettingsAck) && fh.Length > 0 {

		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID != 0 {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	if len(p)%6 != 0 {

		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	f := &http2SettingsFrame{http2FrameHeader: fh, p: p}
	if v, ok := f.Value(http2SettingInitialWindowSize); ok && v > (1<<31)-1 {

		return nil, http2ConnectionError(http2ErrCodeFlowControl)
	}
	return f, nil
}

func (f *http2SettingsFrame) IsAck() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagSettingsAck)
}

func (f *http2SettingsFrame) Value(s http2SettingID) (v uint32, ok bool) {
	f.checkValid()
	buf := f.p
	for len(buf) > 0 {
		settingID := http2SettingID(binary.BigEndian.Uint16(buf[:2]))
		if settingID == s {
			return binary.BigEndian.Uint32(buf[2:6]), true
		}
		buf = buf[6:]
	}
	return 0, false
}

// ForeachSetting runs fn for each setting.
// It stops and returns the first error.
func (f *http2SettingsFrame) ForeachSetting(fn func(http2Setting) error) error {
	f.checkValid()
	buf := f.p
	for len(buf) > 0 {
		if err := fn(http2Setting{
			http2SettingID(binary.BigEndian.Uint16(buf[:2])),
			binary.BigEndian.Uint32(buf[2:6]),
		}); err != nil {
			return err
		}
		buf = buf[6:]
	}
	return nil
}

// WriteSettings writes a SETTINGS frame with zero or more settings
// specified and the ACK bit not set.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WriteSettings(settings ...http2Setting) error {
	f.startWrite(http2FrameSettings, 0, 0)
	for _, s := range settings {
		f.writeUint16(uint16(s.ID))
		f.writeUint32(s.Val)
	}
	return f.endWrite()
}

// WriteSettingsAck writes an empty SETTINGS frame with the ACK bit set.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WriteSettingsAck() error {
	f.startWrite(http2FrameSettings, http2FlagSettingsAck, 0)
	return f.endWrite()
}

// A PingFrame is a mechanism for measuring a minimal round trip time
// from the sender, as well as determining whether an idle connection
// is still functional.
// See http://http2.github.io/http2-spec/#rfc.section.6.7
type http2PingFrame struct {
	http2FrameHeader
	Data [8]byte
}

func (f *http2PingFrame) IsAck() bool { return f.Flags.Has(http2FlagPingAck) }

func http2parsePingFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if len(payload) != 8 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID != 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	f := &http2PingFrame{http2FrameHeader: fh}
	copy(f.Data[:], payload)
	return f, nil
}

func (f *http2Framer) WritePing(ack bool, data [8]byte) error {
	var flags http2Flags
	if ack {
		flags = http2FlagPingAck
	}
	f.startWrite(http2FramePing, flags, 0)
	f.writeBytes(data[:])
	return f.endWrite()
}

// A GoAwayFrame informs the remote peer to stop creating streams on this connection.
// See http://http2.github.io/http2-spec/#rfc.section.6.8
type http2GoAwayFrame struct {
	http2FrameHeader
	LastStreamID uint32
	ErrCode      http2ErrCode
	debugData    []byte
}

// DebugData returns any debug data in the GOAWAY frame. Its contents
// are not defined.
// The caller must not retain the returned memory past the next
// call to ReadFrame.
func (f *http2GoAwayFrame) DebugData() []byte {
	f.checkValid()
	return f.debugData
}

func http2parseGoAwayFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.StreamID != 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	if len(p) < 8 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	return &http2GoAwayFrame{
		http2FrameHeader: fh,
		LastStreamID:     binary.BigEndian.Uint32(p[:4]) & (1<<31 - 1),
		ErrCode:          http2ErrCode(binary.BigEndian.Uint32(p[4:8])),
		debugData:        p[8:],
	}, nil
}

func (f *http2Framer) WriteGoAway(maxStreamID uint32, code http2ErrCode, debugData []byte) error {
	f.startWrite(http2FrameGoAway, 0, 0)
	f.writeUint32(maxStreamID & (1<<31 - 1))
	f.writeUint32(uint32(code))
	f.writeBytes(debugData)
	return f.endWrite()
}

// An UnknownFrame is the frame type returned when the frame type is unknown
// or no specific frame type parser exists.
type http2UnknownFrame struct {
	http2FrameHeader
	p []byte
}

// Payload returns the frame's payload (after the header).  It is not
// valid to call this method after a subsequent call to
// Framer.ReadFrame, nor is it valid to retain the returned slice.
// The memory is owned by the Framer and is invalidated when the next
// frame is read.
func (f *http2UnknownFrame) Payload() []byte {
	f.checkValid()
	return f.p
}

func http2parseUnknownFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	return &http2UnknownFrame{fh, p}, nil
}

// A WindowUpdateFrame is used to implement flow control.
// See http://http2.github.io/http2-spec/#rfc.section.6.9
type http2WindowUpdateFrame struct {
	http2FrameHeader
	Increment uint32 // never read with high bit set
}

func http2parseWindowUpdateFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if len(p) != 4 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	inc := binary.BigEndian.Uint32(p[:4]) & 0x7fffffff
	if inc == 0 {

		if fh.StreamID == 0 {
			return nil, http2ConnectionError(http2ErrCodeProtocol)
		}
		return nil, http2streamError(fh.StreamID, http2ErrCodeProtocol)
	}
	return &http2WindowUpdateFrame{
		http2FrameHeader: fh,
		Increment:        inc,
	}, nil
}

// WriteWindowUpdate writes a WINDOW_UPDATE frame.
// The increment value must be between 1 and 2,147,483,647, inclusive.
// If the Stream ID is zero, the window update applies to the
// connection as a whole.
func (f *http2Framer) WriteWindowUpdate(streamID, incr uint32) error {

	if (incr < 1 || incr > 2147483647) && !f.AllowIllegalWrites {
		return errors.New("illegal window increment value")
	}
	f.startWrite(http2FrameWindowUpdate, 0, streamID)
	f.writeUint32(incr)
	return f.endWrite()
}

// A HeadersFrame is used to open a stream and additionally carries a
// header block fragment.
type http2HeadersFrame struct {
	http2FrameHeader

	// Priority is set if FlagHeadersPriority is set in the FrameHeader.
	Priority http2PriorityParam

	headerFragBuf []byte // not owned
}

func (f *http2HeadersFrame) HeaderBlockFragment() []byte {
	f.checkValid()
	return f.headerFragBuf
}

func (f *http2HeadersFrame) HeadersEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagHeadersEndHeaders)
}

func (f *http2HeadersFrame) StreamEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagHeadersEndStream)
}

func (f *http2HeadersFrame) HasPriority() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagHeadersPriority)
}

func http2parseHeadersFrame(fh http2FrameHeader, p []byte) (_ http2Frame, err error) {
	hf := &http2HeadersFrame{
		http2FrameHeader: fh,
	}
	if fh.StreamID == 0 {

		return nil, http2connError{http2ErrCodeProtocol, "HEADERS frame with stream ID 0"}
	}
	var padLength uint8
	if fh.Flags.Has(http2FlagHeadersPadded) {
		if p, padLength, err = http2readByte(p); err != nil {
			return
		}
	}
	if fh.Flags.Has(http2FlagHeadersPriority) {
		var v uint32
		p, v, err = http2readUint32(p)
		if err != nil {
			return nil, err
		}
		hf.Priority.StreamDep = v & 0x7fffffff
		hf.Priority.Exclusive = (v != hf.Priority.StreamDep)
		p, hf.Priority.Weight, err = http2readByte(p)
		if err != nil {
			return nil, err
		}
	}
	if len(p)-int(padLength) <= 0 {
		return nil, http2streamError(fh.StreamID, http2ErrCodeProtocol)
	}
	hf.headerFragBuf = p[:len(p)-int(padLength)]
	return hf, nil
}

// HeadersFrameParam are the parameters for writing a HEADERS frame.
type http2HeadersFrameParam struct {
	// StreamID is the required Stream ID to initiate.
	StreamID uint32
	// BlockFragment is part (or all) of a Header Block.
	BlockFragment []byte

	// EndStream indicates that the header block is the last that
	// the endpoint will send for the identified stream. Setting
	// this flag causes the stream to enter one of "half closed"
	// states.
	EndStream bool

	// EndHeaders indicates that this frame contains an entire
	// header block and is not followed by any
	// CONTINUATION frames.
	EndHeaders bool

	// PadLength is the optional number of bytes of zeros to add
	// to this frame.
	PadLength uint8

	// Priority, if non-zero, includes stream priority information
	// in the HEADER frame.
	Priority http2PriorityParam
}

// WriteHeaders writes a single HEADERS frame.
//
// This is a low-level header writing method. Encoding headers and
// splitting them into any necessary CONTINUATION frames is handled
// elsewhere.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WriteHeaders(p http2HeadersFrameParam) error {
	if !http2validStreamID(p.StreamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	var flags http2Flags
	if p.PadLength != 0 {
		flags |= http2FlagHeadersPadded
	}
	if p.EndStream {
		flags |= http2FlagHeadersEndStream
	}
	if p.EndHeaders {
		flags |= http2FlagHeadersEndHeaders
	}
	if !p.Priority.IsZero() {
		flags |= http2FlagHeadersPriority
	}
	f.startWrite(http2FrameHeaders, flags, p.StreamID)
	if p.PadLength != 0 {
		f.writeByte(p.PadLength)
	}
	if !p.Priority.IsZero() {
		v := p.Priority.StreamDep
		if !http2validStreamIDOrZero(v) && !f.AllowIllegalWrites {
			return http2errDepStreamID
		}
		if p.Priority.Exclusive {
			v |= 1 << 31
		}
		f.writeUint32(v)
		f.writeByte(p.Priority.Weight)
	}
	f.wbuf = append(f.wbuf, p.BlockFragment...)
	f.wbuf = append(f.wbuf, http2padZeros[:p.PadLength]...)
	return f.endWrite()
}

// A PriorityFrame specifies the sender-advised priority of a stream.
// See http://http2.github.io/http2-spec/#rfc.section.6.3
type http2PriorityFrame struct {
	http2FrameHeader
	http2PriorityParam
}

// PriorityParam are the stream prioritzation parameters.
type http2PriorityParam struct {
	// StreamDep is a 31-bit stream identifier for the
	// stream that this stream depends on. Zero means no
	// dependency.
	StreamDep uint32

	// Exclusive is whether the dependency is exclusive.
	Exclusive bool

	// Weight is the stream's zero-indexed weight. It should be
	// set together with StreamDep, or neither should be set.  Per
	// the spec, "Add one to the value to obtain a weight between
	// 1 and 256."
	Weight uint8
}

func (p http2PriorityParam) IsZero() bool {
	return p == http2PriorityParam{}
}

func http2parsePriorityFrame(fh http2FrameHeader, payload []byte) (http2Frame, error) {
	if fh.StreamID == 0 {
		return nil, http2connError{http2ErrCodeProtocol, "PRIORITY frame with stream ID 0"}
	}
	if len(payload) != 5 {
		return nil, http2connError{http2ErrCodeFrameSize, fmt.Sprintf("PRIORITY frame payload size was %d; want 5", len(payload))}
	}
	v := binary.BigEndian.Uint32(payload[:4])
	streamID := v & 0x7fffffff
	return &http2PriorityFrame{
		http2FrameHeader: fh,
		http2PriorityParam: http2PriorityParam{
			Weight:    payload[4],
			StreamDep: streamID,
			Exclusive: streamID != v,
		},
	}, nil
}

// WritePriority writes a PRIORITY frame.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WritePriority(streamID uint32, p http2PriorityParam) error {
	if !http2validStreamID(streamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	if !http2validStreamIDOrZero(p.StreamDep) {
		return http2errDepStreamID
	}
	f.startWrite(http2FramePriority, 0, streamID)
	v := p.StreamDep
	if p.Exclusive {
		v |= 1 << 31
	}
	f.writeUint32(v)
	f.writeByte(p.Weight)
	return f.endWrite()
}

// A RSTStreamFrame allows for abnormal termination of a stream.
// See http://http2.github.io/http2-spec/#rfc.section.6.4
type http2RSTStreamFrame struct {
	http2FrameHeader
	ErrCode http2ErrCode
}

func http2parseRSTStreamFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if len(p) != 4 {
		return nil, http2ConnectionError(http2ErrCodeFrameSize)
	}
	if fh.StreamID == 0 {
		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	return &http2RSTStreamFrame{fh, http2ErrCode(binary.BigEndian.Uint32(p[:4]))}, nil
}

// WriteRSTStream writes a RST_STREAM frame.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WriteRSTStream(streamID uint32, code http2ErrCode) error {
	if !http2validStreamID(streamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	f.startWrite(http2FrameRSTStream, 0, streamID)
	f.writeUint32(uint32(code))
	return f.endWrite()
}

// A ContinuationFrame is used to continue a sequence of header block fragments.
// See http://http2.github.io/http2-spec/#rfc.section.6.10
type http2ContinuationFrame struct {
	http2FrameHeader
	headerFragBuf []byte
}

func http2parseContinuationFrame(fh http2FrameHeader, p []byte) (http2Frame, error) {
	if fh.StreamID == 0 {
		return nil, http2connError{http2ErrCodeProtocol, "CONTINUATION frame with stream ID 0"}
	}
	return &http2ContinuationFrame{fh, p}, nil
}

func (f *http2ContinuationFrame) HeaderBlockFragment() []byte {
	f.checkValid()
	return f.headerFragBuf
}

func (f *http2ContinuationFrame) HeadersEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagContinuationEndHeaders)
}

// WriteContinuation writes a CONTINUATION frame.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WriteContinuation(streamID uint32, endHeaders bool, headerBlockFragment []byte) error {
	if !http2validStreamID(streamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	var flags http2Flags
	if endHeaders {
		flags |= http2FlagContinuationEndHeaders
	}
	f.startWrite(http2FrameContinuation, flags, streamID)
	f.wbuf = append(f.wbuf, headerBlockFragment...)
	return f.endWrite()
}

// A PushPromiseFrame is used to initiate a server stream.
// See http://http2.github.io/http2-spec/#rfc.section.6.6
type http2PushPromiseFrame struct {
	http2FrameHeader
	PromiseID     uint32
	headerFragBuf []byte // not owned
}

func (f *http2PushPromiseFrame) HeaderBlockFragment() []byte {
	f.checkValid()
	return f.headerFragBuf
}

func (f *http2PushPromiseFrame) HeadersEnded() bool {
	return f.http2FrameHeader.Flags.Has(http2FlagPushPromiseEndHeaders)
}

func http2parsePushPromise(fh http2FrameHeader, p []byte) (_ http2Frame, err error) {
	pp := &http2PushPromiseFrame{
		http2FrameHeader: fh,
	}
	if pp.StreamID == 0 {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	// The PUSH_PROMISE frame includes optional padding.
	// Padding fields and flags are identical to those defined for DATA frames
	var padLength uint8
	if fh.Flags.Has(http2FlagPushPromisePadded) {
		if p, padLength, err = http2readByte(p); err != nil {
			return
		}
	}

	p, pp.PromiseID, err = http2readUint32(p)
	if err != nil {
		return
	}
	pp.PromiseID = pp.PromiseID & (1<<31 - 1)

	if int(padLength) > len(p) {

		return nil, http2ConnectionError(http2ErrCodeProtocol)
	}
	pp.headerFragBuf = p[:len(p)-int(padLength)]
	return pp, nil
}

// PushPromiseParam are the parameters for writing a PUSH_PROMISE frame.
type http2PushPromiseParam struct {
	// StreamID is the required Stream ID to initiate.
	StreamID uint32

	// PromiseID is the required Stream ID which this
	// Push Promises
	PromiseID uint32

	// BlockFragment is part (or all) of a Header Block.
	BlockFragment []byte

	// EndHeaders indicates that this frame contains an entire
	// header block and is not followed by any
	// CONTINUATION frames.
	EndHeaders bool

	// PadLength is the optional number of bytes of zeros to add
	// to this frame.
	PadLength uint8
}

// WritePushPromise writes a single PushPromise Frame.
//
// As with Header Frames, This is the low level call for writing
// individual frames. Continuation frames are handled elsewhere.
//
// It will perform exactly one Write to the underlying Writer.
// It is the caller's responsibility to not call other Write methods concurrently.
func (f *http2Framer) WritePushPromise(p http2PushPromiseParam) error {
	if !http2validStreamID(p.StreamID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	var flags http2Flags
	if p.PadLength != 0 {
		flags |= http2FlagPushPromisePadded
	}
	if p.EndHeaders {
		flags |= http2FlagPushPromiseEndHeaders
	}
	f.startWrite(http2FramePushPromise, flags, p.StreamID)
	if p.PadLength != 0 {
		f.writeByte(p.PadLength)
	}
	if !http2validStreamID(p.PromiseID) && !f.AllowIllegalWrites {
		return http2errStreamID
	}
	f.writeUint32(p.PromiseID)
	f.wbuf = append(f.wbuf, p.BlockFragment...)
	f.wbuf = append(f.wbuf, http2padZeros[:p.PadLength]...)
	return f.endWrite()
}

// WriteRawFrame writes a raw frame. This can be used to write
// extension frames unknown to this package.
func (f *http2Framer) WriteRawFrame(t http2FrameType, flags http2Flags, streamID uint32, payload []byte) error {
	f.startWrite(t, flags, streamID)
	f.writeBytes(payload)
	return f.endWrite()
}

func http2readByte(p []byte) (remain []byte, b byte, err error) {
	if len(p) == 0 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	return p[1:], p[0], nil
}

func http2readUint32(p []byte) (remain []byte, v uint32, err error) {
	if len(p) < 4 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	return p[4:], binary.BigEndian.Uint32(p[:4]), nil
}

type http2streamEnder interface {
	StreamEnded() bool
}

type http2headersEnder interface {
	HeadersEnded() bool
}

type http2headersOrContinuation interface {
	http2headersEnder
	HeaderBlockFragment() []byte
}

// A MetaHeadersFrame is the representation of one HEADERS frame and
// zero or more contiguous CONTINUATION frames and the decoding of
// their HPACK-encoded contents.
//
// This type of frame does not appear on the wire and is only returned
// by the Framer when Framer.ReadMetaHeaders is set.
type http2MetaHeadersFrame struct {
	*http2HeadersFrame

	// Fields are the fields contained in the HEADERS and
	// CONTINUATION frames. The underlying slice is owned by the
	// Framer and must not be retained after the next call to
	// ReadFrame.
	//
	// Fields are guaranteed to be in the correct http2 order and
	// not have unknown pseudo header fields or invalid header
	// field names or values. Required pseudo header fields may be
	// missing, however. Use the MetaHeadersFrame.Pseudo accessor
	// method access pseudo headers.
	Fields []hpack.HeaderField

	// Truncated is whether the max header list size limit was hit
	// and Fields is incomplete. The hpack decoder state is still
	// valid, however.
	Truncated bool
}

// PseudoValue returns the given pseudo header field's value.
// The provided pseudo field should not contain the leading colon.
func (mh *http2MetaHeadersFrame) PseudoValue(pseudo string) string {
	for _, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return ""
		}
		if hf.Name[1:] == pseudo {
			return hf.Value
		}
	}
	return ""
}

// RegularFields returns the regular (non-pseudo) header fields of mh.
// The caller does not own the returned slice.
func (mh *http2MetaHeadersFrame) RegularFields() []hpack.HeaderField {
	for i, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return mh.Fields[i:]
		}
	}
	return nil
}

// PseudoFields returns the pseudo header fields of mh.
// The caller does not own the returned slice.
func (mh *http2MetaHeadersFrame) PseudoFields() []hpack.HeaderField {
	for i, hf := range mh.Fields {
		if !hf.IsPseudo() {
			return mh.Fields[:i]
		}
	}
	return mh.Fields
}

func (mh *http2MetaHeadersFrame) checkPseudos() error {
	var isRequest, isResponse bool
	pf := mh.PseudoFields()
	for i, hf := range pf {
		switch hf.Name {
		case ":method", ":path", ":scheme", ":authority":
			isRequest = true
		case ":status":
			isResponse = true
		default:
			return http2pseudoHeaderError(hf.Name)
		}

		for _, hf2 := range pf[:i] {
			if hf.Name == hf2.Name {
				return http2duplicatePseudoHeaderError(hf.Name)
			}
		}
	}
	if isRequest && isResponse {
		return http2errMixPseudoHeaderTypes
	}
	return nil
}

func (fr *http2Framer) maxHeaderStringLen() int {
	v := fr.maxHeaderListSize()
	if uint32(int(v)) == v {
		return int(v)
	}

	return 0
}

// readMetaFrame returns 0 or more CONTINUATION frames from fr and
// merge them into into the provided hf and returns a MetaHeadersFrame
// with the decoded hpack values.
func (fr *http2Framer) readMetaFrame(hf *http2HeadersFrame) (*http2MetaHeadersFrame, error) {
	if fr.AllowIllegalReads {
		return nil, errors.New("illegal use of AllowIllegalReads with ReadMetaHeaders")
	}
	mh := &http2MetaHeadersFrame{
		http2HeadersFrame: hf,
	}
	var remainSize = fr.maxHeaderListSize()
	var sawRegular bool

	var invalid error // pseudo header field errors
	hdec := fr.ReadMetaHeaders
	hdec.SetEmitEnabled(true)
	hdec.SetMaxStringLength(fr.maxHeaderStringLen())
	hdec.SetEmitFunc(func(hf hpack.HeaderField) {
		if http2VerboseLogs && fr.logReads {
			fr.debugReadLoggerf("http2: decoded hpack field %+v", hf)
		}
		if !httplex.ValidHeaderFieldValue(hf.Value) {
			invalid = http2headerFieldValueError(hf.Value)
		}
		isPseudo := strings.HasPrefix(hf.Name, ":")
		if isPseudo {
			if sawRegular {
				invalid = http2errPseudoAfterRegular
			}
		} else {
			sawRegular = true
			if !http2validWireHeaderFieldName(hf.Name) {
				invalid = http2headerFieldNameError(hf.Name)
			}
		}

		if invalid != nil {
			hdec.SetEmitEnabled(false)
			return
		}

		size := hf.Size()
		if size > remainSize {
			hdec.SetEmitEnabled(false)
			mh.Truncated = true
			return
		}
		remainSize -= size

		mh.Fields = append(mh.Fields, hf)
	})

	defer hdec.SetEmitFunc(func(hf hpack.HeaderField) {})

	var hc http2headersOrContinuation = hf
	for {
		frag := hc.HeaderBlockFragment()
		if _, err := hdec.Write(frag); err != nil {
			return nil, http2ConnectionError(http2ErrCodeCompression)
		}

		if hc.HeadersEnded() {
			break
		}
		if f, err := fr.ReadFrame(); err != nil {
			return nil, err
		} else {
			hc = f.(*http2ContinuationFrame)
		}
	}

	mh.http2HeadersFrame.headerFragBuf = nil
	mh.http2HeadersFrame.invalidate()

	if err := hdec.Close(); err != nil {
		return nil, http2ConnectionError(http2ErrCodeCompression)
	}
	if invalid != nil {
		fr.errDetail = invalid
		if http2VerboseLogs {
			log.Printf("http2: invalid header: %v", invalid)
		}
		return nil, http2StreamError{mh.StreamID, http2ErrCodeProtocol, invalid}
	}
	if err := mh.checkPseudos(); err != nil {
		fr.errDetail = err
		if http2VerboseLogs {
			log.Printf("http2: invalid pseudo headers: %v", err)
		}
		return nil, http2StreamError{mh.StreamID, http2ErrCodeProtocol, err}
	}
	return mh, nil
}

func http2summarizeFrame(f http2Frame) string {
	var buf bytes.Buffer
	f.Header().writeDebug(&buf)
	switch f := f.(type) {
	case *http2SettingsFrame:
		n := 0
		f.ForeachSetting(func(s http2Setting) error {
			n++
			if n == 1 {
				buf.WriteString(", settings:")
			}
			fmt.Fprintf(&buf, " %v=%v,", s.ID, s.Val)
			return nil
		})
		if n > 0 {
			buf.Truncate(buf.Len() - 1)
		}
	case *http2DataFrame:
		data := f.Data()
		const max = 256
		if len(data) > max {
			data = data[:max]
		}
		fmt.Fprintf(&buf, " data=%q", data)
		if len(f.Data()) > max {
			fmt.Fprintf(&buf, " (%d bytes omitted)", len(f.Data())-max)
		}
	case *http2WindowUpdateFrame:
		if f.StreamID == 0 {
			buf.WriteString(" (conn)")
		}
		fmt.Fprintf(&buf, " incr=%v", f.Increment)
	case *http2PingFrame:
		fmt.Fprintf(&buf, " ping=%q", f.Data[:])
	case *http2GoAwayFrame:
		fmt.Fprintf(&buf, " LastStreamID=%v ErrCode=%v Debug=%q",
			f.LastStreamID, f.ErrCode, f.debugData)
	case *http2RSTStreamFrame:
		fmt.Fprintf(&buf, " ErrCode=%v", f.ErrCode)
	}
	return buf.String()
}

func http2transportExpectContinueTimeout(t1 *Transport) time.Duration {
	return t1.ExpectContinueTimeout
}

// isBadCipher reports whether the cipher is blacklisted by the HTTP/2 spec.
func http2isBadCipher(cipher uint16) bool {
	switch cipher {
	case tls.TLS_RSA_WITH_RC4_128_SHA,
		tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
		tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:

		return true
	default:
		return false
	}
}

type http2contextContext interface {
	context.Context
}

func http2serverConnBaseContext(c net.Conn, opts *http2ServeConnOpts) (ctx http2contextContext, cancel func()) {
	ctx, cancel = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, LocalAddrContextKey, c.LocalAddr())
	if hs := opts.baseConfig(); hs != nil {
		ctx = context.WithValue(ctx, ServerContextKey, hs)
	}
	return
}

func http2contextWithCancel(ctx http2contextContext) (_ http2contextContext, cancel func()) {
	return context.WithCancel(ctx)
}

func http2requestWithContext(req *Request, ctx http2contextContext) *Request {
	return req.WithContext(ctx)
}

type http2clientTrace httptrace.ClientTrace

func http2reqContext(r *Request) context.Context { return r.Context() }

func (t *http2Transport) idleConnTimeout() time.Duration {
	if t.t1 != nil {
		return t.t1.IdleConnTimeout
	}
	return 0
}

func http2setResponseUncompressed(res *Response) { res.Uncompressed = true }

func http2traceGotConn(req *Request, cc *http2ClientConn) {
	trace := httptrace.ContextClientTrace(req.Context())
	if trace == nil || trace.GotConn == nil {
		return
	}
	ci := httptrace.GotConnInfo{Conn: cc.tconn}
	cc.mu.Lock()
	ci.Reused = cc.nextStreamID > 1
	ci.WasIdle = len(cc.streams) == 0 && ci.Reused
	if ci.WasIdle && !cc.lastActive.IsZero() {
		ci.IdleTime = time.Now().Sub(cc.lastActive)
	}
	cc.mu.Unlock()

	trace.GotConn(ci)
}

func http2traceWroteHeaders(trace *http2clientTrace) {
	if trace != nil && trace.WroteHeaders != nil {
		trace.WroteHeaders()
	}
}

func http2traceGot100Continue(trace *http2clientTrace) {
	if trace != nil && trace.Got100Continue != nil {
		trace.Got100Continue()
	}
}

func http2traceWait100Continue(trace *http2clientTrace) {
	if trace != nil && trace.Wait100Continue != nil {
		trace.Wait100Continue()
	}
}

func http2traceWroteRequest(trace *http2clientTrace, err error) {
	if trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{Err: err})
	}
}

func http2traceFirstResponseByte(trace *http2clientTrace) {
	if trace != nil && trace.GotFirstResponseByte != nil {
		trace.GotFirstResponseByte()
	}
}

func http2requestTrace(req *Request) *http2clientTrace {
	trace := httptrace.ContextClientTrace(req.Context())
	return (*http2clientTrace)(trace)
}

// Ping sends a PING frame to the server and waits for the ack.
func (cc *http2ClientConn) Ping(ctx context.Context) error {
	return cc.ping(ctx)
}

func http2cloneTLSConfig(c *tls.Config) *tls.Config { return c.Clone() }

var _ Pusher = (*http2responseWriter)(nil)

// Push implements http.Pusher.
func (w *http2responseWriter) Push(target string, opts *PushOptions) error {
	internalOpts := http2pushOptions{}
	if opts != nil {
		internalOpts.Method = opts.Method
		internalOpts.Header = opts.Header
	}
	return w.push(target, internalOpts)
}

func http2configureServer18(h1 *Server, h2 *http2Server) error {
	if h2.IdleTimeout == 0 {
		if h1.IdleTimeout != 0 {
			h2.IdleTimeout = h1.IdleTimeout
		} else {
			h2.IdleTimeout = h1.ReadTimeout
		}
	}
	return nil
}

func http2shouldLogPanic(panicValue interface{}) bool {
	return panicValue != nil && panicValue != ErrAbortHandler
}

func http2reqGetBody(req *Request) func() (io.ReadCloser, error) {
	return req.GetBody
}

func http2reqBodyIsNoBody(body io.ReadCloser) bool {
	return body == NoBody
}

var http2DebugGoroutines = os.Getenv("DEBUG_HTTP2_GOROUTINES") == "1"

type http2goroutineLock uint64

func http2newGoroutineLock() http2goroutineLock {
	if !http2DebugGoroutines {
		return 0
	}
	return http2goroutineLock(http2curGoroutineID())
}

func (g http2goroutineLock) check() {
	if !http2DebugGoroutines {
		return
	}
	if http2curGoroutineID() != uint64(g) {
		panic("running on the wrong goroutine")
	}
}

func (g http2goroutineLock) checkNotOn() {
	if !http2DebugGoroutines {
		return
	}
	if http2curGoroutineID() == uint64(g) {
		panic("running on the wrong goroutine")
	}
}

var http2goroutineSpace = []byte("goroutine ")

func http2curGoroutineID() uint64 {
	bp := http2littleBuf.Get().(*[]byte)
	defer http2littleBuf.Put(bp)
	b := *bp
	b = b[:runtime.Stack(b, false)]

	b = bytes.TrimPrefix(b, http2goroutineSpace)
	i := bytes.IndexByte(b, ' ')
	if i < 0 {
		panic(fmt.Sprintf("No space found in %q", b))
	}
	b = b[:i]
	n, err := http2parseUintBytes(b, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse goroutine ID out of %q: %v", b, err))
	}
	return n
}

var http2littleBuf = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 64)
		return &buf
	},
}

// parseUintBytes is like strconv.ParseUint, but using a []byte.
func http2parseUintBytes(s []byte, base int, bitSize int) (n uint64, err error) {
	var cutoff, maxVal uint64

	if bitSize == 0 {
		bitSize = int(strconv.IntSize)
	}

	s0 := s
	switch {
	case len(s) < 1:
		err = strconv.ErrSyntax
		goto Error

	case 2 <= base && base <= 36:

	case base == 0:

		switch {
		case s[0] == '0' && len(s) > 1 && (s[1] == 'x' || s[1] == 'X'):
			base = 16
			s = s[2:]
			if len(s) < 1 {
				err = strconv.ErrSyntax
				goto Error
			}
		case s[0] == '0':
			base = 8
		default:
			base = 10
		}

	default:
		err = errors.New("invalid base " + strconv.Itoa(base))
		goto Error
	}

	n = 0
	cutoff = http2cutoff64(base)
	maxVal = 1<<uint(bitSize) - 1

	for i := 0; i < len(s); i++ {
		var v byte
		d := s[i]
		switch {
		case '0' <= d && d <= '9':
			v = d - '0'
		case 'a' <= d && d <= 'z':
			v = d - 'a' + 10
		case 'A' <= d && d <= 'Z':
			v = d - 'A' + 10
		default:
			n = 0
			err = strconv.ErrSyntax
			goto Error
		}
		if int(v) >= base {
			n = 0
			err = strconv.ErrSyntax
			goto Error
		}

		if n >= cutoff {

			n = 1<<64 - 1
			err = strconv.ErrRange
			goto Error
		}
		n *= uint64(base)

		n1 := n + uint64(v)
		if n1 < n || n1 > maxVal {

			n = 1<<64 - 1
			err = strconv.ErrRange
			goto Error
		}
		n = n1
	}

	return n, nil

Error:
	return n, &strconv.NumError{Func: "ParseUint", Num: string(s0), Err: err}
}

// Return the first number n such that n*base >= 1<<64.
func http2cutoff64(base int) uint64 {
	if base < 2 {
		return 0
	}
	return (1<<64-1)/uint64(base) + 1
}

var (
	http2commonLowerHeader = map[string]string{} // Go-Canonical-Case -> lower-case
	http2commonCanonHeader = map[string]string{} // lower-case -> Go-Canonical-Case
)

func init() {
	for _, v := range []string{
		"accept",
		"accept-charset",
		"accept-encoding",
		"accept-language",
		"accept-ranges",
		"age",
		"access-control-allow-origin",
		"allow",
		"authorization",
		"cache-control",
		"content-disposition",
		"content-encoding",
		"content-language",
		"content-length",
		"content-location",
		"content-range",
		"content-type",
		"cookie",
		"date",
		"etag",
		"expect",
		"expires",
		"from",
		"host",
		"if-match",
		"if-modified-since",
		"if-none-match",
		"if-unmodified-since",
		"last-modified",
		"link",
		"location",
		"max-forwards",
		"proxy-authenticate",
		"proxy-authorization",
		"range",
		"referer",
		"refresh",
		"retry-after",
		"server",
		"set-cookie",
		"strict-transport-security",
		"trailer",
		"transfer-encoding",
		"user-agent",
		"vary",
		"via",
		"www-authenticate",
	} {
		chk := CanonicalHeaderKey(v)
		http2commonLowerHeader[chk] = v
		http2commonCanonHeader[v] = chk
	}
}

func http2lowerHeader(v string) string {
	if s, ok := http2commonLowerHeader[v]; ok {
		return s
	}
	return strings.ToLower(v)
}

var (
	http2VerboseLogs    bool
	http2logFrameWrites bool
	http2logFrameReads  bool
	http2inTests        bool
)

func init() {
	e := os.Getenv("GODEBUG")
	if strings.Contains(e, "http2debug=1") {
		http2VerboseLogs = true
	}
	if strings.Contains(e, "http2debug=2") {
		http2VerboseLogs = true
		http2logFrameWrites = true
		http2logFrameReads = true
	}
}

const (
	// ClientPreface is the string that must be sent by new
	// connections from clients.
	http2ClientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

	// SETTINGS_MAX_FRAME_SIZE default
	// http://http2.github.io/http2-spec/#rfc.section.6.5.2
	http2initialMaxFrameSize = 16384

	// NextProtoTLS is the NPN/ALPN protocol negotiated during
	// HTTP/2's TLS setup.
	http2NextProtoTLS = "h2"

	// http://http2.github.io/http2-spec/#SettingValues
	http2initialHeaderTableSize = 4096

	http2initialWindowSize = 65535 // 6.9.2 Initial Flow Control Window Size

	http2defaultMaxReadFrameSize = 1 << 20
)

var (
	http2clientPreface = []byte(http2ClientPreface)
)

type http2streamState int

// HTTP/2 stream states.
//
// See http://tools.ietf.org/html/rfc7540#section-5.1.
//
// For simplicity, the server code merges "reserved (local)" into
// "half-closed (remote)". This is one less state transition to track.
// The only downside is that we send PUSH_PROMISEs slightly less
// liberally than allowable. More discussion here:
// https://lists.w3.org/Archives/Public/ietf-http-wg/2016JulSep/0599.html
//
// "reserved (remote)" is omitted since the client code does not
// support server push.
const (
	http2stateIdle http2streamState = iota
	http2stateOpen
	http2stateHalfClosedLocal
	http2stateHalfClosedRemote
	http2stateClosed
)

var http2stateName = [...]string{
	http2stateIdle:             "Idle",
	http2stateOpen:             "Open",
	http2stateHalfClosedLocal:  "HalfClosedLocal",
	http2stateHalfClosedRemote: "HalfClosedRemote",
	http2stateClosed:           "Closed",
}

func (st http2streamState) String() string {
	return http2stateName[st]
}

// Setting is a setting parameter: which setting it is, and its value.
type http2Setting struct {
	// ID is which setting is being set.
	// See http://http2.github.io/http2-spec/#SettingValues
	ID http2SettingID

	// Val is the value.
	Val uint32
}

func (s http2Setting) String() string {
	return fmt.Sprintf("[%v = %d]", s.ID, s.Val)
}

// Valid reports whether the setting is valid.
func (s http2Setting) Valid() error {

	switch s.ID {
	case http2SettingEnablePush:
		if s.Val != 1 && s.Val != 0 {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
	case http2SettingInitialWindowSize:
		if s.Val > 1<<31-1 {
			return http2ConnectionError(http2ErrCodeFlowControl)
		}
	case http2SettingMaxFrameSize:
		if s.Val < 16384 || s.Val > 1<<24-1 {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
	}
	return nil
}

// A SettingID is an HTTP/2 setting as defined in
// http://http2.github.io/http2-spec/#iana-settings
type http2SettingID uint16

const (
	http2SettingHeaderTableSize      http2SettingID = 0x1
	http2SettingEnablePush           http2SettingID = 0x2
	http2SettingMaxConcurrentStreams http2SettingID = 0x3
	http2SettingInitialWindowSize    http2SettingID = 0x4
	http2SettingMaxFrameSize         http2SettingID = 0x5
	http2SettingMaxHeaderListSize    http2SettingID = 0x6
)

var http2settingName = map[http2SettingID]string{
	http2SettingHeaderTableSize:      "HEADER_TABLE_SIZE",
	http2SettingEnablePush:           "ENABLE_PUSH",
	http2SettingMaxConcurrentStreams: "MAX_CONCURRENT_STREAMS",
	http2SettingInitialWindowSize:    "INITIAL_WINDOW_SIZE",
	http2SettingMaxFrameSize:         "MAX_FRAME_SIZE",
	http2SettingMaxHeaderListSize:    "MAX_HEADER_LIST_SIZE",
}

func (s http2SettingID) String() string {
	if v, ok := http2settingName[s]; ok {
		return v
	}
	return fmt.Sprintf("UNKNOWN_SETTING_%d", uint16(s))
}

var (
	http2errInvalidHeaderFieldName  = errors.New("http2: invalid header field name")
	http2errInvalidHeaderFieldValue = errors.New("http2: invalid header field value")
)

// validWireHeaderFieldName reports whether v is a valid header field
// name (key). See httplex.ValidHeaderName for the base rules.
//
// Further, http2 says:
//   "Just as in HTTP/1.x, header field names are strings of ASCII
//   characters that are compared in a case-insensitive
//   fashion. However, header field names MUST be converted to
//   lowercase prior to their encoding in HTTP/2. "
func http2validWireHeaderFieldName(v string) bool {
	if len(v) == 0 {
		return false
	}
	for _, r := range v {
		if !httplex.IsTokenRune(r) {
			return false
		}
		if 'A' <= r && r <= 'Z' {
			return false
		}
	}
	return true
}

var http2httpCodeStringCommon = map[int]string{} // n -> strconv.Itoa(n)

func init() {
	for i := 100; i <= 999; i++ {
		if v := StatusText(i); v != "" {
			http2httpCodeStringCommon[i] = strconv.Itoa(i)
		}
	}
}

func http2httpCodeString(code int) string {
	if s, ok := http2httpCodeStringCommon[code]; ok {
		return s
	}
	return strconv.Itoa(code)
}

// from pkg io
type http2stringWriter interface {
	WriteString(s string) (n int, err error)
}

// A gate lets two goroutines coordinate their activities.
type http2gate chan struct{}

func (g http2gate) Done() { g <- struct{}{} }

func (g http2gate) Wait() { <-g }

// A closeWaiter is like a sync.WaitGroup but only goes 1 to 0 (open to closed).
type http2closeWaiter chan struct{}

// Init makes a closeWaiter usable.
// It exists because so a closeWaiter value can be placed inside a
// larger struct and have the Mutex and Cond's memory in the same
// allocation.
func (cw *http2closeWaiter) Init() {
	*cw = make(chan struct{})
}

// Close marks the closeWaiter as closed and unblocks any waiters.
func (cw http2closeWaiter) Close() {
	close(cw)
}

// Wait waits for the closeWaiter to become closed.
func (cw http2closeWaiter) Wait() {
	<-cw
}

// bufferedWriter is a buffered writer that writes to w.
// Its buffered writer is lazily allocated as needed, to minimize
// idle memory usage with many connections.
type http2bufferedWriter struct {
	w  io.Writer     // immutable
	bw *bufio.Writer // non-nil when data is buffered
}

func http2newBufferedWriter(w io.Writer) *http2bufferedWriter {
	return &http2bufferedWriter{w: w}
}

// bufWriterPoolBufferSize is the size of bufio.Writer's
// buffers created using bufWriterPool.
//
// TODO: pick a less arbitrary value? this is a bit under
// (3 x typical 1500 byte MTU) at least. Other than that,
// not much thought went into it.
const http2bufWriterPoolBufferSize = 4 << 10

var http2bufWriterPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewWriterSize(nil, http2bufWriterPoolBufferSize)
	},
}

func (w *http2bufferedWriter) Available() int {
	if w.bw == nil {
		return http2bufWriterPoolBufferSize
	}
	return w.bw.Available()
}

func (w *http2bufferedWriter) Write(p []byte) (n int, err error) {
	if w.bw == nil {
		bw := http2bufWriterPool.Get().(*bufio.Writer)
		bw.Reset(w.w)
		w.bw = bw
	}
	return w.bw.Write(p)
}

func (w *http2bufferedWriter) Flush() error {
	bw := w.bw
	if bw == nil {
		return nil
	}
	err := bw.Flush()
	bw.Reset(nil)
	http2bufWriterPool.Put(bw)
	w.bw = nil
	return err
}

func http2mustUint31(v int32) uint32 {
	if v < 0 || v > 2147483647 {
		panic("out of range")
	}
	return uint32(v)
}

// bodyAllowedForStatus reports whether a given response status code
// permits a body. See RFC 2616, section 4.4.
func http2bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == 204:
		return false
	case status == 304:
		return false
	}
	return true
}

type http2httpError struct {
	msg     string
	timeout bool
}

func (e *http2httpError) Error() string { return e.msg }

func (e *http2httpError) Timeout() bool { return e.timeout }

func (e *http2httpError) Temporary() bool { return true }

var http2errTimeout error = &http2httpError{msg: "http2: timeout awaiting response headers", timeout: true}

type http2connectionStater interface {
	ConnectionState() tls.ConnectionState
}

var http2sorterPool = sync.Pool{New: func() interface{} { return new(http2sorter) }}

type http2sorter struct {
	v []string // owned by sorter
}

func (s *http2sorter) Len() int { return len(s.v) }

func (s *http2sorter) Swap(i, j int) { s.v[i], s.v[j] = s.v[j], s.v[i] }

func (s *http2sorter) Less(i, j int) bool { return s.v[i] < s.v[j] }

// Keys returns the sorted keys of h.
//
// The returned slice is only valid until s used again or returned to
// its pool.
func (s *http2sorter) Keys(h Header) []string {
	keys := s.v[:0]
	for k := range h {
		keys = append(keys, k)
	}
	s.v = keys
	sort.Sort(s)
	return keys
}

func (s *http2sorter) SortStrings(ss []string) {

	save := s.v
	s.v = ss
	sort.Sort(s)
	s.v = save
}

// validPseudoPath reports whether v is a valid :path pseudo-header
// value. It must be either:
//
//     *) a non-empty string starting with '/', but not with with "//",
//     *) the string '*', for OPTIONS requests.
//
// For now this is only used a quick check for deciding when to clean
// up Opaque URLs before sending requests from the Transport.
// See golang.org/issue/16847
func http2validPseudoPath(v string) bool {
	return (len(v) > 0 && v[0] == '/' && (len(v) == 1 || v[1] != '/')) || v == "*"
}

// pipe is a goroutine-safe io.Reader/io.Writer pair.  It's like
// io.Pipe except there are no PipeReader/PipeWriter halves, and the
// underlying buffer is an interface. (io.Pipe is always unbuffered)
type http2pipe struct {
	mu       sync.Mutex
	c        sync.Cond // c.L lazily initialized to &p.mu
	b        http2pipeBuffer
	err      error         // read error once empty. non-nil means closed.
	breakErr error         // immediate read error (caller doesn't see rest of b)
	donec    chan struct{} // closed on error
	readFn   func()        // optional code to run in Read before error
}

type http2pipeBuffer interface {
	Len() int
	io.Writer
	io.Reader
}

func (p *http2pipe) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b.Len()
}

// Read waits until data is available and copies bytes
// from the buffer into p.
func (p *http2pipe) Read(d []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c.L == nil {
		p.c.L = &p.mu
	}
	for {
		if p.breakErr != nil {
			return 0, p.breakErr
		}
		if p.b.Len() > 0 {
			return p.b.Read(d)
		}
		if p.err != nil {
			if p.readFn != nil {
				p.readFn()
				p.readFn = nil
			}
			return 0, p.err
		}
		p.c.Wait()
	}
}

var http2errClosedPipeWrite = errors.New("write on closed buffer")

// Write copies bytes from p into the buffer and wakes a reader.
// It is an error to write more data than the buffer can hold.
func (p *http2pipe) Write(d []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c.L == nil {
		p.c.L = &p.mu
	}
	defer p.c.Signal()
	if p.err != nil {
		return 0, http2errClosedPipeWrite
	}
	return p.b.Write(d)
}

// CloseWithError causes the next Read (waking up a current blocked
// Read if needed) to return the provided err after all data has been
// read.
//
// The error must be non-nil.
func (p *http2pipe) CloseWithError(err error) { p.closeWithError(&p.err, err, nil) }

// BreakWithError causes the next Read (waking up a current blocked
// Read if needed) to return the provided err immediately, without
// waiting for unread data.
func (p *http2pipe) BreakWithError(err error) { p.closeWithError(&p.breakErr, err, nil) }

// closeWithErrorAndCode is like CloseWithError but also sets some code to run
// in the caller's goroutine before returning the error.
func (p *http2pipe) closeWithErrorAndCode(err error, fn func()) { p.closeWithError(&p.err, err, fn) }

func (p *http2pipe) closeWithError(dst *error, err error, fn func()) {
	if err == nil {
		panic("err must be non-nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c.L == nil {
		p.c.L = &p.mu
	}
	defer p.c.Signal()
	if *dst != nil {

		return
	}
	p.readFn = fn
	*dst = err
	p.closeDoneLocked()
}

// requires p.mu be held.
func (p *http2pipe) closeDoneLocked() {
	if p.donec == nil {
		return
	}

	select {
	case <-p.donec:
	default:
		close(p.donec)
	}
}

// Err returns the error (if any) first set by BreakWithError or CloseWithError.
func (p *http2pipe) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.breakErr != nil {
		return p.breakErr
	}
	return p.err
}

// Done returns a channel which is closed if and when this pipe is closed
// with CloseWithError.
func (p *http2pipe) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.donec == nil {
		p.donec = make(chan struct{})
		if p.err != nil || p.breakErr != nil {

			p.closeDoneLocked()
		}
	}
	return p.donec
}

const (
	http2prefaceTimeout        = 10 * time.Second
	http2firstSettingsTimeout  = 2 * time.Second // should be in-flight with preface anyway
	http2handlerChunkWriteSize = 4 << 10
	http2defaultMaxStreams     = 250 // TODO: make this 100 as the GFE seems to?
)

var (
	http2errClientDisconnected = errors.New("client disconnected")
	http2errClosedBody         = errors.New("body closed by handler")
	http2errHandlerComplete    = errors.New("http2: request body closed due to handler exiting")
	http2errStreamClosed       = errors.New("http2: stream closed")
)

var http2responseWriterStatePool = sync.Pool{
	New: func() interface{} {
		rws := &http2responseWriterState{}
		rws.bw = bufio.NewWriterSize(http2chunkWriter{rws}, http2handlerChunkWriteSize)
		return rws
	},
}

// Test hooks.
var (
	http2testHookOnConn        func()
	http2testHookGetServerConn func(*http2serverConn)
	http2testHookOnPanicMu     *sync.Mutex // nil except in tests
	http2testHookOnPanic       func(sc *http2serverConn, panicVal interface{}) (rePanic bool)
)

// Server is an HTTP/2 server.
type http2Server struct {
	// MaxHandlers limits the number of http.Handler ServeHTTP goroutines
	// which may run at a time over all connections.
	// Negative or zero no limit.
	// TODO: implement
	MaxHandlers int

	// MaxConcurrentStreams optionally specifies the number of
	// concurrent streams that each client may have open at a
	// time. This is unrelated to the number of http.Handler goroutines
	// which may be active globally, which is MaxHandlers.
	// If zero, MaxConcurrentStreams defaults to at least 100, per
	// the HTTP/2 spec's recommendations.
	MaxConcurrentStreams uint32

	// MaxReadFrameSize optionally specifies the largest frame
	// this server is willing to read. A valid value is between
	// 16k and 16M, inclusive. If zero or otherwise invalid, a
	// default value is used.
	MaxReadFrameSize uint32

	// PermitProhibitedCipherSuites, if true, permits the use of
	// cipher suites prohibited by the HTTP/2 spec.
	PermitProhibitedCipherSuites bool

	// IdleTimeout specifies how long until idle clients should be
	// closed with a GOAWAY frame. PING frames are not considered
	// activity for the purposes of IdleTimeout.
	IdleTimeout time.Duration

	// NewWriteScheduler constructs a write scheduler for a connection.
	// If nil, a default scheduler is chosen.
	NewWriteScheduler func() http2WriteScheduler
}

func (s *http2Server) maxReadFrameSize() uint32 {
	if v := s.MaxReadFrameSize; v >= http2minMaxFrameSize && v <= http2maxFrameSize {
		return v
	}
	return http2defaultMaxReadFrameSize
}

func (s *http2Server) maxConcurrentStreams() uint32 {
	if v := s.MaxConcurrentStreams; v > 0 {
		return v
	}
	return http2defaultMaxStreams
}

// ConfigureServer adds HTTP/2 support to a net/http Server.
//
// The configuration conf may be nil.
//
// ConfigureServer must be called before s begins serving.
func http2ConfigureServer(s *Server, conf *http2Server) error {
	if s == nil {
		panic("nil *http.Server")
	}
	if conf == nil {
		conf = new(http2Server)
	}
	if err := http2configureServer18(s, conf); err != nil {
		return err
	}

	if s.TLSConfig == nil {
		s.TLSConfig = new(tls.Config)
	} else if s.TLSConfig.CipherSuites != nil {
		// If they already provided a CipherSuite list, return
		// an error if it has a bad order or is missing
		// ECDHE_RSA_WITH_AES_128_GCM_SHA256.
		const requiredCipher = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
		haveRequired := false
		sawBad := false
		for i, cs := range s.TLSConfig.CipherSuites {
			if cs == requiredCipher {
				haveRequired = true
			}
			if http2isBadCipher(cs) {
				sawBad = true
			} else if sawBad {
				return fmt.Errorf("http2: TLSConfig.CipherSuites index %d contains an HTTP/2-approved cipher suite (%#04x), but it comes after unapproved cipher suites. With this configuration, clients that don't support previous, approved cipher suites may be given an unapproved one and reject the connection.", i, cs)
			}
		}
		if !haveRequired {
			return fmt.Errorf("http2: TLSConfig.CipherSuites is missing HTTP/2-required TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")
		}
	}

	s.TLSConfig.PreferServerCipherSuites = true

	haveNPN := false
	for _, p := range s.TLSConfig.NextProtos {
		if p == http2NextProtoTLS {
			haveNPN = true
			break
		}
	}
	if !haveNPN {
		s.TLSConfig.NextProtos = append(s.TLSConfig.NextProtos, http2NextProtoTLS)
	}

	if s.TLSNextProto == nil {
		s.TLSNextProto = map[string]func(*Server, *tls.Conn, Handler){}
	}
	protoHandler := func(hs *Server, c *tls.Conn, h Handler) {
		if http2testHookOnConn != nil {
			http2testHookOnConn()
		}
		conf.ServeConn(c, &http2ServeConnOpts{
			Handler:    h,
			BaseConfig: hs,
		})
	}
	s.TLSNextProto[http2NextProtoTLS] = protoHandler
	return nil
}

// ServeConnOpts are options for the Server.ServeConn method.
type http2ServeConnOpts struct {
	// BaseConfig optionally sets the base configuration
	// for values. If nil, defaults are used.
	BaseConfig *Server

	// Handler specifies which handler to use for processing
	// requests. If nil, BaseConfig.Handler is used. If BaseConfig
	// or BaseConfig.Handler is nil, http.DefaultServeMux is used.
	Handler Handler
}

func (o *http2ServeConnOpts) baseConfig() *Server {
	if o != nil && o.BaseConfig != nil {
		return o.BaseConfig
	}
	return new(Server)
}

func (o *http2ServeConnOpts) handler() Handler {
	if o != nil {
		if o.Handler != nil {
			return o.Handler
		}
		if o.BaseConfig != nil && o.BaseConfig.Handler != nil {
			return o.BaseConfig.Handler
		}
	}
	return DefaultServeMux
}

// ServeConn serves HTTP/2 requests on the provided connection and
// blocks until the connection is no longer readable.
//
// ServeConn starts speaking HTTP/2 assuming that c has not had any
// reads or writes. It writes its initial settings frame and expects
// to be able to read the preface and settings frame from the
// client. If c has a ConnectionState method like a *tls.Conn, the
// ConnectionState is used to verify the TLS ciphersuite and to set
// the Request.TLS field in Handlers.
//
// ServeConn does not support h2c by itself. Any h2c support must be
// implemented in terms of providing a suitably-behaving net.Conn.
//
// The opts parameter is optional. If nil, default values are used.
func (s *http2Server) ServeConn(c net.Conn, opts *http2ServeConnOpts) {
	baseCtx, cancel := http2serverConnBaseContext(c, opts)
	defer cancel()

	sc := &http2serverConn{
		srv:               s,
		hs:                opts.baseConfig(),
		conn:              c,
		baseCtx:           baseCtx,
		remoteAddrStr:     c.RemoteAddr().String(),
		bw:                http2newBufferedWriter(c),
		handler:           opts.handler(),
		streams:           make(map[uint32]*http2stream),
		readFrameCh:       make(chan http2readFrameResult),
		wantWriteFrameCh:  make(chan http2FrameWriteRequest, 8),
		wantStartPushCh:   make(chan http2startPushRequest, 8),
		wroteFrameCh:      make(chan http2frameWriteResult, 1),
		bodyReadCh:        make(chan http2bodyReadMsg),
		doneServing:       make(chan struct{}),
		clientMaxStreams:  math.MaxUint32,
		advMaxStreams:     s.maxConcurrentStreams(),
		initialWindowSize: http2initialWindowSize,
		maxFrameSize:      http2initialMaxFrameSize,
		headerTableSize:   http2initialHeaderTableSize,
		serveG:            http2newGoroutineLock(),
		pushEnabled:       true,
	}

	if sc.hs.WriteTimeout != 0 {
		sc.conn.SetWriteDeadline(time.Time{})
	}

	if s.NewWriteScheduler != nil {
		sc.writeSched = s.NewWriteScheduler()
	} else {
		sc.writeSched = http2NewRandomWriteScheduler()
	}

	sc.flow.add(http2initialWindowSize)
	sc.inflow.add(http2initialWindowSize)
	sc.hpackEncoder = hpack.NewEncoder(&sc.headerWriteBuf)

	fr := http2NewFramer(sc.bw, c)
	fr.ReadMetaHeaders = hpack.NewDecoder(http2initialHeaderTableSize, nil)
	fr.MaxHeaderListSize = sc.maxHeaderListSize()
	fr.SetMaxReadFrameSize(s.maxReadFrameSize())
	sc.framer = fr

	if tc, ok := c.(http2connectionStater); ok {
		sc.tlsState = new(tls.ConnectionState)
		*sc.tlsState = tc.ConnectionState()

		if sc.tlsState.Version < tls.VersionTLS12 {
			sc.rejectConn(http2ErrCodeInadequateSecurity, "TLS version too low")
			return
		}

		if sc.tlsState.ServerName == "" {

		}

		if !s.PermitProhibitedCipherSuites && http2isBadCipher(sc.tlsState.CipherSuite) {

			sc.rejectConn(http2ErrCodeInadequateSecurity, fmt.Sprintf("Prohibited TLS 1.2 Cipher Suite: %x", sc.tlsState.CipherSuite))
			return
		}
	}

	if hook := http2testHookGetServerConn; hook != nil {
		hook(sc)
	}
	sc.serve()
}

func (sc *http2serverConn) rejectConn(err http2ErrCode, debug string) {
	sc.vlogf("http2: server rejecting conn: %v, %s", err, debug)

	sc.framer.WriteGoAway(0, err, []byte(debug))
	sc.bw.Flush()
	sc.conn.Close()
}

type http2serverConn struct {
	// Immutable:
	srv              *http2Server
	hs               *Server
	conn             net.Conn
	bw               *http2bufferedWriter // writing to conn
	handler          Handler
	baseCtx          http2contextContext
	framer           *http2Framer
	doneServing      chan struct{}               // closed when serverConn.serve ends
	readFrameCh      chan http2readFrameResult   // written by serverConn.readFrames
	wantWriteFrameCh chan http2FrameWriteRequest // from handlers -> serve
	wantStartPushCh  chan http2startPushRequest  // from handlers -> serve
	wroteFrameCh     chan http2frameWriteResult  // from writeFrameAsync -> serve, tickles more frame writes
	bodyReadCh       chan http2bodyReadMsg       // from handlers -> serve
	testHookCh       chan func(int)              // code to run on the serve loop
	flow             http2flow                   // conn-wide (not stream-specific) outbound flow control
	inflow           http2flow                   // conn-wide inbound flow control
	tlsState         *tls.ConnectionState        // shared by all handlers, like net/http
	remoteAddrStr    string
	writeSched       http2WriteScheduler

	// Everything following is owned by the serve loop; use serveG.check():
	serveG                http2goroutineLock // used to verify funcs are on serve()
	pushEnabled           bool
	sawFirstSettings      bool // got the initial SETTINGS frame after the preface
	needToSendSettingsAck bool
	unackedSettings       int    // how many SETTINGS have we sent without ACKs?
	clientMaxStreams      uint32 // SETTINGS_MAX_CONCURRENT_STREAMS from client (our PUSH_PROMISE limit)
	advMaxStreams         uint32 // our SETTINGS_MAX_CONCURRENT_STREAMS advertised the client
	curClientStreams      uint32 // number of open streams initiated by the client
	curPushedStreams      uint32 // number of open streams initiated by server push
	maxClientStreamID     uint32 // max ever seen from client (odd), or 0 if there have been no client requests
	maxPushPromiseID      uint32 // ID of the last push promise (even), or 0 if there have been no pushes
	streams               map[uint32]*http2stream
	initialWindowSize     int32
	maxFrameSize          int32
	headerTableSize       uint32
	peerMaxHeaderListSize uint32            // zero means unknown (default)
	canonHeader           map[string]string // http2-lower-case -> Go-Canonical-Case
	writingFrame          bool              // started writing a frame (on serve goroutine or separate)
	writingFrameAsync     bool              // started a frame on its own goroutine but haven't heard back on wroteFrameCh
	needsFrameFlush       bool              // last frame write wasn't a flush
	inGoAway              bool              // we've started to or sent GOAWAY
	inFrameScheduleLoop   bool              // whether we're in the scheduleFrameWrite loop
	needToSendGoAway      bool              // we need to schedule a GOAWAY frame write
	goAwayCode            http2ErrCode
	shutdownTimerCh       <-chan time.Time // nil until used
	shutdownTimer         *time.Timer      // nil until used
	idleTimer             *time.Timer      // nil if unused
	idleTimerCh           <-chan time.Time // nil if unused

	// Owned by the writeFrameAsync goroutine:
	headerWriteBuf bytes.Buffer
	hpackEncoder   *hpack.Encoder
}

func (sc *http2serverConn) maxHeaderListSize() uint32 {
	n := sc.hs.MaxHeaderBytes
	if n <= 0 {
		n = DefaultMaxHeaderBytes
	}
	// http2's count is in a slightly different unit and includes 32 bytes per pair.
	// So, take the net/http.Server value and pad it up a bit, assuming 10 headers.
	const perFieldOverhead = 32 // per http2 spec
	const typicalHeaders = 10   // conservative
	return uint32(n + typicalHeaders*perFieldOverhead)
}

func (sc *http2serverConn) curOpenStreams() uint32 {
	sc.serveG.check()
	return sc.curClientStreams + sc.curPushedStreams
}

// stream represents a stream. This is the minimal metadata needed by
// the serve goroutine. Most of the actual stream state is owned by
// the http.Handler's goroutine in the responseWriter. Because the
// responseWriter's responseWriterState is recycled at the end of a
// handler, this struct intentionally has no pointer to the
// *responseWriter{,State} itself, as the Handler ending nils out the
// responseWriter's state field.
type http2stream struct {
	// immutable:
	sc        *http2serverConn
	id        uint32
	body      *http2pipe       // non-nil if expecting DATA frames
	cw        http2closeWaiter // closed wait stream transitions to closed state
	ctx       http2contextContext
	cancelCtx func()

	// owned by serverConn's serve loop:
	bodyBytes        int64        // body bytes seen so far
	declBodyBytes    int64        // or -1 if undeclared
	flow             http2flow    // limits writing from Handler to client
	inflow           http2flow    // what the client is allowed to POST/etc to us
	parent           *http2stream // or nil
	numTrailerValues int64
	weight           uint8
	state            http2streamState
	resetQueued      bool   // RST_STREAM queued for write; set by sc.resetStream
	gotTrailerHeader bool   // HEADER frame for trailers was seen
	wroteHeaders     bool   // whether we wrote headers (not status 100)
	reqBuf           []byte // if non-nil, body pipe buffer to return later at EOF

	trailer    Header // accumulated trailers
	reqTrailer Header // handler's Request.Trailer
}

func (sc *http2serverConn) Framer() *http2Framer { return sc.framer }

func (sc *http2serverConn) CloseConn() error { return sc.conn.Close() }

func (sc *http2serverConn) Flush() error { return sc.bw.Flush() }

func (sc *http2serverConn) HeaderEncoder() (*hpack.Encoder, *bytes.Buffer) {
	return sc.hpackEncoder, &sc.headerWriteBuf
}

func (sc *http2serverConn) state(streamID uint32) (http2streamState, *http2stream) {
	sc.serveG.check()

	if st, ok := sc.streams[streamID]; ok {
		return st.state, st
	}

	if streamID%2 == 1 {
		if streamID <= sc.maxClientStreamID {
			return http2stateClosed, nil
		}
	} else {
		if streamID <= sc.maxPushPromiseID {
			return http2stateClosed, nil
		}
	}
	return http2stateIdle, nil
}

// setConnState calls the net/http ConnState hook for this connection, if configured.
// Note that the net/http package does StateNew and StateClosed for us.
// There is currently no plan for StateHijacked or hijacking HTTP/2 connections.
func (sc *http2serverConn) setConnState(state ConnState) {
	if sc.hs.ConnState != nil {
		sc.hs.ConnState(sc.conn, state)
	}
}

func (sc *http2serverConn) vlogf(format string, args ...interface{}) {
	if http2VerboseLogs {
		sc.logf(format, args...)
	}
}

func (sc *http2serverConn) logf(format string, args ...interface{}) {
	if lg := sc.hs.ErrorLog; lg != nil {
		lg.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// errno returns v's underlying uintptr, else 0.
//
// TODO: remove this helper function once http2 can use build
// tags. See comment in isClosedConnError.
func http2errno(v error) uintptr {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Uintptr {
		return uintptr(rv.Uint())
	}
	return 0
}

// isClosedConnError reports whether err is an error from use of a closed
// network connection.
func http2isClosedConnError(err error) bool {
	if err == nil {
		return false
	}

	str := err.Error()
	if strings.Contains(str, "use of closed network connection") {
		return true
	}

	if runtime.GOOS == "windows" {
		if oe, ok := err.(*net.OpError); ok && oe.Op == "read" {
			if se, ok := oe.Err.(*os.SyscallError); ok && se.Syscall == "wsarecv" {
				const WSAECONNABORTED = 10053
				const WSAECONNRESET = 10054
				if n := http2errno(se.Err); n == WSAECONNRESET || n == WSAECONNABORTED {
					return true
				}
			}
		}
	}
	return false
}

func (sc *http2serverConn) condlogf(err error, format string, args ...interface{}) {
	if err == nil {
		return
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF || http2isClosedConnError(err) {

		sc.vlogf(format, args...)
	} else {
		sc.logf(format, args...)
	}
}

func (sc *http2serverConn) canonicalHeader(v string) string {
	sc.serveG.check()
	cv, ok := http2commonCanonHeader[v]
	if ok {
		return cv
	}
	cv, ok = sc.canonHeader[v]
	if ok {
		return cv
	}
	if sc.canonHeader == nil {
		sc.canonHeader = make(map[string]string)
	}
	cv = CanonicalHeaderKey(v)
	sc.canonHeader[v] = cv
	return cv
}

type http2readFrameResult struct {
	f   http2Frame // valid until readMore is called
	err error

	// readMore should be called once the consumer no longer needs or
	// retains f. After readMore, f is invalid and more frames can be
	// read.
	readMore func()
}

// readFrames is the loop that reads incoming frames.
// It takes care to only read one frame at a time, blocking until the
// consumer is done with the frame.
// It's run on its own goroutine.
func (sc *http2serverConn) readFrames() {
	gate := make(http2gate)
	gateDone := gate.Done
	for {
		f, err := sc.framer.ReadFrame()
		select {
		case sc.readFrameCh <- http2readFrameResult{f, err, gateDone}:
		case <-sc.doneServing:
			return
		}
		select {
		case <-gate:
		case <-sc.doneServing:
			return
		}
		if http2terminalReadFrameError(err) {
			return
		}
	}
}

// frameWriteResult is the message passed from writeFrameAsync to the serve goroutine.
type http2frameWriteResult struct {
	wr  http2FrameWriteRequest // what was written (or attempted)
	err error                  // result of the writeFrame call
}

// writeFrameAsync runs in its own goroutine and writes a single frame
// and then reports when it's done.
// At most one goroutine can be running writeFrameAsync at a time per
// serverConn.
func (sc *http2serverConn) writeFrameAsync(wr http2FrameWriteRequest) {
	err := wr.write.writeFrame(sc)
	sc.wroteFrameCh <- http2frameWriteResult{wr, err}
}

func (sc *http2serverConn) closeAllStreamsOnConnClose() {
	sc.serveG.check()
	for _, st := range sc.streams {
		sc.closeStream(st, http2errClientDisconnected)
	}
}

func (sc *http2serverConn) stopShutdownTimer() {
	sc.serveG.check()
	if t := sc.shutdownTimer; t != nil {
		t.Stop()
	}
}

func (sc *http2serverConn) notePanic() {

	if http2testHookOnPanicMu != nil {
		http2testHookOnPanicMu.Lock()
		defer http2testHookOnPanicMu.Unlock()
	}
	if http2testHookOnPanic != nil {
		if e := recover(); e != nil {
			if http2testHookOnPanic(sc, e) {
				panic(e)
			}
		}
	}
}

func (sc *http2serverConn) serve() {
	sc.serveG.check()
	defer sc.notePanic()
	defer sc.conn.Close()
	defer sc.closeAllStreamsOnConnClose()
	defer sc.stopShutdownTimer()
	defer close(sc.doneServing)

	if http2VerboseLogs {
		sc.vlogf("http2: server connection from %v on %p", sc.conn.RemoteAddr(), sc.hs)
	}

	sc.writeFrame(http2FrameWriteRequest{
		write: http2writeSettings{
			{http2SettingMaxFrameSize, sc.srv.maxReadFrameSize()},
			{http2SettingMaxConcurrentStreams, sc.advMaxStreams},
			{http2SettingMaxHeaderListSize, sc.maxHeaderListSize()},
		},
	})
	sc.unackedSettings++

	if err := sc.readPreface(); err != nil {
		sc.condlogf(err, "http2: server: error reading preface from client %v: %v", sc.conn.RemoteAddr(), err)
		return
	}

	sc.setConnState(StateActive)
	sc.setConnState(StateIdle)

	if sc.srv.IdleTimeout != 0 {
		sc.idleTimer = time.NewTimer(sc.srv.IdleTimeout)
		defer sc.idleTimer.Stop()
		sc.idleTimerCh = sc.idleTimer.C
	}

	var gracefulShutdownCh chan struct{}
	if sc.hs != nil {
		ch := http2h1ServerShutdownChan(sc.hs)
		if ch != nil {
			gracefulShutdownCh = make(chan struct{})
			go sc.awaitGracefulShutdown(ch, gracefulShutdownCh)
		}
	}

	go sc.readFrames()

	settingsTimer := time.NewTimer(http2firstSettingsTimeout)
	loopNum := 0
	for {
		loopNum++
		select {
		case wr := <-sc.wantWriteFrameCh:
			sc.writeFrame(wr)
		case spr := <-sc.wantStartPushCh:
			sc.startPush(spr)
		case res := <-sc.wroteFrameCh:
			sc.wroteFrame(res)
		case res := <-sc.readFrameCh:
			if !sc.processFrameFromReader(res) {
				return
			}
			res.readMore()
			if settingsTimer.C != nil {
				settingsTimer.Stop()
				settingsTimer.C = nil
			}
		case m := <-sc.bodyReadCh:
			sc.noteBodyRead(m.st, m.n)
		case <-settingsTimer.C:
			sc.logf("timeout waiting for SETTINGS frames from %v", sc.conn.RemoteAddr())
			return
		case <-gracefulShutdownCh:
			gracefulShutdownCh = nil
			sc.startGracefulShutdown()
		case <-sc.shutdownTimerCh:
			sc.vlogf("GOAWAY close timer fired; closing conn from %v", sc.conn.RemoteAddr())
			return
		case <-sc.idleTimerCh:
			sc.vlogf("connection is idle")
			sc.goAway(http2ErrCodeNo)
		case fn := <-sc.testHookCh:
			fn(loopNum)
		}

		if sc.inGoAway && sc.curOpenStreams() == 0 && !sc.needToSendGoAway && !sc.writingFrame {
			return
		}
	}
}

func (sc *http2serverConn) awaitGracefulShutdown(sharedCh <-chan struct{}, privateCh chan struct{}) {
	select {
	case <-sc.doneServing:
	case <-sharedCh:
		close(privateCh)
	}
}

// readPreface reads the ClientPreface greeting from the peer
// or returns an error on timeout or an invalid greeting.
func (sc *http2serverConn) readPreface() error {
	errc := make(chan error, 1)
	go func() {

		buf := make([]byte, len(http2ClientPreface))
		if _, err := io.ReadFull(sc.conn, buf); err != nil {
			errc <- err
		} else if !bytes.Equal(buf, http2clientPreface) {
			errc <- fmt.Errorf("bogus greeting %q", buf)
		} else {
			errc <- nil
		}
	}()
	timer := time.NewTimer(http2prefaceTimeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		return errors.New("timeout waiting for client preface")
	case err := <-errc:
		if err == nil {
			if http2VerboseLogs {
				sc.vlogf("http2: server: client %v said hello", sc.conn.RemoteAddr())
			}
		}
		return err
	}
}

var http2errChanPool = sync.Pool{
	New: func() interface{} { return make(chan error, 1) },
}

var http2writeDataPool = sync.Pool{
	New: func() interface{} { return new(http2writeData) },
}

// writeDataFromHandler writes DATA response frames from a handler on
// the given stream.
func (sc *http2serverConn) writeDataFromHandler(stream *http2stream, data []byte, endStream bool) error {
	ch := http2errChanPool.Get().(chan error)
	writeArg := http2writeDataPool.Get().(*http2writeData)
	*writeArg = http2writeData{stream.id, data, endStream}
	err := sc.writeFrameFromHandler(http2FrameWriteRequest{
		write:  writeArg,
		stream: stream,
		done:   ch,
	})
	if err != nil {
		return err
	}
	var frameWriteDone bool // the frame write is done (successfully or not)
	select {
	case err = <-ch:
		frameWriteDone = true
	case <-sc.doneServing:
		return http2errClientDisconnected
	case <-stream.cw:

		select {
		case err = <-ch:
			frameWriteDone = true
		default:
			return http2errStreamClosed
		}
	}
	http2errChanPool.Put(ch)
	if frameWriteDone {
		http2writeDataPool.Put(writeArg)
	}
	return err
}

// writeFrameFromHandler sends wr to sc.wantWriteFrameCh, but aborts
// if the connection has gone away.
//
// This must not be run from the serve goroutine itself, else it might
// deadlock writing to sc.wantWriteFrameCh (which is only mildly
// buffered and is read by serve itself). If you're on the serve
// goroutine, call writeFrame instead.
func (sc *http2serverConn) writeFrameFromHandler(wr http2FrameWriteRequest) error {
	sc.serveG.checkNotOn()
	select {
	case sc.wantWriteFrameCh <- wr:
		return nil
	case <-sc.doneServing:

		return http2errClientDisconnected
	}
}

// writeFrame schedules a frame to write and sends it if there's nothing
// already being written.
//
// There is no pushback here (the serve goroutine never blocks). It's
// the http.Handlers that block, waiting for their previous frames to
// make it onto the wire
//
// If you're not on the serve goroutine, use writeFrameFromHandler instead.
func (sc *http2serverConn) writeFrame(wr http2FrameWriteRequest) {
	sc.serveG.check()

	// If true, wr will not be written and wr.done will not be signaled.
	var ignoreWrite bool

	if wr.StreamID() != 0 {
		_, isReset := wr.write.(http2StreamError)
		if state, _ := sc.state(wr.StreamID()); state == http2stateClosed && !isReset {
			ignoreWrite = true
		}
	}

	switch wr.write.(type) {
	case *http2writeResHeaders:
		wr.stream.wroteHeaders = true
	case http2write100ContinueHeadersFrame:
		if wr.stream.wroteHeaders {

			if wr.done != nil {
				panic("wr.done != nil for write100ContinueHeadersFrame")
			}
			ignoreWrite = true
		}
	}

	if !ignoreWrite {
		sc.writeSched.Push(wr)
	}
	sc.scheduleFrameWrite()
}

// startFrameWrite starts a goroutine to write wr (in a separate
// goroutine since that might block on the network), and updates the
// serve goroutine's state about the world, updated from info in wr.
func (sc *http2serverConn) startFrameWrite(wr http2FrameWriteRequest) {
	sc.serveG.check()
	if sc.writingFrame {
		panic("internal error: can only be writing one frame at a time")
	}

	st := wr.stream
	if st != nil {
		switch st.state {
		case http2stateHalfClosedLocal:
			switch wr.write.(type) {
			case http2StreamError, http2handlerPanicRST, http2writeWindowUpdate:

			default:
				panic(fmt.Sprintf("internal error: attempt to send frame on a half-closed-local stream: %v", wr))
			}
		case http2stateClosed:
			panic(fmt.Sprintf("internal error: attempt to send frame on a closed stream: %v", wr))
		}
	}
	if wpp, ok := wr.write.(*http2writePushPromise); ok {
		var err error
		wpp.promisedID, err = wpp.allocatePromisedID()
		if err != nil {
			sc.writingFrameAsync = false
			wr.replyToWriter(err)
			return
		}
	}

	sc.writingFrame = true
	sc.needsFrameFlush = true
	if wr.write.staysWithinBuffer(sc.bw.Available()) {
		sc.writingFrameAsync = false
		err := wr.write.writeFrame(sc)
		sc.wroteFrame(http2frameWriteResult{wr, err})
	} else {
		sc.writingFrameAsync = true
		go sc.writeFrameAsync(wr)
	}
}

// errHandlerPanicked is the error given to any callers blocked in a read from
// Request.Body when the main goroutine panics. Since most handlers read in the
// the main ServeHTTP goroutine, this will show up rarely.
var http2errHandlerPanicked = errors.New("http2: handler panicked")

// wroteFrame is called on the serve goroutine with the result of
// whatever happened on writeFrameAsync.
func (sc *http2serverConn) wroteFrame(res http2frameWriteResult) {
	sc.serveG.check()
	if !sc.writingFrame {
		panic("internal error: expected to be already writing a frame")
	}
	sc.writingFrame = false
	sc.writingFrameAsync = false

	wr := res.wr

	if http2writeEndsStream(wr.write) {
		st := wr.stream
		if st == nil {
			panic("internal error: expecting non-nil stream")
		}
		switch st.state {
		case http2stateOpen:

			st.state = http2stateHalfClosedLocal
			sc.resetStream(http2streamError(st.id, http2ErrCodeCancel))
		case http2stateHalfClosedRemote:
			sc.closeStream(st, http2errHandlerComplete)
		}
	} else {
		switch v := wr.write.(type) {
		case http2StreamError:

			if st, ok := sc.streams[v.StreamID]; ok {
				sc.closeStream(st, v)
			}
		case http2handlerPanicRST:
			sc.closeStream(wr.stream, http2errHandlerPanicked)
		}
	}

	wr.replyToWriter(res.err)

	sc.scheduleFrameWrite()
}

// scheduleFrameWrite tickles the frame writing scheduler.
//
// If a frame is already being written, nothing happens. This will be called again
// when the frame is done being written.
//
// If a frame isn't being written we need to send one, the best frame
// to send is selected, preferring first things that aren't
// stream-specific (e.g. ACKing settings), and then finding the
// highest priority stream.
//
// If a frame isn't being written and there's nothing else to send, we
// flush the write buffer.
func (sc *http2serverConn) scheduleFrameWrite() {
	sc.serveG.check()
	if sc.writingFrame || sc.inFrameScheduleLoop {
		return
	}
	sc.inFrameScheduleLoop = true
	for !sc.writingFrameAsync {
		if sc.needToSendGoAway {
			sc.needToSendGoAway = false
			sc.startFrameWrite(http2FrameWriteRequest{
				write: &http2writeGoAway{
					maxStreamID: sc.maxClientStreamID,
					code:        sc.goAwayCode,
				},
			})
			continue
		}
		if sc.needToSendSettingsAck {
			sc.needToSendSettingsAck = false
			sc.startFrameWrite(http2FrameWriteRequest{write: http2writeSettingsAck{}})
			continue
		}
		if !sc.inGoAway || sc.goAwayCode == http2ErrCodeNo {
			if wr, ok := sc.writeSched.Pop(); ok {
				sc.startFrameWrite(wr)
				continue
			}
		}
		if sc.needsFrameFlush {
			sc.startFrameWrite(http2FrameWriteRequest{write: http2flushFrameWriter{}})
			sc.needsFrameFlush = false
			continue
		}
		break
	}
	sc.inFrameScheduleLoop = false
}

// startGracefulShutdown sends a GOAWAY with ErrCodeNo to tell the
// client we're gracefully shutting down. The connection isn't closed
// until all current streams are done.
func (sc *http2serverConn) startGracefulShutdown() {
	sc.goAwayIn(http2ErrCodeNo, 0)
}

func (sc *http2serverConn) goAway(code http2ErrCode) {
	sc.serveG.check()
	var forceCloseIn time.Duration
	if code != http2ErrCodeNo {
		forceCloseIn = 250 * time.Millisecond
	} else {

		forceCloseIn = 1 * time.Second
	}
	sc.goAwayIn(code, forceCloseIn)
}

func (sc *http2serverConn) goAwayIn(code http2ErrCode, forceCloseIn time.Duration) {
	sc.serveG.check()
	if sc.inGoAway {
		return
	}
	if forceCloseIn != 0 {
		sc.shutDownIn(forceCloseIn)
	}
	sc.inGoAway = true
	sc.needToSendGoAway = true
	sc.goAwayCode = code
	sc.scheduleFrameWrite()
}

func (sc *http2serverConn) shutDownIn(d time.Duration) {
	sc.serveG.check()
	sc.shutdownTimer = time.NewTimer(d)
	sc.shutdownTimerCh = sc.shutdownTimer.C
}

func (sc *http2serverConn) resetStream(se http2StreamError) {
	sc.serveG.check()
	sc.writeFrame(http2FrameWriteRequest{write: se})
	if st, ok := sc.streams[se.StreamID]; ok {
		st.resetQueued = true
	}
}

// processFrameFromReader processes the serve loop's read from readFrameCh from the
// frame-reading goroutine.
// processFrameFromReader returns whether the connection should be kept open.
func (sc *http2serverConn) processFrameFromReader(res http2readFrameResult) bool {
	sc.serveG.check()
	err := res.err
	if err != nil {
		if err == http2ErrFrameTooLarge {
			sc.goAway(http2ErrCodeFrameSize)
			return true
		}
		clientGone := err == io.EOF || err == io.ErrUnexpectedEOF || http2isClosedConnError(err)
		if clientGone {

			return false
		}
	} else {
		f := res.f
		if http2VerboseLogs {
			sc.vlogf("http2: server read frame %v", http2summarizeFrame(f))
		}
		err = sc.processFrame(f)
		if err == nil {
			return true
		}
	}

	switch ev := err.(type) {
	case http2StreamError:
		sc.resetStream(ev)
		return true
	case http2goAwayFlowError:
		sc.goAway(http2ErrCodeFlowControl)
		return true
	case http2ConnectionError:
		sc.logf("http2: server connection error from %v: %v", sc.conn.RemoteAddr(), ev)
		sc.goAway(http2ErrCode(ev))
		return true
	default:
		if res.err != nil {
			sc.vlogf("http2: server closing client connection; error reading frame from client %s: %v", sc.conn.RemoteAddr(), err)
		} else {
			sc.logf("http2: server closing client connection: %v", err)
		}
		return false
	}
}

func (sc *http2serverConn) processFrame(f http2Frame) error {
	sc.serveG.check()

	if !sc.sawFirstSettings {
		if _, ok := f.(*http2SettingsFrame); !ok {
			return http2ConnectionError(http2ErrCodeProtocol)
		}
		sc.sawFirstSettings = true
	}

	switch f := f.(type) {
	case *http2SettingsFrame:
		return sc.processSettings(f)
	case *http2MetaHeadersFrame:
		return sc.processHeaders(f)
	case *http2WindowUpdateFrame:
		return sc.processWindowUpdate(f)
	case *http2PingFrame:
		return sc.processPing(f)
	case *http2DataFrame:
		return sc.processData(f)
	case *http2RSTStreamFrame:
		return sc.processResetStream(f)
	case *http2PriorityFrame:
		return sc.processPriority(f)
	case *http2GoAwayFrame:
		return sc.processGoAway(f)
	case *http2PushPromiseFrame:

		return http2ConnectionError(http2ErrCodeProtocol)
	default:
		sc.vlogf("http2: server ignoring frame: %v", f.Header())
		return nil
	}
}

func (sc *http2serverConn) processPing(f *http2PingFrame) error {
	sc.serveG.check()
	if f.IsAck() {

		return nil
	}
	if f.StreamID != 0 {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if sc.inGoAway && sc.goAwayCode != http2ErrCodeNo {
		return nil
	}
	sc.writeFrame(http2FrameWriteRequest{write: http2writePingAck{f}})
	return nil
}

func (sc *http2serverConn) processWindowUpdate(f *http2WindowUpdateFrame) error {
	sc.serveG.check()
	switch {
	case f.StreamID != 0:
		state, st := sc.state(f.StreamID)
		if state == http2stateIdle {

			return http2ConnectionError(http2ErrCodeProtocol)
		}
		if st == nil {

			return nil
		}
		if !st.flow.add(int32(f.Increment)) {
			return http2streamError(f.StreamID, http2ErrCodeFlowControl)
		}
	default:
		if !sc.flow.add(int32(f.Increment)) {
			return http2goAwayFlowError{}
		}
	}
	sc.scheduleFrameWrite()
	return nil
}

func (sc *http2serverConn) processResetStream(f *http2RSTStreamFrame) error {
	sc.serveG.check()

	state, st := sc.state(f.StreamID)
	if state == http2stateIdle {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if st != nil {
		st.cancelCtx()
		sc.closeStream(st, http2streamError(f.StreamID, f.ErrCode))
	}
	return nil
}

func (sc *http2serverConn) closeStream(st *http2stream, err error) {
	sc.serveG.check()
	if st.state == http2stateIdle || st.state == http2stateClosed {
		panic(fmt.Sprintf("invariant; can't close stream in state %v", st.state))
	}
	st.state = http2stateClosed
	if st.isPushed() {
		sc.curPushedStreams--
	} else {
		sc.curClientStreams--
	}
	delete(sc.streams, st.id)
	if len(sc.streams) == 0 {
		sc.setConnState(StateIdle)
		if sc.srv.IdleTimeout != 0 {
			sc.idleTimer.Reset(sc.srv.IdleTimeout)
		}
		if http2h1ServerKeepAlivesDisabled(sc.hs) {
			sc.startGracefulShutdown()
		}
	}
	if p := st.body; p != nil {

		sc.sendWindowUpdate(nil, p.Len())

		p.CloseWithError(err)
	}
	st.cw.Close()
	sc.writeSched.CloseStream(st.id)
}

func (sc *http2serverConn) processSettings(f *http2SettingsFrame) error {
	sc.serveG.check()
	if f.IsAck() {
		sc.unackedSettings--
		if sc.unackedSettings < 0 {

			return http2ConnectionError(http2ErrCodeProtocol)
		}
		return nil
	}
	if err := f.ForeachSetting(sc.processSetting); err != nil {
		return err
	}
	sc.needToSendSettingsAck = true
	sc.scheduleFrameWrite()
	return nil
}

func (sc *http2serverConn) processSetting(s http2Setting) error {
	sc.serveG.check()
	if err := s.Valid(); err != nil {
		return err
	}
	if http2VerboseLogs {
		sc.vlogf("http2: server processing setting %v", s)
	}
	switch s.ID {
	case http2SettingHeaderTableSize:
		sc.headerTableSize = s.Val
		sc.hpackEncoder.SetMaxDynamicTableSize(s.Val)
	case http2SettingEnablePush:
		sc.pushEnabled = s.Val != 0
	case http2SettingMaxConcurrentStreams:
		sc.clientMaxStreams = s.Val
	case http2SettingInitialWindowSize:
		return sc.processSettingInitialWindowSize(s.Val)
	case http2SettingMaxFrameSize:
		sc.maxFrameSize = int32(s.Val)
	case http2SettingMaxHeaderListSize:
		sc.peerMaxHeaderListSize = s.Val
	default:

		if http2VerboseLogs {
			sc.vlogf("http2: server ignoring unknown setting %v", s)
		}
	}
	return nil
}

func (sc *http2serverConn) processSettingInitialWindowSize(val uint32) error {
	sc.serveG.check()

	old := sc.initialWindowSize
	sc.initialWindowSize = int32(val)
	growth := sc.initialWindowSize - old
	for _, st := range sc.streams {
		if !st.flow.add(growth) {

			return http2ConnectionError(http2ErrCodeFlowControl)
		}
	}
	return nil
}

func (sc *http2serverConn) processData(f *http2DataFrame) error {
	sc.serveG.check()
	if sc.inGoAway && sc.goAwayCode != http2ErrCodeNo {
		return nil
	}
	data := f.Data()

	id := f.Header().StreamID
	state, st := sc.state(id)
	if id == 0 || state == http2stateIdle {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if st == nil || state != http2stateOpen || st.gotTrailerHeader || st.resetQueued {

		if sc.inflow.available() < int32(f.Length) {
			return http2streamError(id, http2ErrCodeFlowControl)
		}

		sc.inflow.take(int32(f.Length))
		sc.sendWindowUpdate(nil, int(f.Length))

		if st != nil && st.resetQueued {

			return nil
		}
		return http2streamError(id, http2ErrCodeStreamClosed)
	}
	if st.body == nil {
		panic("internal error: should have a body in this state")
	}

	if st.declBodyBytes != -1 && st.bodyBytes+int64(len(data)) > st.declBodyBytes {
		st.body.CloseWithError(fmt.Errorf("sender tried to send more than declared Content-Length of %d bytes", st.declBodyBytes))
		return http2streamError(id, http2ErrCodeStreamClosed)
	}
	if f.Length > 0 {

		if st.inflow.available() < int32(f.Length) {
			return http2streamError(id, http2ErrCodeFlowControl)
		}
		st.inflow.take(int32(f.Length))

		if len(data) > 0 {
			wrote, err := st.body.Write(data)
			if err != nil {
				return http2streamError(id, http2ErrCodeStreamClosed)
			}
			if wrote != len(data) {
				panic("internal error: bad Writer")
			}
			st.bodyBytes += int64(len(data))
		}

		if pad := int32(f.Length) - int32(len(data)); pad > 0 {
			sc.sendWindowUpdate32(nil, pad)
			sc.sendWindowUpdate32(st, pad)
		}
	}
	if f.StreamEnded() {
		st.endStream()
	}
	return nil
}

func (sc *http2serverConn) processGoAway(f *http2GoAwayFrame) error {
	sc.serveG.check()
	if f.ErrCode != http2ErrCodeNo {
		sc.logf("http2: received GOAWAY %+v, starting graceful shutdown", f)
	} else {
		sc.vlogf("http2: received GOAWAY %+v, starting graceful shutdown", f)
	}
	sc.startGracefulShutdown()

	sc.pushEnabled = false
	return nil
}

// isPushed reports whether the stream is server-initiated.
func (st *http2stream) isPushed() bool {
	return st.id%2 == 0
}

// endStream closes a Request.Body's pipe. It is called when a DATA
// frame says a request body is over (or after trailers).
func (st *http2stream) endStream() {
	sc := st.sc
	sc.serveG.check()

	if st.declBodyBytes != -1 && st.declBodyBytes != st.bodyBytes {
		st.body.CloseWithError(fmt.Errorf("request declared a Content-Length of %d but only wrote %d bytes",
			st.declBodyBytes, st.bodyBytes))
	} else {
		st.body.closeWithErrorAndCode(io.EOF, st.copyTrailersToHandlerRequest)
		st.body.CloseWithError(io.EOF)
	}
	st.state = http2stateHalfClosedRemote
}

// copyTrailersToHandlerRequest is run in the Handler's goroutine in
// its Request.Body.Read just before it gets io.EOF.
func (st *http2stream) copyTrailersToHandlerRequest() {
	for k, vv := range st.trailer {
		if _, ok := st.reqTrailer[k]; ok {

			st.reqTrailer[k] = vv
		}
	}
}

func (sc *http2serverConn) processHeaders(f *http2MetaHeadersFrame) error {
	sc.serveG.check()
	id := f.StreamID
	if sc.inGoAway {

		return nil
	}

	if id%2 != 1 {
		return http2ConnectionError(http2ErrCodeProtocol)
	}

	if st := sc.streams[f.StreamID]; st != nil {
		if st.resetQueued {

			return nil
		}
		return st.processTrailerHeaders(f)
	}

	if id <= sc.maxClientStreamID {
		return http2ConnectionError(http2ErrCodeProtocol)
	}
	sc.maxClientStreamID = id

	if sc.idleTimer != nil {
		sc.idleTimer.Stop()
	}

	if sc.curClientStreams+1 > sc.advMaxStreams {
		if sc.unackedSettings == 0 {

			return http2streamError(id, http2ErrCodeProtocol)
		}

		return http2streamError(id, http2ErrCodeRefusedStream)
	}

	initialState := http2stateOpen
	if f.StreamEnded() {
		initialState = http2stateHalfClosedRemote
	}
	st := sc.newStream(id, 0, initialState)

	if f.HasPriority() {
		if err := http2checkPriority(f.StreamID, f.Priority); err != nil {
			return err
		}
		sc.writeSched.AdjustStream(st.id, f.Priority)
	}

	rw, req, err := sc.newWriterAndRequest(st, f)
	if err != nil {
		return err
	}
	st.reqTrailer = req.Trailer
	if st.reqTrailer != nil {
		st.trailer = make(Header)
	}
	st.body = req.Body.(*http2requestBody).pipe
	st.declBodyBytes = req.ContentLength

	handler := sc.handler.ServeHTTP
	if f.Truncated {

		handler = http2handleHeaderListTooLong
	} else if err := http2checkValidHTTP2RequestHeaders(req.Header); err != nil {
		handler = http2new400Handler(err)
	}

	if sc.hs.ReadTimeout != 0 {
		sc.conn.SetReadDeadline(time.Time{})
	}

	go sc.runHandler(rw, req, handler)
	return nil
}

func (st *http2stream) processTrailerHeaders(f *http2MetaHeadersFrame) error {
	sc := st.sc
	sc.serveG.check()
	if st.gotTrailerHeader {
		return http2ConnectionError(http2ErrCodeProtocol)
	}
	st.gotTrailerHeader = true
	if !f.StreamEnded() {
		return http2streamError(st.id, http2ErrCodeProtocol)
	}

	if len(f.PseudoFields()) > 0 {
		return http2streamError(st.id, http2ErrCodeProtocol)
	}
	if st.trailer != nil {
		for _, hf := range f.RegularFields() {
			key := sc.canonicalHeader(hf.Name)
			if !http2ValidTrailerHeader(key) {

				return http2streamError(st.id, http2ErrCodeProtocol)
			}
			st.trailer[key] = append(st.trailer[key], hf.Value)
		}
	}
	st.endStream()
	return nil
}

func http2checkPriority(streamID uint32, p http2PriorityParam) error {
	if streamID == p.StreamDep {

		return http2streamError(streamID, http2ErrCodeProtocol)
	}
	return nil
}

func (sc *http2serverConn) processPriority(f *http2PriorityFrame) error {
	if sc.inGoAway {
		return nil
	}
	if err := http2checkPriority(f.StreamID, f.http2PriorityParam); err != nil {
		return err
	}
	sc.writeSched.AdjustStream(f.StreamID, f.http2PriorityParam)
	return nil
}

func (sc *http2serverConn) newStream(id, pusherID uint32, state http2streamState) *http2stream {
	sc.serveG.check()
	if id == 0 {
		panic("internal error: cannot create stream with id 0")
	}

	ctx, cancelCtx := http2contextWithCancel(sc.baseCtx)
	st := &http2stream{
		sc:        sc,
		id:        id,
		state:     state,
		ctx:       ctx,
		cancelCtx: cancelCtx,
	}
	st.cw.Init()
	st.flow.conn = &sc.flow
	st.flow.add(sc.initialWindowSize)
	st.inflow.conn = &sc.inflow
	st.inflow.add(http2initialWindowSize)

	sc.streams[id] = st
	sc.writeSched.OpenStream(st.id, http2OpenStreamOptions{PusherID: pusherID})
	if st.isPushed() {
		sc.curPushedStreams++
	} else {
		sc.curClientStreams++
	}
	if sc.curOpenStreams() == 1 {
		sc.setConnState(StateActive)
	}

	return st
}

func (sc *http2serverConn) newWriterAndRequest(st *http2stream, f *http2MetaHeadersFrame) (*http2responseWriter, *Request, error) {
	sc.serveG.check()

	rp := http2requestParam{
		method:    f.PseudoValue("method"),
		scheme:    f.PseudoValue("scheme"),
		authority: f.PseudoValue("authority"),
		path:      f.PseudoValue("path"),
	}

	isConnect := rp.method == "CONNECT"
	if isConnect {
		if rp.path != "" || rp.scheme != "" || rp.authority == "" {
			return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
		}
	} else if rp.method == "" || rp.path == "" || (rp.scheme != "https" && rp.scheme != "http") {

		return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
	}

	bodyOpen := !f.StreamEnded()
	if rp.method == "HEAD" && bodyOpen {

		return nil, nil, http2streamError(f.StreamID, http2ErrCodeProtocol)
	}

	rp.header = make(Header)
	for _, hf := range f.RegularFields() {
		rp.header.Add(sc.canonicalHeader(hf.Name), hf.Value)
	}
	if rp.authority == "" {
		rp.authority = rp.header.Get("Host")
	}

	rw, req, err := sc.newWriterAndRequestNoBody(st, rp)
	if err != nil {
		return nil, nil, err
	}
	if bodyOpen {
		st.reqBuf = http2getRequestBodyBuf()
		req.Body.(*http2requestBody).pipe = &http2pipe{
			b: &http2fixedBuffer{buf: st.reqBuf},
		}

		if vv, ok := rp.header["Content-Length"]; ok {
			req.ContentLength, _ = strconv.ParseInt(vv[0], 10, 64)
		} else {
			req.ContentLength = -1
		}
	}
	return rw, req, nil
}

type http2requestParam struct {
	method                  string
	scheme, authority, path string
	header                  Header
}

func (sc *http2serverConn) newWriterAndRequestNoBody(st *http2stream, rp http2requestParam) (*http2responseWriter, *Request, error) {
	sc.serveG.check()

	var tlsState *tls.ConnectionState // nil if not scheme https
	if rp.scheme == "https" {
		tlsState = sc.tlsState
	}

	needsContinue := rp.header.Get("Expect") == "100-continue"
	if needsContinue {
		rp.header.Del("Expect")
	}

	if cookies := rp.header["Cookie"]; len(cookies) > 1 {
		rp.header.Set("Cookie", strings.Join(cookies, "; "))
	}

	// Setup Trailers
	var trailer Header
	for _, v := range rp.header["Trailer"] {
		for _, key := range strings.Split(v, ",") {
			key = CanonicalHeaderKey(strings.TrimSpace(key))
			switch key {
			case "Transfer-Encoding", "Trailer", "Content-Length":

			default:
				if trailer == nil {
					trailer = make(Header)
				}
				trailer[key] = nil
			}
		}
	}
	delete(rp.header, "Trailer")

	var url_ *url.URL
	var requestURI string
	if rp.method == "CONNECT" {
		url_ = &url.URL{Host: rp.authority}
		requestURI = rp.authority
	} else {
		var err error
		url_, err = url.ParseRequestURI(rp.path)
		if err != nil {
			return nil, nil, http2streamError(st.id, http2ErrCodeProtocol)
		}
		requestURI = rp.path
	}

	body := &http2requestBody{
		conn:          sc,
		stream:        st,
		needsContinue: needsContinue,
	}
	req := &Request{
		Method:     rp.method,
		URL:        url_,
		RemoteAddr: sc.remoteAddrStr,
		Header:     rp.header,
		RequestURI: requestURI,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		TLS:        tlsState,
		Host:       rp.authority,
		Body:       body,
		Trailer:    trailer,
	}
	req = http2requestWithContext(req, st.ctx)

	rws := http2responseWriterStatePool.Get().(*http2responseWriterState)
	bwSave := rws.bw
	*rws = http2responseWriterState{}
	rws.conn = sc
	rws.bw = bwSave
	rws.bw.Reset(http2chunkWriter{rws})
	rws.stream = st
	rws.req = req
	rws.body = body

	rw := &http2responseWriter{rws: rws}
	return rw, req, nil
}

var http2reqBodyCache = make(chan []byte, 8)

func http2getRequestBodyBuf() []byte {
	select {
	case b := <-http2reqBodyCache:
		return b
	default:
		return make([]byte, http2initialWindowSize)
	}
}

func http2putRequestBodyBuf(b []byte) {
	select {
	case http2reqBodyCache <- b:
	default:
	}
}

// Run on its own goroutine.
func (sc *http2serverConn) runHandler(rw *http2responseWriter, req *Request, handler func(ResponseWriter, *Request)) {
	didPanic := true
	defer func() {
		rw.rws.stream.cancelCtx()
		if didPanic {
			e := recover()
			sc.writeFrameFromHandler(http2FrameWriteRequest{
				write:  http2handlerPanicRST{rw.rws.stream.id},
				stream: rw.rws.stream,
			})

			if http2shouldLogPanic(e) {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				sc.logf("http2: panic serving %v: %v\n%s", sc.conn.RemoteAddr(), e, buf)
			}
			return
		}
		rw.handlerDone()
	}()
	handler(rw, req)
	didPanic = false
}

func http2handleHeaderListTooLong(w ResponseWriter, r *Request) {
	// 10.5.1 Limits on Header Block Size:
	// .. "A server that receives a larger header block than it is
	// willing to handle can send an HTTP 431 (Request Header Fields Too
	// Large) status code"
	const statusRequestHeaderFieldsTooLarge = 431 // only in Go 1.6+
	w.WriteHeader(statusRequestHeaderFieldsTooLarge)
	io.WriteString(w, "<h1>HTTP Error 431</h1><p>Request Header Field(s) Too Large</p>")
}

// called from handler goroutines.
// h may be nil.
func (sc *http2serverConn) writeHeaders(st *http2stream, headerData *http2writeResHeaders) error {
	sc.serveG.checkNotOn()
	var errc chan error
	if headerData.h != nil {

		errc = http2errChanPool.Get().(chan error)
	}
	if err := sc.writeFrameFromHandler(http2FrameWriteRequest{
		write:  headerData,
		stream: st,
		done:   errc,
	}); err != nil {
		return err
	}
	if errc != nil {
		select {
		case err := <-errc:
			http2errChanPool.Put(errc)
			return err
		case <-sc.doneServing:
			return http2errClientDisconnected
		case <-st.cw:
			return http2errStreamClosed
		}
	}
	return nil
}

// called from handler goroutines.
func (sc *http2serverConn) write100ContinueHeaders(st *http2stream) {
	sc.writeFrameFromHandler(http2FrameWriteRequest{
		write:  http2write100ContinueHeadersFrame{st.id},
		stream: st,
	})
}

// A bodyReadMsg tells the server loop that the http.Handler read n
// bytes of the DATA from the client on the given stream.
type http2bodyReadMsg struct {
	st *http2stream
	n  int
}

// called from handler goroutines.
// Notes that the handler for the given stream ID read n bytes of its body
// and schedules flow control tokens to be sent.
func (sc *http2serverConn) noteBodyReadFromHandler(st *http2stream, n int, err error) {
	sc.serveG.checkNotOn()
	if n > 0 {
		select {
		case sc.bodyReadCh <- http2bodyReadMsg{st, n}:
		case <-sc.doneServing:
		}
	}
	if err == io.EOF {
		if buf := st.reqBuf; buf != nil {
			st.reqBuf = nil
			http2putRequestBodyBuf(buf)
		}
	}
}

func (sc *http2serverConn) noteBodyRead(st *http2stream, n int) {
	sc.serveG.check()
	sc.sendWindowUpdate(nil, n)
	if st.state != http2stateHalfClosedRemote && st.state != http2stateClosed {

		sc.sendWindowUpdate(st, n)
	}
}

// st may be nil for conn-level
func (sc *http2serverConn) sendWindowUpdate(st *http2stream, n int) {
	sc.serveG.check()
	// "The legal range for the increment to the flow control
	// window is 1 to 2^31-1 (2,147,483,647) octets."
	// A Go Read call on 64-bit machines could in theory read
	// a larger Read than this. Very unlikely, but we handle it here
	// rather than elsewhere for now.
	const maxUint31 = 1<<31 - 1
	for n >= maxUint31 {
		sc.sendWindowUpdate32(st, maxUint31)
		n -= maxUint31
	}
	sc.sendWindowUpdate32(st, int32(n))
}

// st may be nil for conn-level
func (sc *http2serverConn) sendWindowUpdate32(st *http2stream, n int32) {
	sc.serveG.check()
	if n == 0 {
		return
	}
	if n < 0 {
		panic("negative update")
	}
	var streamID uint32
	if st != nil {
		streamID = st.id
	}
	sc.writeFrame(http2FrameWriteRequest{
		write:  http2writeWindowUpdate{streamID: streamID, n: uint32(n)},
		stream: st,
	})
	var ok bool
	if st == nil {
		ok = sc.inflow.add(n)
	} else {
		ok = st.inflow.add(n)
	}
	if !ok {
		panic("internal error; sent too many window updates without decrements?")
	}
}

// requestBody is the Handler's Request.Body type.
// Read and Close may be called concurrently.
type http2requestBody struct {
	stream        *http2stream
	conn          *http2serverConn
	closed        bool       // for use by Close only
	sawEOF        bool       // for use by Read only
	pipe          *http2pipe // non-nil if we have a HTTP entity message body
	needsContinue bool       // need to send a 100-continue
}

func (b *http2requestBody) Close() error {
	if b.pipe != nil && !b.closed {
		b.pipe.BreakWithError(http2errClosedBody)
	}
	b.closed = true
	return nil
}

func (b *http2requestBody) Read(p []byte) (n int, err error) {
	if b.needsContinue {
		b.needsContinue = false
		b.conn.write100ContinueHeaders(b.stream)
	}
	if b.pipe == nil || b.sawEOF {
		return 0, io.EOF
	}
	n, err = b.pipe.Read(p)
	if err == io.EOF {
		b.sawEOF = true
	}
	if b.conn == nil && http2inTests {
		return
	}
	b.conn.noteBodyReadFromHandler(b.stream, n, err)
	return
}

// responseWriter is the http.ResponseWriter implementation.  It's
// intentionally small (1 pointer wide) to minimize garbage.  The
// responseWriterState pointer inside is zeroed at the end of a
// request (in handlerDone) and calls on the responseWriter thereafter
// simply crash (caller's mistake), but the much larger responseWriterState
// and buffers are reused between multiple requests.
type http2responseWriter struct {
	rws *http2responseWriterState
}

// Optional http.ResponseWriter interfaces implemented.
var (
	_ CloseNotifier     = (*http2responseWriter)(nil)
	_ Flusher           = (*http2responseWriter)(nil)
	_ http2stringWriter = (*http2responseWriter)(nil)
)

type http2responseWriterState struct {
	// immutable within a request:
	stream *http2stream
	req    *Request
	body   *http2requestBody // to close at end of request, if DATA frames didn't
	conn   *http2serverConn

	// TODO: adjust buffer writing sizes based on server config, frame size updates from peer, etc
	bw *bufio.Writer // writing to a chunkWriter{this *responseWriterState}

	// mutated by http.Handler goroutine:
	handlerHeader Header   // nil until called
	snapHeader    Header   // snapshot of handlerHeader at WriteHeader time
	trailers      []string // set in writeChunk
	status        int      // status code passed to WriteHeader
	wroteHeader   bool     // WriteHeader called (explicitly or implicitly). Not necessarily sent to user yet.
	sentHeader    bool     // have we sent the header frame?
	handlerDone   bool     // handler has finished

	sentContentLen int64 // non-zero if handler set a Content-Length header
	wroteBytes     int64

	closeNotifierMu sync.Mutex // guards closeNotifierCh
	closeNotifierCh chan bool  // nil until first used
}

type http2chunkWriter struct{ rws *http2responseWriterState }

func (cw http2chunkWriter) Write(p []byte) (n int, err error) { return cw.rws.writeChunk(p) }

func (rws *http2responseWriterState) hasTrailers() bool { return len(rws.trailers) != 0 }

// declareTrailer is called for each Trailer header when the
// response header is written. It notes that a header will need to be
// written in the trailers at the end of the response.
func (rws *http2responseWriterState) declareTrailer(k string) {
	k = CanonicalHeaderKey(k)
	if !http2ValidTrailerHeader(k) {

		rws.conn.logf("ignoring invalid trailer %q", k)
		return
	}
	if !http2strSliceContains(rws.trailers, k) {
		rws.trailers = append(rws.trailers, k)
	}
}

// writeChunk writes chunks from the bufio.Writer. But because
// bufio.Writer may bypass its chunking, sometimes p may be
// arbitrarily large.
//
// writeChunk is also responsible (on the first chunk) for sending the
// HEADER response.
func (rws *http2responseWriterState) writeChunk(p []byte) (n int, err error) {
	if !rws.wroteHeader {
		rws.writeHeader(200)
	}

	isHeadResp := rws.req.Method == "HEAD"
	if !rws.sentHeader {
		rws.sentHeader = true
		var ctype, clen string
		if clen = rws.snapHeader.Get("Content-Length"); clen != "" {
			rws.snapHeader.Del("Content-Length")
			clen64, err := strconv.ParseInt(clen, 10, 64)
			if err == nil && clen64 >= 0 {
				rws.sentContentLen = clen64
			} else {
				clen = ""
			}
		}
		if clen == "" && rws.handlerDone && http2bodyAllowedForStatus(rws.status) && (len(p) > 0 || !isHeadResp) {
			clen = strconv.Itoa(len(p))
		}
		_, hasContentType := rws.snapHeader["Content-Type"]
		if !hasContentType && http2bodyAllowedForStatus(rws.status) {
			ctype = DetectContentType(p)
		}
		var date string
		if _, ok := rws.snapHeader["Date"]; !ok {

			date = time.Now().UTC().Format(TimeFormat)
		}

		for _, v := range rws.snapHeader["Trailer"] {
			http2foreachHeaderElement(v, rws.declareTrailer)
		}

		endStream := (rws.handlerDone && !rws.hasTrailers() && len(p) == 0) || isHeadResp
		err = rws.conn.writeHeaders(rws.stream, &http2writeResHeaders{
			streamID:      rws.stream.id,
			httpResCode:   rws.status,
			h:             rws.snapHeader,
			endStream:     endStream,
			contentType:   ctype,
			contentLength: clen,
			date:          date,
		})
		if err != nil {
			return 0, err
		}
		if endStream {
			return 0, nil
		}
	}
	if isHeadResp {
		return len(p), nil
	}
	if len(p) == 0 && !rws.handlerDone {
		return 0, nil
	}

	if rws.handlerDone {
		rws.promoteUndeclaredTrailers()
	}

	endStream := rws.handlerDone && !rws.hasTrailers()
	if len(p) > 0 || endStream {

		if err := rws.conn.writeDataFromHandler(rws.stream, p, endStream); err != nil {
			return 0, err
		}
	}

	if rws.handlerDone && rws.hasTrailers() {
		err = rws.conn.writeHeaders(rws.stream, &http2writeResHeaders{
			streamID:  rws.stream.id,
			h:         rws.handlerHeader,
			trailers:  rws.trailers,
			endStream: true,
		})
		return len(p), err
	}
	return len(p), nil
}

// TrailerPrefix is a magic prefix for ResponseWriter.Header map keys
// that, if present, signals that the map entry is actually for
// the response trailers, and not the response headers. The prefix
// is stripped after the ServeHTTP call finishes and the values are
// sent in the trailers.
//
// This mechanism is intended only for trailers that are not known
// prior to the headers being written. If the set of trailers is fixed
// or known before the header is written, the normal Go trailers mechanism
// is preferred:
//    https://golang.org/pkg/net/http/#ResponseWriter
//    https://golang.org/pkg/net/http/#example_ResponseWriter_trailers
const http2TrailerPrefix = "Trailer:"

// promoteUndeclaredTrailers permits http.Handlers to set trailers
// after the header has already been flushed. Because the Go
// ResponseWriter interface has no way to set Trailers (only the
// Header), and because we didn't want to expand the ResponseWriter
// interface, and because nobody used trailers, and because RFC 2616
// says you SHOULD (but not must) predeclare any trailers in the
// header, the official ResponseWriter rules said trailers in Go must
// be predeclared, and then we reuse the same ResponseWriter.Header()
// map to mean both Headers and Trailers.  When it's time to write the
// Trailers, we pick out the fields of Headers that were declared as
// trailers. That worked for a while, until we found the first major
// user of Trailers in the wild: gRPC (using them only over http2),
// and gRPC libraries permit setting trailers mid-stream without
// predeclarnig them. So: change of plans. We still permit the old
// way, but we also permit this hack: if a Header() key begins with
// "Trailer:", the suffix of that key is a Trailer. Because ':' is an
// invalid token byte anyway, there is no ambiguity. (And it's already
// filtered out) It's mildly hacky, but not terrible.
//
// This method runs after the Handler is done and promotes any Header
// fields to be trailers.
func (rws *http2responseWriterState) promoteUndeclaredTrailers() {
	for k, vv := range rws.handlerHeader {
		if !strings.HasPrefix(k, http2TrailerPrefix) {
			continue
		}
		trailerKey := strings.TrimPrefix(k, http2TrailerPrefix)
		rws.declareTrailer(trailerKey)
		rws.handlerHeader[CanonicalHeaderKey(trailerKey)] = vv
	}

	if len(rws.trailers) > 1 {
		sorter := http2sorterPool.Get().(*http2sorter)
		sorter.SortStrings(rws.trailers)
		http2sorterPool.Put(sorter)
	}
}

func (w *http2responseWriter) Flush() {
	rws := w.rws
	if rws == nil {
		panic("Header called after Handler finished")
	}
	if rws.bw.Buffered() > 0 {
		if err := rws.bw.Flush(); err != nil {

			return
		}
	} else {

		rws.writeChunk(nil)
	}
}

func (w *http2responseWriter) CloseNotify() <-chan bool {
	rws := w.rws
	if rws == nil {
		panic("CloseNotify called after Handler finished")
	}
	rws.closeNotifierMu.Lock()
	ch := rws.closeNotifierCh
	if ch == nil {
		ch = make(chan bool, 1)
		rws.closeNotifierCh = ch
		cw := rws.stream.cw
		go func() {
			cw.Wait()
			ch <- true
		}()
	}
	rws.closeNotifierMu.Unlock()
	return ch
}

func (w *http2responseWriter) Header() Header {
	rws := w.rws
	if rws == nil {
		panic("Header called after Handler finished")
	}
	if rws.handlerHeader == nil {
		rws.handlerHeader = make(Header)
	}
	return rws.handlerHeader
}

func (w *http2responseWriter) WriteHeader(code int) {
	rws := w.rws
	if rws == nil {
		panic("WriteHeader called after Handler finished")
	}
	rws.writeHeader(code)
}

func (rws *http2responseWriterState) writeHeader(code int) {
	if !rws.wroteHeader {
		rws.wroteHeader = true
		rws.status = code
		if len(rws.handlerHeader) > 0 {
			rws.snapHeader = http2cloneHeader(rws.handlerHeader)
		}
	}
}

func http2cloneHeader(h Header) Header {
	h2 := make(Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}

// The Life Of A Write is like this:
//
// * Handler calls w.Write or w.WriteString ->
// * -> rws.bw (*bufio.Writer) ->
// * (Handler migth call Flush)
// * -> chunkWriter{rws}
// * -> responseWriterState.writeChunk(p []byte)
// * -> responseWriterState.writeChunk (most of the magic; see comment there)
func (w *http2responseWriter) Write(p []byte) (n int, err error) {
	return w.write(len(p), p, "")
}

func (w *http2responseWriter) WriteString(s string) (n int, err error) {
	return w.write(len(s), nil, s)
}

// either dataB or dataS is non-zero.
func (w *http2responseWriter) write(lenData int, dataB []byte, dataS string) (n int, err error) {
	rws := w.rws
	if rws == nil {
		panic("Write called after Handler finished")
	}
	if !rws.wroteHeader {
		w.WriteHeader(200)
	}
	if !http2bodyAllowedForStatus(rws.status) {
		return 0, ErrBodyNotAllowed
	}
	rws.wroteBytes += int64(len(dataB)) + int64(len(dataS))
	if rws.sentContentLen != 0 && rws.wroteBytes > rws.sentContentLen {

		return 0, errors.New("http2: handler wrote more than declared Content-Length")
	}

	if dataB != nil {
		return rws.bw.Write(dataB)
	} else {
		return rws.bw.WriteString(dataS)
	}
}

func (w *http2responseWriter) handlerDone() {
	rws := w.rws
	rws.handlerDone = true
	w.Flush()
	w.rws = nil
	http2responseWriterStatePool.Put(rws)
}

// Push errors.
var (
	http2ErrRecursivePush    = errors.New("http2: recursive push not allowed")
	http2ErrPushLimitReached = errors.New("http2: push would exceed peer's SETTINGS_MAX_CONCURRENT_STREAMS")
)

// pushOptions is the internal version of http.PushOptions, which we
// cannot include here because it's only defined in Go 1.8 and later.
type http2pushOptions struct {
	Method string
	Header Header
}

func (w *http2responseWriter) push(target string, opts http2pushOptions) error {
	st := w.rws.stream
	sc := st.sc
	sc.serveG.checkNotOn()

	if st.isPushed() {
		return http2ErrRecursivePush
	}

	if opts.Method == "" {
		opts.Method = "GET"
	}
	if opts.Header == nil {
		opts.Header = Header{}
	}
	wantScheme := "http"
	if w.rws.req.TLS != nil {
		wantScheme = "https"
	}

	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	if u.Scheme == "" {
		if !strings.HasPrefix(target, "/") {
			return fmt.Errorf("target must be an absolute URL or an absolute path: %q", target)
		}
		u.Scheme = wantScheme
		u.Host = w.rws.req.Host
	} else {
		if u.Scheme != wantScheme {
			return fmt.Errorf("cannot push URL with scheme %q from request with scheme %q", u.Scheme, wantScheme)
		}
		if u.Host == "" {
			return errors.New("URL must have a host")
		}
	}
	for k := range opts.Header {
		if strings.HasPrefix(k, ":") {
			return fmt.Errorf("promised request headers cannot include pseudo header %q", k)
		}

		switch strings.ToLower(k) {
		case "content-length", "content-encoding", "trailer", "te", "expect", "host":
			return fmt.Errorf("promised request headers cannot include %q", k)
		}
	}
	if err := http2checkValidHTTP2RequestHeaders(opts.Header); err != nil {
		return err
	}

	if opts.Method != "GET" && opts.Method != "HEAD" {
		return fmt.Errorf("method %q must be GET or HEAD", opts.Method)
	}

	msg := http2startPushRequest{
		parent: st,
		method: opts.Method,
		url:    u,
		header: http2cloneHeader(opts.Header),
		done:   http2errChanPool.Get().(chan error),
	}

	select {
	case <-sc.doneServing:
		return http2errClientDisconnected
	case <-st.cw:
		return http2errStreamClosed
	case sc.wantStartPushCh <- msg:
	}

	select {
	case <-sc.doneServing:
		return http2errClientDisconnected
	case <-st.cw:
		return http2errStreamClosed
	case err := <-msg.done:
		http2errChanPool.Put(msg.done)
		return err
	}
}

type http2startPushRequest struct {
	parent *http2stream
	method string
	url    *url.URL
	header Header
	done   chan error
}

func (sc *http2serverConn) startPush(msg http2startPushRequest) {
	sc.serveG.check()

	if msg.parent.state != http2stateOpen && msg.parent.state != http2stateHalfClosedRemote {

		msg.done <- http2errStreamClosed
		return
	}

	if !sc.pushEnabled {
		msg.done <- ErrNotSupported
		return
	}

	allocatePromisedID := func() (uint32, error) {
		sc.serveG.check()

		if !sc.pushEnabled {
			return 0, ErrNotSupported
		}

		if sc.curPushedStreams+1 > sc.clientMaxStreams {
			return 0, http2ErrPushLimitReached
		}

		if sc.maxPushPromiseID+2 >= 1<<31 {
			sc.startGracefulShutdown()
			return 0, http2ErrPushLimitReached
		}
		sc.maxPushPromiseID += 2
		promisedID := sc.maxPushPromiseID

		promised := sc.newStream(promisedID, msg.parent.id, http2stateHalfClosedRemote)
		rw, req, err := sc.newWriterAndRequestNoBody(promised, http2requestParam{
			method:    msg.method,
			scheme:    msg.url.Scheme,
			authority: msg.url.Host,
			path:      msg.url.RequestURI(),
			header:    http2cloneHeader(msg.header),
		})
		if err != nil {

			panic(fmt.Sprintf("newWriterAndRequestNoBody(%+v): %v", msg.url, err))
		}

		go sc.runHandler(rw, req, sc.handler.ServeHTTP)
		return promisedID, nil
	}

	sc.writeFrame(http2FrameWriteRequest{
		write: &http2writePushPromise{
			streamID:           msg.parent.id,
			method:             msg.method,
			url:                msg.url,
			h:                  msg.header,
			allocatePromisedID: allocatePromisedID,
		},
		stream: msg.parent,
		done:   msg.done,
	})
}

// foreachHeaderElement splits v according to the "#rule" construction
// in RFC 2616 section 2.1 and calls fn for each non-empty element.
func http2foreachHeaderElement(v string, fn func(string)) {
	v = textproto.TrimString(v)
	if v == "" {
		return
	}
	if !strings.Contains(v, ",") {
		fn(v)
		return
	}
	for _, f := range strings.Split(v, ",") {
		if f = textproto.TrimString(f); f != "" {
			fn(f)
		}
	}
}

// From http://httpwg.org/specs/rfc7540.html#rfc.section.8.1.2.2
var http2connHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"Transfer-Encoding",
	"Upgrade",
}

// checkValidHTTP2RequestHeaders checks whether h is a valid HTTP/2 request,
// per RFC 7540 Section 8.1.2.2.
// The returned error is reported to users.
func http2checkValidHTTP2RequestHeaders(h Header) error {
	for _, k := range http2connHeaders {
		if _, ok := h[k]; ok {
			return fmt.Errorf("request header %q is not valid in HTTP/2", k)
		}
	}
	te := h["Te"]
	if len(te) > 0 && (len(te) > 1 || (te[0] != "trailers" && te[0] != "")) {
		return errors.New(`request header "TE" may only be "trailers" in HTTP/2`)
	}
	return nil
}

func http2new400Handler(err error) HandlerFunc {
	return func(w ResponseWriter, r *Request) {
		Error(w, err.Error(), StatusBadRequest)
	}
}

// ValidTrailerHeader reports whether name is a valid header field name to appear
// in trailers.
// See: http://tools.ietf.org/html/rfc7230#section-4.1.2
func http2ValidTrailerHeader(name string) bool {
	name = CanonicalHeaderKey(name)
	if strings.HasPrefix(name, "If-") || http2badTrailer[name] {
		return false
	}
	return true
}

var http2badTrailer = map[string]bool{
	"Authorization":       true,
	"Cache-Control":       true,
	"Connection":          true,
	"Content-Encoding":    true,
	"Content-Length":      true,
	"Content-Range":       true,
	"Content-Type":        true,
	"Expect":              true,
	"Host":                true,
	"Keep-Alive":          true,
	"Max-Forwards":        true,
	"Pragma":              true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Range":               true,
	"Realm":               true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Www-Authenticate":    true,
}

// h1ServerShutdownChan returns a channel that will be closed when the
// provided *http.Server wants to shut down.
//
// This is a somewhat hacky way to get at http1 innards. It works
// when the http2 code is bundled into the net/http package in the
// standard library. The alternatives ended up making the cmd/go tool
// depend on http Servers. This is the lightest option for now.
// This is tested via the TestServeShutdown* tests in net/http.
func http2h1ServerShutdownChan(hs *Server) <-chan struct{} {
	if fn := http2testh1ServerShutdownChan; fn != nil {
		return fn(hs)
	}
	var x interface{} = hs
	type I interface {
		getDoneChan() <-chan struct{}
	}
	if hs, ok := x.(I); ok {
		return hs.getDoneChan()
	}
	return nil
}

// optional test hook for h1ServerShutdownChan.
var http2testh1ServerShutdownChan func(hs *Server) <-chan struct{}

// h1ServerKeepAlivesDisabled reports whether hs has its keep-alives
// disabled. See comments on h1ServerShutdownChan above for why
// the code is written this way.
func http2h1ServerKeepAlivesDisabled(hs *Server) bool {
	var x interface{} = hs
	type I interface {
		doKeepAlives() bool
	}
	if hs, ok := x.(I); ok {
		return !hs.doKeepAlives()
	}
	return false
}

const (
	// transportDefaultConnFlow is how many connection-level flow control
	// tokens we give the server at start-up, past the default 64k.
	http2transportDefaultConnFlow = 1 << 30

	// transportDefaultStreamFlow is how many stream-level flow
	// control tokens we announce to the peer, and how many bytes
	// we buffer per stream.
	http2transportDefaultStreamFlow = 4 << 20

	// transportDefaultStreamMinRefresh is the minimum number of bytes we'll send
	// a stream-level WINDOW_UPDATE for at a time.
	http2transportDefaultStreamMinRefresh = 4 << 10

	http2defaultUserAgent = "Go-http-client/2.0"
)

// Transport is an HTTP/2 Transport.
//
// A Transport internally caches connections to servers. It is safe
// for concurrent use by multiple goroutines.
type http2Transport struct {
	// DialTLS specifies an optional dial function for creating
	// TLS connections for requests.
	//
	// If DialTLS is nil, tls.Dial is used.
	//
	// If the returned net.Conn has a ConnectionState method like tls.Conn,
	// it will be used to set http.Response.TLS.
	DialTLS func(network, addr string, cfg *tls.Config) (net.Conn, error)

	// TLSClientConfig specifies the TLS configuration to use with
	// tls.Client. If nil, the default configuration is used.
	TLSClientConfig *tls.Config

	// ConnPool optionally specifies an alternate connection pool to use.
	// If nil, the default is used.
	ConnPool http2ClientConnPool

	// DisableCompression, if true, prevents the Transport from
	// requesting compression with an "Accept-Encoding: gzip"
	// request header when the Request contains no existing
	// Accept-Encoding value. If the Transport requests gzip on
	// its own and gets a gzipped response, it's transparently
	// decoded in the Response.Body. However, if the user
	// explicitly requested gzip it is not automatically
	// uncompressed.
	DisableCompression bool

	// AllowHTTP, if true, permits HTTP/2 requests using the insecure,
	// plain-text "http" scheme. Note that this does not enable h2c support.
	AllowHTTP bool

	// MaxHeaderListSize is the http2 SETTINGS_MAX_HEADER_LIST_SIZE to
	// send in the initial settings frame. It is how many bytes
	// of response headers are allow. Unlike the http2 spec, zero here
	// means to use a default limit (currently 10MB). If you actually
	// want to advertise an ulimited value to the peer, Transport
	// interprets the highest possible value here (0xffffffff or 1<<32-1)
	// to mean no limit.
	MaxHeaderListSize uint32

	// t1, if non-nil, is the standard library Transport using
	// this transport. Its settings are used (but not its
	// RoundTrip method, etc).
	t1 *Transport

	connPoolOnce  sync.Once
	connPoolOrDef http2ClientConnPool // non-nil version of ConnPool
}

func (t *http2Transport) maxHeaderListSize() uint32 {
	if t.MaxHeaderListSize == 0 {
		return 10 << 20
	}
	if t.MaxHeaderListSize == 0xffffffff {
		return 0
	}
	return t.MaxHeaderListSize
}

func (t *http2Transport) disableCompression() bool {
	return t.DisableCompression || (t.t1 != nil && t.t1.DisableCompression)
}

var http2errTransportVersion = errors.New("http2: ConfigureTransport is only supported starting at Go 1.6")

// ConfigureTransport configures a net/http HTTP/1 Transport to use HTTP/2.
// It requires Go 1.6 or later and returns an error if the net/http package is too old
// or if t1 has already been HTTP/2-enabled.
func http2ConfigureTransport(t1 *Transport) error {
	_, err := http2configureTransport(t1)
	return err
}

func (t *http2Transport) connPool() http2ClientConnPool {
	t.connPoolOnce.Do(t.initConnPool)
	return t.connPoolOrDef
}

func (t *http2Transport) initConnPool() {
	if t.ConnPool != nil {
		t.connPoolOrDef = t.ConnPool
	} else {
		t.connPoolOrDef = &http2clientConnPool{t: t}
	}
}

// ClientConn is the state of a single HTTP/2 client connection to an
// HTTP/2 server.
type http2ClientConn struct {
	t         *http2Transport
	tconn     net.Conn             // usually *tls.Conn, except specialized impls
	tlsState  *tls.ConnectionState // nil only for specialized impls
	singleUse bool                 // whether being used for a single http.Request

	// readLoop goroutine fields:
	readerDone chan struct{} // closed on error
	readerErr  error         // set before readerDone is closed

	idleTimeout time.Duration // or 0 for never
	idleTimer   *time.Timer

	mu              sync.Mutex // guards following
	cond            *sync.Cond // hold mu; broadcast on flow/closed changes
	flow            http2flow  // our conn-level flow control quota (cs.flow is per stream)
	inflow          http2flow  // peer's conn-level flow control
	closed          bool
	wantSettingsAck bool                          // we sent a SETTINGS frame and haven't heard back
	goAway          *http2GoAwayFrame             // if non-nil, the GoAwayFrame we received
	goAwayDebug     string                        // goAway frame's debug data, retained as a string
	streams         map[uint32]*http2clientStream // client-initiated
	nextStreamID    uint32
	pings           map[[8]byte]chan struct{} // in flight ping data to notification channel
	bw              *bufio.Writer
	br              *bufio.Reader
	fr              *http2Framer
	lastActive      time.Time
	// Settings from peer: (also guarded by mu)
	maxFrameSize         uint32
	maxConcurrentStreams uint32
	initialWindowSize    uint32

	hbuf    bytes.Buffer // HPACK encoder writes into this
	henc    *hpack.Encoder
	freeBuf [][]byte

	wmu  sync.Mutex // held while writing; acquire AFTER mu if holding both
	werr error      // first write error that has occurred
}

// clientStream is the state for a single HTTP/2 stream. One of these
// is created for each Transport.RoundTrip call.
type http2clientStream struct {
	cc            *http2ClientConn
	req           *Request
	trace         *http2clientTrace // or nil
	ID            uint32
	resc          chan http2resAndError
	bufPipe       http2pipe // buffered pipe with the flow-controlled response payload
	startedWrite  bool      // started request body write; guarded by cc.mu
	requestedGzip bool
	on100         func() // optional code to run if get a 100 continue response

	flow        http2flow // guarded by cc.mu
	inflow      http2flow // guarded by cc.mu
	bytesRemain int64     // -1 means unknown; owned by transportResponseBody.Read
	readErr     error     // sticky read error; owned by transportResponseBody.Read
	stopReqBody error     // if non-nil, stop writing req body; guarded by cc.mu
	didReset    bool      // whether we sent a RST_STREAM to the server; guarded by cc.mu

	peerReset chan struct{} // closed on peer reset
	resetErr  error         // populated before peerReset is closed

	done chan struct{} // closed when stream remove from cc.streams map; close calls guarded by cc.mu

	// owned by clientConnReadLoop:
	firstByte    bool // got the first response byte
	pastHeaders  bool // got first MetaHeadersFrame (actual headers)
	pastTrailers bool // got optional second MetaHeadersFrame (trailers)

	trailer    Header  // accumulated trailers
	resTrailer *Header // client's Response.Trailer
}

// awaitRequestCancel runs in its own goroutine and waits for the user
// to cancel a RoundTrip request, its context to expire, or for the
// request to be done (any way it might be removed from the cc.streams
// map: peer reset, successful completion, TCP connection breakage,
// etc)
func (cs *http2clientStream) awaitRequestCancel(req *Request) {
	ctx := http2reqContext(req)
	if req.Cancel == nil && ctx.Done() == nil {
		return
	}
	select {
	case <-req.Cancel:
		cs.cancelStream()
		cs.bufPipe.CloseWithError(http2errRequestCanceled)
	case <-ctx.Done():
		cs.cancelStream()
		cs.bufPipe.CloseWithError(ctx.Err())
	case <-cs.done:
	}
}

func (cs *http2clientStream) cancelStream() {
	cs.cc.mu.Lock()
	didReset := cs.didReset
	cs.didReset = true
	cs.cc.mu.Unlock()

	if !didReset {
		cs.cc.writeStreamReset(cs.ID, http2ErrCodeCancel, nil)
	}
}

// checkResetOrDone reports any error sent in a RST_STREAM frame by the
// server, or errStreamClosed if the stream is complete.
func (cs *http2clientStream) checkResetOrDone() error {
	select {
	case <-cs.peerReset:
		return cs.resetErr
	case <-cs.done:
		return http2errStreamClosed
	default:
		return nil
	}
}

func (cs *http2clientStream) abortRequestBodyWrite(err error) {
	if err == nil {
		panic("nil error")
	}
	cc := cs.cc
	cc.mu.Lock()
	cs.stopReqBody = err
	cc.cond.Broadcast()
	cc.mu.Unlock()
}

type http2stickyErrWriter struct {
	w   io.Writer
	err *error
}

func (sew http2stickyErrWriter) Write(p []byte) (n int, err error) {
	if *sew.err != nil {
		return 0, *sew.err
	}
	n, err = sew.w.Write(p)
	*sew.err = err
	return
}

var http2ErrNoCachedConn = errors.New("http2: no cached connection was available")

// RoundTripOpt are options for the Transport.RoundTripOpt method.
type http2RoundTripOpt struct {
	// OnlyCachedConn controls whether RoundTripOpt may
	// create a new TCP connection. If set true and
	// no cached connection is available, RoundTripOpt
	// will return ErrNoCachedConn.
	OnlyCachedConn bool
}

func (t *http2Transport) RoundTrip(req *Request) (*Response, error) {
	return t.RoundTripOpt(req, http2RoundTripOpt{})
}

// authorityAddr returns a given authority (a host/IP, or host:port / ip:port)
// and returns a host:port. The port 443 is added if needed.
func http2authorityAddr(scheme string, authority string) (addr string) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		port = "443"
		if scheme == "http" {
			port = "80"
		}
		host = authority
	}
	if a, err := idna.ToASCII(host); err == nil {
		host = a
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host + ":" + port
	}
	return net.JoinHostPort(host, port)
}

// RoundTripOpt is like RoundTrip, but takes options.
func (t *http2Transport) RoundTripOpt(req *Request, opt http2RoundTripOpt) (*Response, error) {
	if !(req.URL.Scheme == "https" || (req.URL.Scheme == "http" && t.AllowHTTP)) {
		return nil, errors.New("http2: unsupported scheme")
	}

	addr := http2authorityAddr(req.URL.Scheme, req.URL.Host)
	for {
		cc, err := t.connPool().GetClientConn(req, addr)
		if err != nil {
			t.vlogf("http2: Transport failed to get client conn for %s: %v", addr, err)
			return nil, err
		}
		http2traceGotConn(req, cc)
		res, err := cc.RoundTrip(req)
		if err != nil {
			if req, err = http2shouldRetryRequest(req, err); err == nil {
				continue
			}
		}
		if err != nil {
			t.vlogf("RoundTrip failure: %v", err)
			return nil, err
		}
		return res, nil
	}
}

// CloseIdleConnections closes any connections which were previously
// connected from previous requests but are now sitting idle.
// It does not interrupt any connections currently in use.
func (t *http2Transport) CloseIdleConnections() {
	if cp, ok := t.connPool().(http2clientConnPoolIdleCloser); ok {
		cp.closeIdleConnections()
	}
}

var (
	http2errClientConnClosed   = errors.New("http2: client conn is closed")
	http2errClientConnUnusable = errors.New("http2: client conn not usable")

	http2errClientConnGotGoAway                 = errors.New("http2: Transport received Server's graceful shutdown GOAWAY")
	http2errClientConnGotGoAwayAfterSomeReqBody = errors.New("http2: Transport received Server's graceful shutdown GOAWAY; some request body already written")
)

// shouldRetryRequest is called by RoundTrip when a request fails to get
// response headers. It is always called with a non-nil error.
// It returns either a request to retry (either the same request, or a
// modified clone), or an error if the request can't be replayed.
func http2shouldRetryRequest(req *Request, err error) (*Request, error) {
	switch err {
	default:
		return nil, err
	case http2errClientConnUnusable, http2errClientConnGotGoAway:
		return req, nil
	case http2errClientConnGotGoAwayAfterSomeReqBody:

		if req.Body == nil || http2reqBodyIsNoBody(req.Body) {
			return req, nil
		}

		getBody := http2reqGetBody(req)
		if getBody == nil {
			return nil, errors.New("http2: Transport: peer server initiated graceful shutdown after some of Request.Body was written; define Request.GetBody to avoid this error")
		}
		body, err := getBody()
		if err != nil {
			return nil, err
		}
		newReq := *req
		newReq.Body = body
		return &newReq, nil
	}
}

func (t *http2Transport) dialClientConn(addr string, singleUse bool) (*http2ClientConn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	tconn, err := t.dialTLS()("tcp", addr, t.newTLSConfig(host))
	if err != nil {
		return nil, err
	}
	return t.newClientConn(tconn, singleUse)
}

func (t *http2Transport) newTLSConfig(host string) *tls.Config {
	cfg := new(tls.Config)
	if t.TLSClientConfig != nil {
		*cfg = *http2cloneTLSConfig(t.TLSClientConfig)
	}
	if !http2strSliceContains(cfg.NextProtos, http2NextProtoTLS) {
		cfg.NextProtos = append([]string{http2NextProtoTLS}, cfg.NextProtos...)
	}
	if cfg.ServerName == "" {
		cfg.ServerName = host
	}
	return cfg
}

func (t *http2Transport) dialTLS() func(string, string, *tls.Config) (net.Conn, error) {
	if t.DialTLS != nil {
		return t.DialTLS
	}
	return t.dialTLSDefault
}

func (t *http2Transport) dialTLSDefault(network, addr string, cfg *tls.Config) (net.Conn, error) {
	cn, err := tls.Dial(network, addr, cfg)
	if err != nil {
		return nil, err
	}
	if err := cn.Handshake(); err != nil {
		return nil, err
	}
	if !cfg.InsecureSkipVerify {
		if err := cn.VerifyHostname(cfg.ServerName); err != nil {
			return nil, err
		}
	}
	state := cn.ConnectionState()
	if p := state.NegotiatedProtocol; p != http2NextProtoTLS {
		return nil, fmt.Errorf("http2: unexpected ALPN protocol %q; want %q", p, http2NextProtoTLS)
	}
	if !state.NegotiatedProtocolIsMutual {
		return nil, errors.New("http2: could not negotiate protocol mutually")
	}
	return cn, nil
}

// disableKeepAlives reports whether connections should be closed as
// soon as possible after handling the first request.
func (t *http2Transport) disableKeepAlives() bool {
	return t.t1 != nil && t.t1.DisableKeepAlives
}

func (t *http2Transport) expectContinueTimeout() time.Duration {
	if t.t1 == nil {
		return 0
	}
	return http2transportExpectContinueTimeout(t.t1)
}

func (t *http2Transport) NewClientConn(c net.Conn) (*http2ClientConn, error) {
	return t.newClientConn(c, false)
}

func (t *http2Transport) newClientConn(c net.Conn, singleUse bool) (*http2ClientConn, error) {
	cc := &http2ClientConn{
		t:                    t,
		tconn:                c,
		readerDone:           make(chan struct{}),
		nextStreamID:         1,
		maxFrameSize:         16 << 10,
		initialWindowSize:    65535,
		maxConcurrentStreams: 1000,
		streams:              make(map[uint32]*http2clientStream),
		singleUse:            singleUse,
		wantSettingsAck:      true,
		pings:                make(map[[8]byte]chan struct{}),
	}
	if d := t.idleConnTimeout(); d != 0 {
		cc.idleTimeout = d
		cc.idleTimer = time.AfterFunc(d, cc.onIdleTimeout)
	}
	if http2VerboseLogs {
		t.vlogf("http2: Transport creating client conn %p to %v", cc, c.RemoteAddr())
	}

	cc.cond = sync.NewCond(&cc.mu)
	cc.flow.add(int32(http2initialWindowSize))

	cc.bw = bufio.NewWriter(http2stickyErrWriter{c, &cc.werr})
	cc.br = bufio.NewReader(c)
	cc.fr = http2NewFramer(cc.bw, cc.br)
	cc.fr.ReadMetaHeaders = hpack.NewDecoder(http2initialHeaderTableSize, nil)
	cc.fr.MaxHeaderListSize = t.maxHeaderListSize()

	cc.henc = hpack.NewEncoder(&cc.hbuf)

	if cs, ok := c.(http2connectionStater); ok {
		state := cs.ConnectionState()
		cc.tlsState = &state
	}

	initialSettings := []http2Setting{
		{ID: http2SettingEnablePush, Val: 0},
		{ID: http2SettingInitialWindowSize, Val: http2transportDefaultStreamFlow},
	}
	if max := t.maxHeaderListSize(); max != 0 {
		initialSettings = append(initialSettings, http2Setting{ID: http2SettingMaxHeaderListSize, Val: max})
	}

	cc.bw.Write(http2clientPreface)
	cc.fr.WriteSettings(initialSettings...)
	cc.fr.WriteWindowUpdate(0, http2transportDefaultConnFlow)
	cc.inflow.add(http2transportDefaultConnFlow + http2initialWindowSize)
	cc.bw.Flush()
	if cc.werr != nil {
		return nil, cc.werr
	}

	go cc.readLoop()
	return cc, nil
}

func (cc *http2ClientConn) setGoAway(f *http2GoAwayFrame) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	old := cc.goAway
	cc.goAway = f

	if cc.goAwayDebug == "" {
		cc.goAwayDebug = string(f.DebugData())
	}
	if old != nil && old.ErrCode != http2ErrCodeNo {
		cc.goAway.ErrCode = old.ErrCode
	}
	last := f.LastStreamID
	for streamID, cs := range cc.streams {
		if streamID > last {
			select {
			case cs.resc <- http2resAndError{err: http2errClientConnGotGoAway}:
			default:
			}
		}
	}
}

func (cc *http2ClientConn) CanTakeNewRequest() bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.canTakeNewRequestLocked()
}

func (cc *http2ClientConn) canTakeNewRequestLocked() bool {
	if cc.singleUse && cc.nextStreamID > 1 {
		return false
	}
	return cc.goAway == nil && !cc.closed &&
		int64(len(cc.streams)+1) < int64(cc.maxConcurrentStreams) &&
		cc.nextStreamID < math.MaxInt32
}

// onIdleTimeout is called from a time.AfterFunc goroutine.  It will
// only be called when we're idle, but because we're coming from a new
// goroutine, there could be a new request coming in at the same time,
// so this simply calls the synchronized closeIfIdle to shut down this
// connection. The timer could just call closeIfIdle, but this is more
// clear.
func (cc *http2ClientConn) onIdleTimeout() {
	cc.closeIfIdle()
}

func (cc *http2ClientConn) closeIfIdle() {
	cc.mu.Lock()
	if len(cc.streams) > 0 {
		cc.mu.Unlock()
		return
	}
	cc.closed = true
	nextID := cc.nextStreamID

	cc.mu.Unlock()

	if http2VerboseLogs {
		cc.vlogf("http2: Transport closing idle conn %p (forSingleUse=%v, maxStream=%v)", cc, cc.singleUse, nextID-2)
	}
	cc.tconn.Close()
}

const http2maxAllocFrameSize = 512 << 10

// frameBuffer returns a scratch buffer suitable for writing DATA frames.
// They're capped at the min of the peer's max frame size or 512KB
// (kinda arbitrarily), but definitely capped so we don't allocate 4GB
// bufers.
func (cc *http2ClientConn) frameScratchBuffer() []byte {
	cc.mu.Lock()
	size := cc.maxFrameSize
	if size > http2maxAllocFrameSize {
		size = http2maxAllocFrameSize
	}
	for i, buf := range cc.freeBuf {
		if len(buf) >= int(size) {
			cc.freeBuf[i] = nil
			cc.mu.Unlock()
			return buf[:size]
		}
	}
	cc.mu.Unlock()
	return make([]byte, size)
}

func (cc *http2ClientConn) putFrameScratchBuffer(buf []byte) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	const maxBufs = 4 // arbitrary; 4 concurrent requests per conn? investigate.
	if len(cc.freeBuf) < maxBufs {
		cc.freeBuf = append(cc.freeBuf, buf)
		return
	}
	for i, old := range cc.freeBuf {
		if old == nil {
			cc.freeBuf[i] = buf
			return
		}
	}

}

// errRequestCanceled is a copy of net/http's errRequestCanceled because it's not
// exported. At least they'll be DeepEqual for h1-vs-h2 comparisons tests.
var http2errRequestCanceled = errors.New("net/http: request canceled")

func http2commaSeparatedTrailers(req *Request) (string, error) {
	keys := make([]string, 0, len(req.Trailer))
	for k := range req.Trailer {
		k = CanonicalHeaderKey(k)
		switch k {
		case "Transfer-Encoding", "Trailer", "Content-Length":
			return "", &http2badStringError{"invalid Trailer key", k}
		}
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return strings.Join(keys, ","), nil
	}
	return "", nil
}

func (cc *http2ClientConn) responseHeaderTimeout() time.Duration {
	if cc.t.t1 != nil {
		return cc.t.t1.ResponseHeaderTimeout
	}

	return 0
}

// checkConnHeaders checks whether req has any invalid connection-level headers.
// per RFC 7540 section 8.1.2.2: Connection-Specific Header Fields.
// Certain headers are special-cased as okay but not transmitted later.
func http2checkConnHeaders(req *Request) error {
	if v := req.Header.Get("Upgrade"); v != "" {
		return fmt.Errorf("http2: invalid Upgrade request header: %q", req.Header["Upgrade"])
	}
	if vv := req.Header["Transfer-Encoding"]; len(vv) > 0 && (len(vv) > 1 || vv[0] != "" && vv[0] != "chunked") {
		return fmt.Errorf("http2: invalid Transfer-Encoding request header: %q", vv)
	}
	if vv := req.Header["Connection"]; len(vv) > 0 && (len(vv) > 1 || vv[0] != "" && vv[0] != "close" && vv[0] != "keep-alive") {
		return fmt.Errorf("http2: invalid Connection request header: %q", vv)
	}
	return nil
}

// actualContentLength returns a sanitized version of
// req.ContentLength, where 0 actually means zero (not unknown) and -1
// means unknown.
func http2actualContentLength(req *Request) int64 {
	if req.Body == nil {
		return 0
	}
	if req.ContentLength != 0 {
		return req.ContentLength
	}
	return -1
}

func (cc *http2ClientConn) RoundTrip(req *Request) (*Response, error) {
	if err := http2checkConnHeaders(req); err != nil {
		return nil, err
	}
	if cc.idleTimer != nil {
		cc.idleTimer.Stop()
	}

	trailers, err := http2commaSeparatedTrailers(req)
	if err != nil {
		return nil, err
	}
	hasTrailers := trailers != ""

	cc.mu.Lock()
	cc.lastActive = time.Now()
	if cc.closed || !cc.canTakeNewRequestLocked() {
		cc.mu.Unlock()
		return nil, http2errClientConnUnusable
	}

	body := req.Body
	hasBody := body != nil
	contentLen := http2actualContentLength(req)

	// TODO(bradfitz): this is a copy of the logic in net/http. Unify somewhere?
	var requestedGzip bool
	if !cc.t.disableCompression() &&
		req.Header.Get("Accept-Encoding") == "" &&
		req.Header.Get("Range") == "" &&
		req.Method != "HEAD" {

		requestedGzip = true
	}

	hdrs, err := cc.encodeHeaders(req, requestedGzip, trailers, contentLen)
	if err != nil {
		cc.mu.Unlock()
		return nil, err
	}

	cs := cc.newStream()
	cs.req = req
	cs.trace = http2requestTrace(req)
	cs.requestedGzip = requestedGzip
	bodyWriter := cc.t.getBodyWriterState(cs, body)
	cs.on100 = bodyWriter.on100

	cc.wmu.Lock()
	endStream := !hasBody && !hasTrailers
	werr := cc.writeHeaders(cs.ID, endStream, hdrs)
	cc.wmu.Unlock()
	http2traceWroteHeaders(cs.trace)
	cc.mu.Unlock()

	if werr != nil {
		if hasBody {
			req.Body.Close()
			bodyWriter.cancel()
		}
		cc.forgetStreamID(cs.ID)

		http2traceWroteRequest(cs.trace, werr)
		return nil, werr
	}

	var respHeaderTimer <-chan time.Time
	if hasBody {
		bodyWriter.scheduleBodyWrite()
	} else {
		http2traceWroteRequest(cs.trace, nil)
		if d := cc.responseHeaderTimeout(); d != 0 {
			timer := time.NewTimer(d)
			defer timer.Stop()
			respHeaderTimer = timer.C
		}
	}

	readLoopResCh := cs.resc
	bodyWritten := false
	ctx := http2reqContext(req)

	handleReadLoopResponse := func(re http2resAndError) (*Response, error) {
		res := re.res
		if re.err != nil || res.StatusCode > 299 {

			bodyWriter.cancel()
			cs.abortRequestBodyWrite(http2errStopReqBodyWrite)
		}
		if re.err != nil {
			if re.err == http2errClientConnGotGoAway {
				cc.mu.Lock()
				if cs.startedWrite {
					re.err = http2errClientConnGotGoAwayAfterSomeReqBody
				}
				cc.mu.Unlock()
			}
			cc.forgetStreamID(cs.ID)
			return nil, re.err
		}
		res.Request = req
		res.TLS = cc.tlsState
		return res, nil
	}

	for {
		select {
		case re := <-readLoopResCh:
			return handleReadLoopResponse(re)
		case <-respHeaderTimer:
			cc.forgetStreamID(cs.ID)
			if !hasBody || bodyWritten {
				cc.writeStreamReset(cs.ID, http2ErrCodeCancel, nil)
			} else {
				bodyWriter.cancel()
				cs.abortRequestBodyWrite(http2errStopReqBodyWriteAndCancel)
			}
			return nil, http2errTimeout
		case <-ctx.Done():
			cc.forgetStreamID(cs.ID)
			if !hasBody || bodyWritten {
				cc.writeStreamReset(cs.ID, http2ErrCodeCancel, nil)
			} else {
				bodyWriter.cancel()
				cs.abortRequestBodyWrite(http2errStopReqBodyWriteAndCancel)
			}
			return nil, ctx.Err()
		case <-req.Cancel:
			cc.forgetStreamID(cs.ID)
			if !hasBody || bodyWritten {
				cc.writeStreamReset(cs.ID, http2ErrCodeCancel, nil)
			} else {
				bodyWriter.cancel()
				cs.abortRequestBodyWrite(http2errStopReqBodyWriteAndCancel)
			}
			return nil, http2errRequestCanceled
		case <-cs.peerReset:

			return nil, cs.resetErr
		case err := <-bodyWriter.resc:

			select {
			case re := <-readLoopResCh:
				return handleReadLoopResponse(re)
			default:
			}
			if err != nil {
				return nil, err
			}
			bodyWritten = true
			if d := cc.responseHeaderTimeout(); d != 0 {
				timer := time.NewTimer(d)
				defer timer.Stop()
				respHeaderTimer = timer.C
			}
		}
	}
}

// requires cc.wmu be held
func (cc *http2ClientConn) writeHeaders(streamID uint32, endStream bool, hdrs []byte) error {
	first := true
	frameSize := int(cc.maxFrameSize)
	for len(hdrs) > 0 && cc.werr == nil {
		chunk := hdrs
		if len(chunk) > frameSize {
			chunk = chunk[:frameSize]
		}
		hdrs = hdrs[len(chunk):]
		endHeaders := len(hdrs) == 0
		if first {
			cc.fr.WriteHeaders(http2HeadersFrameParam{
				StreamID:      streamID,
				BlockFragment: chunk,
				EndStream:     endStream,
				EndHeaders:    endHeaders,
			})
			first = false
		} else {
			cc.fr.WriteContinuation(streamID, endHeaders, chunk)
		}
	}

	cc.bw.Flush()
	return cc.werr
}

// internal error values; they don't escape to callers
var (
	// abort request body write; don't send cancel
	http2errStopReqBodyWrite = errors.New("http2: aborting request body write")

	// abort request body write, but send stream reset of cancel.
	http2errStopReqBodyWriteAndCancel = errors.New("http2: canceling request")
)

func (cs *http2clientStream) writeRequestBody(body io.Reader, bodyCloser io.Closer) (err error) {
	cc := cs.cc
	sentEnd := false
	buf := cc.frameScratchBuffer()
	defer cc.putFrameScratchBuffer(buf)

	defer func() {
		http2traceWroteRequest(cs.trace, err)

		cerr := bodyCloser.Close()
		if err == nil {
			err = cerr
		}
	}()

	req := cs.req
	hasTrailers := req.Trailer != nil

	var sawEOF bool
	for !sawEOF {
		n, err := body.Read(buf)
		if err == io.EOF {
			sawEOF = true
			err = nil
		} else if err != nil {
			return err
		}

		remain := buf[:n]
		for len(remain) > 0 && err == nil {
			var allowed int32
			allowed, err = cs.awaitFlowControl(len(remain))
			switch {
			case err == http2errStopReqBodyWrite:
				return err
			case err == http2errStopReqBodyWriteAndCancel:
				cc.writeStreamReset(cs.ID, http2ErrCodeCancel, nil)
				return err
			case err != nil:
				return err
			}
			cc.wmu.Lock()
			data := remain[:allowed]
			remain = remain[allowed:]
			sentEnd = sawEOF && len(remain) == 0 && !hasTrailers
			err = cc.fr.WriteData(cs.ID, sentEnd, data)
			if err == nil {

				err = cc.bw.Flush()
			}
			cc.wmu.Unlock()
		}
		if err != nil {
			return err
		}
	}

	if sentEnd {

		return nil
	}

	var trls []byte
	if hasTrailers {
		cc.mu.Lock()
		defer cc.mu.Unlock()
		trls = cc.encodeTrailers(req)
	}

	cc.wmu.Lock()
	defer cc.wmu.Unlock()

	if len(trls) > 0 {
		err = cc.writeHeaders(cs.ID, true, trls)
	} else {
		err = cc.fr.WriteData(cs.ID, true, nil)
	}
	if ferr := cc.bw.Flush(); ferr != nil && err == nil {
		err = ferr
	}
	return err
}

// awaitFlowControl waits for [1, min(maxBytes, cc.cs.maxFrameSize)] flow
// control tokens from the server.
// It returns either the non-zero number of tokens taken or an error
// if the stream is dead.
func (cs *http2clientStream) awaitFlowControl(maxBytes int) (taken int32, err error) {
	cc := cs.cc
	cc.mu.Lock()
	defer cc.mu.Unlock()
	for {
		if cc.closed {
			return 0, http2errClientConnClosed
		}
		if cs.stopReqBody != nil {
			return 0, cs.stopReqBody
		}
		if err := cs.checkResetOrDone(); err != nil {
			return 0, err
		}
		if a := cs.flow.available(); a > 0 {
			take := a
			if int(take) > maxBytes {

				take = int32(maxBytes)
			}
			if take > int32(cc.maxFrameSize) {
				take = int32(cc.maxFrameSize)
			}
			cs.flow.take(take)
			return take, nil
		}
		cc.cond.Wait()
	}
}

type http2badStringError struct {
	what string
	str  string
}

func (e *http2badStringError) Error() string { return fmt.Sprintf("%s %q", e.what, e.str) }

// requires cc.mu be held.
func (cc *http2ClientConn) encodeHeaders(req *Request, addGzipHeader bool, trailers string, contentLength int64) ([]byte, error) {
	cc.hbuf.Reset()

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	host, err := httplex.PunycodeHostPort(host)
	if err != nil {
		return nil, err
	}

	var path string
	if req.Method != "CONNECT" {
		path = req.URL.RequestURI()
		if !http2validPseudoPath(path) {
			orig := path
			path = strings.TrimPrefix(path, req.URL.Scheme+"://"+host)
			if !http2validPseudoPath(path) {
				if req.URL.Opaque != "" {
					return nil, fmt.Errorf("invalid request :path %q from URL.Opaque = %q", orig, req.URL.Opaque)
				} else {
					return nil, fmt.Errorf("invalid request :path %q", orig)
				}
			}
		}
	}

	for k, vv := range req.Header {
		if !httplex.ValidHeaderFieldName(k) {
			return nil, fmt.Errorf("invalid HTTP header name %q", k)
		}
		for _, v := range vv {
			if !httplex.ValidHeaderFieldValue(v) {
				return nil, fmt.Errorf("invalid HTTP header value %q for header %q", v, k)
			}
		}
	}

	cc.writeHeader(":authority", host)
	cc.writeHeader(":method", req.Method)
	if req.Method != "CONNECT" {
		cc.writeHeader(":path", path)
		cc.writeHeader(":scheme", req.URL.Scheme)
	}
	if trailers != "" {
		cc.writeHeader("trailer", trailers)
	}

	var didUA bool
	for k, vv := range req.Header {
		lowKey := strings.ToLower(k)
		switch lowKey {
		case "host", "content-length":

			continue
		case "connection", "proxy-connection", "transfer-encoding", "upgrade", "keep-alive":

			continue
		case "user-agent":

			didUA = true
			if len(vv) < 1 {
				continue
			}
			vv = vv[:1]
			if vv[0] == "" {
				continue
			}
		}
		for _, v := range vv {
			cc.writeHeader(lowKey, v)
		}
	}
	if http2shouldSendReqContentLength(req.Method, contentLength) {
		cc.writeHeader("content-length", strconv.FormatInt(contentLength, 10))
	}
	if addGzipHeader {
		cc.writeHeader("accept-encoding", "gzip")
	}
	if !didUA {
		cc.writeHeader("user-agent", http2defaultUserAgent)
	}
	return cc.hbuf.Bytes(), nil
}

// shouldSendReqContentLength reports whether the http2.Transport should send
// a "content-length" request header. This logic is basically a copy of the net/http
// transferWriter.shouldSendContentLength.
// The contentLength is the corrected contentLength (so 0 means actually 0, not unknown).
// -1 means unknown.
func http2shouldSendReqContentLength(method string, contentLength int64) bool {
	if contentLength > 0 {
		return true
	}
	if contentLength < 0 {
		return false
	}

	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// requires cc.mu be held.
func (cc *http2ClientConn) encodeTrailers(req *Request) []byte {
	cc.hbuf.Reset()
	for k, vv := range req.Trailer {

		lowKey := strings.ToLower(k)
		for _, v := range vv {
			cc.writeHeader(lowKey, v)
		}
	}
	return cc.hbuf.Bytes()
}

func (cc *http2ClientConn) writeHeader(name, value string) {
	if http2VerboseLogs {
		log.Printf("http2: Transport encoding header %q = %q", name, value)
	}
	cc.henc.WriteField(hpack.HeaderField{Name: name, Value: value})
}

type http2resAndError struct {
	res *Response
	err error
}

// requires cc.mu be held.
func (cc *http2ClientConn) newStream() *http2clientStream {
	cs := &http2clientStream{
		cc:        cc,
		ID:        cc.nextStreamID,
		resc:      make(chan http2resAndError, 1),
		peerReset: make(chan struct{}),
		done:      make(chan struct{}),
	}
	cs.flow.add(int32(cc.initialWindowSize))
	cs.flow.setConnFlow(&cc.flow)
	cs.inflow.add(http2transportDefaultStreamFlow)
	cs.inflow.setConnFlow(&cc.inflow)
	cc.nextStreamID += 2
	cc.streams[cs.ID] = cs
	return cs
}

func (cc *http2ClientConn) forgetStreamID(id uint32) {
	cc.streamByID(id, true)
}

func (cc *http2ClientConn) streamByID(id uint32, andRemove bool) *http2clientStream {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cs := cc.streams[id]
	if andRemove && cs != nil && !cc.closed {
		cc.lastActive = time.Now()
		delete(cc.streams, id)
		if len(cc.streams) == 0 && cc.idleTimer != nil {
			cc.idleTimer.Reset(cc.idleTimeout)
		}
		close(cs.done)
		cc.cond.Broadcast()
	}
	return cs
}

// clientConnReadLoop is the state owned by the clientConn's frame-reading readLoop.
type http2clientConnReadLoop struct {
	cc            *http2ClientConn
	activeRes     map[uint32]*http2clientStream // keyed by streamID
	closeWhenIdle bool
}

// readLoop runs in its own goroutine and reads and dispatches frames.
func (cc *http2ClientConn) readLoop() {
	rl := &http2clientConnReadLoop{
		cc:        cc,
		activeRes: make(map[uint32]*http2clientStream),
	}

	defer rl.cleanup()
	cc.readerErr = rl.run()
	if ce, ok := cc.readerErr.(http2ConnectionError); ok {
		cc.wmu.Lock()
		cc.fr.WriteGoAway(0, http2ErrCode(ce), nil)
		cc.wmu.Unlock()
	}
}

// GoAwayError is returned by the Transport when the server closes the
// TCP connection after sending a GOAWAY frame.
type http2GoAwayError struct {
	LastStreamID uint32
	ErrCode      http2ErrCode
	DebugData    string
}

func (e http2GoAwayError) Error() string {
	return fmt.Sprintf("http2: server sent GOAWAY and closed the connection; LastStreamID=%v, ErrCode=%v, debug=%q",
		e.LastStreamID, e.ErrCode, e.DebugData)
}

func http2isEOFOrNetReadError(err error) bool {
	if err == io.EOF {
		return true
	}
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "read"
}

func (rl *http2clientConnReadLoop) cleanup() {
	cc := rl.cc
	defer cc.tconn.Close()
	defer cc.t.connPool().MarkDead(cc)
	defer close(cc.readerDone)

	if cc.idleTimer != nil {
		cc.idleTimer.Stop()
	}

	err := cc.readerErr
	cc.mu.Lock()
	if cc.goAway != nil && http2isEOFOrNetReadError(err) {
		err = http2GoAwayError{
			LastStreamID: cc.goAway.LastStreamID,
			ErrCode:      cc.goAway.ErrCode,
			DebugData:    cc.goAwayDebug,
		}
	} else if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	for _, cs := range rl.activeRes {
		cs.bufPipe.CloseWithError(err)
	}
	for _, cs := range cc.streams {
		select {
		case cs.resc <- http2resAndError{err: err}:
		default:
		}
		close(cs.done)
	}
	cc.closed = true
	cc.cond.Broadcast()
	cc.mu.Unlock()
}

func (rl *http2clientConnReadLoop) run() error {
	cc := rl.cc
	rl.closeWhenIdle = cc.t.disableKeepAlives() || cc.singleUse
	gotReply := false
	gotSettings := false
	for {
		f, err := cc.fr.ReadFrame()
		if err != nil {
			cc.vlogf("http2: Transport readFrame error on conn %p: (%T) %v", cc, err, err)
		}
		if se, ok := err.(http2StreamError); ok {
			if cs := cc.streamByID(se.StreamID, true); cs != nil {
				cs.cc.writeStreamReset(cs.ID, se.Code, err)
				if se.Cause == nil {
					se.Cause = cc.fr.errDetail
				}
				rl.endStreamError(cs, se)
			}
			continue
		} else if err != nil {
			return err
		}
		if http2VerboseLogs {
			cc.vlogf("http2: Transport received %s", http2summarizeFrame(f))
		}
		if !gotSettings {
			if _, ok := f.(*http2SettingsFrame); !ok {
				cc.logf("protocol error: received %T before a SETTINGS frame", f)
				return http2ConnectionError(http2ErrCodeProtocol)
			}
			gotSettings = true
		}
		maybeIdle := false

		switch f := f.(type) {
		case *http2MetaHeadersFrame:
			err = rl.processHeaders(f)
			maybeIdle = true
			gotReply = true
		case *http2DataFrame:
			err = rl.processData(f)
			maybeIdle = true
		case *http2GoAwayFrame:
			err = rl.processGoAway(f)
			maybeIdle = true
		case *http2RSTStreamFrame:
			err = rl.processResetStream(f)
			maybeIdle = true
		case *http2SettingsFrame:
			err = rl.processSettings(f)
		case *http2PushPromiseFrame:
			err = rl.processPushPromise(f)
		case *http2WindowUpdateFrame:
			err = rl.processWindowUpdate(f)
		case *http2PingFrame:
			err = rl.processPing(f)
		default:
			cc.logf("Transport: unhandled response frame type %T", f)
		}
		if err != nil {
			if http2VerboseLogs {
				cc.vlogf("http2: Transport conn %p received error from processing frame %v: %v", cc, http2summarizeFrame(f), err)
			}
			return err
		}
		if rl.closeWhenIdle && gotReply && maybeIdle && len(rl.activeRes) == 0 {
			cc.closeIfIdle()
		}
	}
}

func (rl *http2clientConnReadLoop) processHeaders(f *http2MetaHeadersFrame) error {
	cc := rl.cc
	cs := cc.streamByID(f.StreamID, f.StreamEnded())
	if cs == nil {

		return nil
	}
	if !cs.firstByte {
		if cs.trace != nil {

			http2traceFirstResponseByte(cs.trace)
		}
		cs.firstByte = true
	}
	if !cs.pastHeaders {
		cs.pastHeaders = true
	} else {
		return rl.processTrailers(cs, f)
	}

	res, err := rl.handleResponse(cs, f)
	if err != nil {
		if _, ok := err.(http2ConnectionError); ok {
			return err
		}

		cs.cc.writeStreamReset(f.StreamID, http2ErrCodeProtocol, err)
		cs.resc <- http2resAndError{err: err}
		return nil
	}
	if res == nil {

		return nil
	}
	if res.Body != http2noBody {
		rl.activeRes[cs.ID] = cs
	}
	cs.resTrailer = &res.Trailer
	cs.resc <- http2resAndError{res: res}
	return nil
}

// may return error types nil, or ConnectionError. Any other error value
// is a StreamError of type ErrCodeProtocol. The returned error in that case
// is the detail.
//
// As a special case, handleResponse may return (nil, nil) to skip the
// frame (currently only used for 100 expect continue). This special
// case is going away after Issue 13851 is fixed.
func (rl *http2clientConnReadLoop) handleResponse(cs *http2clientStream, f *http2MetaHeadersFrame) (*Response, error) {
	if f.Truncated {
		return nil, http2errResponseHeaderListSize
	}

	status := f.PseudoValue("status")
	if status == "" {
		return nil, errors.New("missing status pseudo header")
	}
	statusCode, err := strconv.Atoi(status)
	if err != nil {
		return nil, errors.New("malformed non-numeric status pseudo header")
	}

	if statusCode == 100 {
		http2traceGot100Continue(cs.trace)
		if cs.on100 != nil {
			cs.on100()
		}
		cs.pastHeaders = false
		return nil, nil
	}

	header := make(Header)
	res := &Response{
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		Header:     header,
		StatusCode: statusCode,
		Status:     status + " " + StatusText(statusCode),
	}
	for _, hf := range f.RegularFields() {
		key := CanonicalHeaderKey(hf.Name)
		if key == "Trailer" {
			t := res.Trailer
			if t == nil {
				t = make(Header)
				res.Trailer = t
			}
			http2foreachHeaderElement(hf.Value, func(v string) {
				t[CanonicalHeaderKey(v)] = nil
			})
		} else {
			header[key] = append(header[key], hf.Value)
		}
	}

	streamEnded := f.StreamEnded()
	isHead := cs.req.Method == "HEAD"
	if !streamEnded || isHead {
		res.ContentLength = -1
		if clens := res.Header["Content-Length"]; len(clens) == 1 {
			if clen64, err := strconv.ParseInt(clens[0], 10, 64); err == nil {
				res.ContentLength = clen64
			} else {

			}
		} else if len(clens) > 1 {

		}
	}

	if streamEnded || isHead {
		res.Body = http2noBody
		return res, nil
	}

	buf := new(bytes.Buffer)
	cs.bufPipe = http2pipe{b: buf}
	cs.bytesRemain = res.ContentLength
	res.Body = http2transportResponseBody{cs}
	go cs.awaitRequestCancel(cs.req)

	if cs.requestedGzip && res.Header.Get("Content-Encoding") == "gzip" {
		res.Header.Del("Content-Encoding")
		res.Header.Del("Content-Length")
		res.ContentLength = -1
		res.Body = &http2gzipReader{body: res.Body}
		http2setResponseUncompressed(res)
	}
	return res, nil
}

func (rl *http2clientConnReadLoop) processTrailers(cs *http2clientStream, f *http2MetaHeadersFrame) error {
	if cs.pastTrailers {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	cs.pastTrailers = true
	if !f.StreamEnded() {

		return http2ConnectionError(http2ErrCodeProtocol)
	}
	if len(f.PseudoFields()) > 0 {

		return http2ConnectionError(http2ErrCodeProtocol)
	}

	trailer := make(Header)
	for _, hf := range f.RegularFields() {
		key := CanonicalHeaderKey(hf.Name)
		trailer[key] = append(trailer[key], hf.Value)
	}
	cs.trailer = trailer

	rl.endStream(cs)
	return nil
}

// transportResponseBody is the concrete type of Transport.RoundTrip's
// Response.Body. It is an io.ReadCloser. On Read, it reads from cs.body.
// On Close it sends RST_STREAM if EOF wasn't already seen.
type http2transportResponseBody struct {
	cs *http2clientStream
}

func (b http2transportResponseBody) Read(p []byte) (n int, err error) {
	cs := b.cs
	cc := cs.cc

	if cs.readErr != nil {
		return 0, cs.readErr
	}
	n, err = b.cs.bufPipe.Read(p)
	if cs.bytesRemain != -1 {
		if int64(n) > cs.bytesRemain {
			n = int(cs.bytesRemain)
			if err == nil {
				err = errors.New("net/http: server replied with more than declared Content-Length; truncated")
				cc.writeStreamReset(cs.ID, http2ErrCodeProtocol, err)
			}
			cs.readErr = err
			return int(cs.bytesRemain), err
		}
		cs.bytesRemain -= int64(n)
		if err == io.EOF && cs.bytesRemain > 0 {
			err = io.ErrUnexpectedEOF
			cs.readErr = err
			return n, err
		}
	}
	if n == 0 {

		return
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	var connAdd, streamAdd int32

	if v := cc.inflow.available(); v < http2transportDefaultConnFlow/2 {
		connAdd = http2transportDefaultConnFlow - v
		cc.inflow.add(connAdd)
	}
	if err == nil {

		v := int(cs.inflow.available()) + cs.bufPipe.Len()
		if v < http2transportDefaultStreamFlow-http2transportDefaultStreamMinRefresh {
			streamAdd = int32(http2transportDefaultStreamFlow - v)
			cs.inflow.add(streamAdd)
		}
	}
	if connAdd != 0 || streamAdd != 0 {
		cc.wmu.Lock()
		defer cc.wmu.Unlock()
		if connAdd != 0 {
			cc.fr.WriteWindowUpdate(0, http2mustUint31(connAdd))
		}
		if streamAdd != 0 {
			cc.fr.WriteWindowUpdate(cs.ID, http2mustUint31(streamAdd))
		}
		cc.bw.Flush()
	}
	return
}

var http2errClosedResponseBody = errors.New("http2: response body closed")

func (b http2transportResponseBody) Close() error {
	cs := b.cs
	cc := cs.cc

	serverSentStreamEnd := cs.bufPipe.Err() == io.EOF
	unread := cs.bufPipe.Len()

	if unread > 0 || !serverSentStreamEnd {
		cc.mu.Lock()
		cc.wmu.Lock()
		if !serverSentStreamEnd {
			cc.fr.WriteRSTStream(cs.ID, http2ErrCodeCancel)
		}

		if unread > 0 {
			cc.inflow.add(int32(unread))
			cc.fr.WriteWindowUpdate(0, uint32(unread))
		}
		cc.bw.Flush()
		cc.wmu.Unlock()
		cc.mu.Unlock()
	}

	cs.bufPipe.BreakWithError(http2errClosedResponseBody)
	return nil
}

func (rl *http2clientConnReadLoop) processData(f *http2DataFrame) error {
	cc := rl.cc
	cs := cc.streamByID(f.StreamID, f.StreamEnded())
	data := f.Data()
	if cs == nil {
		cc.mu.Lock()
		neverSent := cc.nextStreamID
		cc.mu.Unlock()
		if f.StreamID >= neverSent {

			cc.logf("http2: Transport received unsolicited DATA frame; closing connection")
			return http2ConnectionError(http2ErrCodeProtocol)
		}

		if f.Length > 0 {
			cc.mu.Lock()
			cc.inflow.add(int32(f.Length))
			cc.mu.Unlock()

			cc.wmu.Lock()
			cc.fr.WriteWindowUpdate(0, uint32(f.Length))
			cc.bw.Flush()
			cc.wmu.Unlock()
		}
		return nil
	}
	if f.Length > 0 {
		if len(data) > 0 && cs.bufPipe.b == nil {

			cc.logf("http2: Transport received DATA frame for closed stream; closing connection")
			return http2ConnectionError(http2ErrCodeProtocol)
		}

		cc.mu.Lock()
		if cs.inflow.available() >= int32(f.Length) {
			cs.inflow.take(int32(f.Length))
		} else {
			cc.mu.Unlock()
			return http2ConnectionError(http2ErrCodeFlowControl)
		}

		if pad := int32(f.Length) - int32(len(data)); pad > 0 {
			cs.inflow.add(pad)
			cc.inflow.add(pad)
			cc.wmu.Lock()
			cc.fr.WriteWindowUpdate(0, uint32(pad))
			cc.fr.WriteWindowUpdate(cs.ID, uint32(pad))
			cc.bw.Flush()
			cc.wmu.Unlock()
		}
		didReset := cs.didReset
		cc.mu.Unlock()

		if len(data) > 0 && !didReset {
			if _, err := cs.bufPipe.Write(data); err != nil {
				rl.endStreamError(cs, err)
				return err
			}
		}
	}

	if f.StreamEnded() {
		rl.endStream(cs)
	}
	return nil
}

var http2errInvalidTrailers = errors.New("http2: invalid trailers")

func (rl *http2clientConnReadLoop) endStream(cs *http2clientStream) {

	rl.endStreamError(cs, nil)
}

func (rl *http2clientConnReadLoop) endStreamError(cs *http2clientStream, err error) {
	var code func()
	if err == nil {
		err = io.EOF
		code = cs.copyTrailers
	}
	cs.bufPipe.closeWithErrorAndCode(err, code)
	delete(rl.activeRes, cs.ID)
	if http2isConnectionCloseRequest(cs.req) {
		rl.closeWhenIdle = true
	}

	select {
	case cs.resc <- http2resAndError{err: err}:
	default:
	}
}

func (cs *http2clientStream) copyTrailers() {
	for k, vv := range cs.trailer {
		t := cs.resTrailer
		if *t == nil {
			*t = make(Header)
		}
		(*t)[k] = vv
	}
}

func (rl *http2clientConnReadLoop) processGoAway(f *http2GoAwayFrame) error {
	cc := rl.cc
	cc.t.connPool().MarkDead(cc)
	if f.ErrCode != 0 {

		cc.vlogf("transport got GOAWAY with error code = %v", f.ErrCode)
	}
	cc.setGoAway(f)
	return nil
}

func (rl *http2clientConnReadLoop) processSettings(f *http2SettingsFrame) error {
	cc := rl.cc
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if f.IsAck() {
		if cc.wantSettingsAck {
			cc.wantSettingsAck = false
			return nil
		}
		return http2ConnectionError(http2ErrCodeProtocol)
	}

	err := f.ForeachSetting(func(s http2Setting) error {
		switch s.ID {
		case http2SettingMaxFrameSize:
			cc.maxFrameSize = s.Val
		case http2SettingMaxConcurrentStreams:
			cc.maxConcurrentStreams = s.Val
		case http2SettingInitialWindowSize:

			if s.Val > math.MaxInt32 {
				return http2ConnectionError(http2ErrCodeFlowControl)
			}

			delta := int32(s.Val) - int32(cc.initialWindowSize)
			for _, cs := range cc.streams {
				cs.flow.add(delta)
			}
			cc.cond.Broadcast()

			cc.initialWindowSize = s.Val
		default:

			cc.vlogf("Unhandled Setting: %v", s)
		}
		return nil
	})
	if err != nil {
		return err
	}

	cc.wmu.Lock()
	defer cc.wmu.Unlock()

	cc.fr.WriteSettingsAck()
	cc.bw.Flush()
	return cc.werr
}

func (rl *http2clientConnReadLoop) processWindowUpdate(f *http2WindowUpdateFrame) error {
	cc := rl.cc
	cs := cc.streamByID(f.StreamID, false)
	if f.StreamID != 0 && cs == nil {
		return nil
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	fl := &cc.flow
	if cs != nil {
		fl = &cs.flow
	}
	if !fl.add(int32(f.Increment)) {
		return http2ConnectionError(http2ErrCodeFlowControl)
	}
	cc.cond.Broadcast()
	return nil
}

func (rl *http2clientConnReadLoop) processResetStream(f *http2RSTStreamFrame) error {
	cs := rl.cc.streamByID(f.StreamID, true)
	if cs == nil {

		return nil
	}
	select {
	case <-cs.peerReset:

	default:
		err := http2streamError(cs.ID, f.ErrCode)
		cs.resetErr = err
		close(cs.peerReset)
		cs.bufPipe.CloseWithError(err)
		cs.cc.cond.Broadcast()
	}
	delete(rl.activeRes, cs.ID)
	return nil
}

// Ping sends a PING frame to the server and waits for the ack.
// Public implementation is in go17.go and not_go17.go
func (cc *http2ClientConn) ping(ctx http2contextContext) error {
	c := make(chan struct{})
	// Generate a random payload
	var p [8]byte
	for {
		if _, err := rand.Read(p[:]); err != nil {
			return err
		}
		cc.mu.Lock()

		if _, found := cc.pings[p]; !found {
			cc.pings[p] = c
			cc.mu.Unlock()
			break
		}
		cc.mu.Unlock()
	}
	cc.wmu.Lock()
	if err := cc.fr.WritePing(false, p); err != nil {
		cc.wmu.Unlock()
		return err
	}
	if err := cc.bw.Flush(); err != nil {
		cc.wmu.Unlock()
		return err
	}
	cc.wmu.Unlock()
	select {
	case <-c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-cc.readerDone:

		return cc.readerErr
	}
}

func (rl *http2clientConnReadLoop) processPing(f *http2PingFrame) error {
	if f.IsAck() {
		cc := rl.cc
		cc.mu.Lock()
		defer cc.mu.Unlock()

		if c, ok := cc.pings[f.Data]; ok {
			close(c)
			delete(cc.pings, f.Data)
		}
		return nil
	}
	cc := rl.cc
	cc.wmu.Lock()
	defer cc.wmu.Unlock()
	if err := cc.fr.WritePing(true, f.Data); err != nil {
		return err
	}
	return cc.bw.Flush()
}

func (rl *http2clientConnReadLoop) processPushPromise(f *http2PushPromiseFrame) error {

	return http2ConnectionError(http2ErrCodeProtocol)
}

func (cc *http2ClientConn) writeStreamReset(streamID uint32, code http2ErrCode, err error) {

	cc.wmu.Lock()
	cc.fr.WriteRSTStream(streamID, code)
	cc.bw.Flush()
	cc.wmu.Unlock()
}

var (
	http2errResponseHeaderListSize = errors.New("http2: response header list larger than advertised limit")
	http2errPseudoTrailers         = errors.New("http2: invalid pseudo header in trailers")
)

func (cc *http2ClientConn) logf(format string, args ...interface{}) {
	cc.t.logf(format, args...)
}

func (cc *http2ClientConn) vlogf(format string, args ...interface{}) {
	cc.t.vlogf(format, args...)
}

func (t *http2Transport) vlogf(format string, args ...interface{}) {
	if http2VerboseLogs {
		t.logf(format, args...)
	}
}

func (t *http2Transport) logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

var http2noBody io.ReadCloser = ioutil.NopCloser(bytes.NewReader(nil))

func http2strSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

type http2erringRoundTripper struct{ err error }

func (rt http2erringRoundTripper) RoundTrip(*Request) (*Response, error) { return nil, rt.err }

// gzipReader wraps a response body so it can lazily
// call gzip.NewReader on the first call to Read
type http2gzipReader struct {
	body io.ReadCloser // underlying Response.Body
	zr   *gzip.Reader  // lazily-initialized gzip reader
	zerr error         // sticky error
}

func (gz *http2gzipReader) Read(p []byte) (n int, err error) {
	if gz.zerr != nil {
		return 0, gz.zerr
	}
	if gz.zr == nil {
		gz.zr, err = gzip.NewReader(gz.body)
		if err != nil {
			gz.zerr = err
			return 0, err
		}
	}
	return gz.zr.Read(p)
}

func (gz *http2gzipReader) Close() error {
	return gz.body.Close()
}

type http2errorReader struct{ err error }

func (r http2errorReader) Read(p []byte) (int, error) { return 0, r.err }

// bodyWriterState encapsulates various state around the Transport's writing
// of the request body, particularly regarding doing delayed writes of the body
// when the request contains "Expect: 100-continue".
type http2bodyWriterState struct {
	cs     *http2clientStream
	timer  *time.Timer   // if non-nil, we're doing a delayed write
	fnonce *sync.Once    // to call fn with
	fn     func()        // the code to run in the goroutine, writing the body
	resc   chan error    // result of fn's execution
	delay  time.Duration // how long we should delay a delayed write for
}

func (t *http2Transport) getBodyWriterState(cs *http2clientStream, body io.Reader) (s http2bodyWriterState) {
	s.cs = cs
	if body == nil {
		return
	}
	resc := make(chan error, 1)
	s.resc = resc
	s.fn = func() {
		cs.cc.mu.Lock()
		cs.startedWrite = true
		cs.cc.mu.Unlock()
		resc <- cs.writeRequestBody(body, cs.req.Body)
	}
	s.delay = t.expectContinueTimeout()
	if s.delay == 0 ||
		!httplex.HeaderValuesContainsToken(
			cs.req.Header["Expect"],
			"100-continue") {
		return
	}
	s.fnonce = new(sync.Once)

	// Arm the timer with a very large duration, which we'll
	// intentionally lower later. It has to be large now because
	// we need a handle to it before writing the headers, but the
	// s.delay value is defined to not start until after the
	// request headers were written.
	const hugeDuration = 365 * 24 * time.Hour
	s.timer = time.AfterFunc(hugeDuration, func() {
		s.fnonce.Do(s.fn)
	})
	return
}

func (s http2bodyWriterState) cancel() {
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s http2bodyWriterState) on100() {
	if s.timer == nil {

		return
	}
	s.timer.Stop()
	go func() { s.fnonce.Do(s.fn) }()
}

// scheduleBodyWrite starts writing the body, either immediately (in
// the common case) or after the delay timeout. It should not be
// called until after the headers have been written.
func (s http2bodyWriterState) scheduleBodyWrite() {
	if s.timer == nil {

		go s.fn()
		return
	}
	http2traceWait100Continue(s.cs.trace)
	if s.timer.Stop() {
		s.timer.Reset(s.delay)
	}
}

// isConnectionCloseRequest reports whether req should use its own
// connection for a single request and then close the connection.
func http2isConnectionCloseRequest(req *Request) bool {
	return req.Close || httplex.HeaderValuesContainsToken(req.Header["Connection"], "close")
}

// writeFramer is implemented by any type that is used to write frames.
type http2writeFramer interface {
	writeFrame(http2writeContext) error

	// staysWithinBuffer reports whether this writer promises that
	// it will only write less than or equal to size bytes, and it
	// won't Flush the write context.
	staysWithinBuffer(size int) bool
}

// writeContext is the interface needed by the various frame writer
// types below. All the writeFrame methods below are scheduled via the
// frame writing scheduler (see writeScheduler in writesched.go).
//
// This interface is implemented by *serverConn.
//
// TODO: decide whether to a) use this in the client code (which didn't
// end up using this yet, because it has a simpler design, not
// currently implementing priorities), or b) delete this and
// make the server code a bit more concrete.
type http2writeContext interface {
	Framer() *http2Framer
	Flush() error
	CloseConn() error
	// HeaderEncoder returns an HPACK encoder that writes to the
	// returned buffer.
	HeaderEncoder() (*hpack.Encoder, *bytes.Buffer)
}

// writeEndsStream reports whether w writes a frame that will transition
// the stream to a half-closed local state. This returns false for RST_STREAM,
// which closes the entire stream (not just the local half).
func http2writeEndsStream(w http2writeFramer) bool {
	switch v := w.(type) {
	case *http2writeData:
		return v.endStream
	case *http2writeResHeaders:
		return v.endStream
	case nil:

		panic("writeEndsStream called on nil writeFramer")
	}
	return false
}

type http2flushFrameWriter struct{}

func (http2flushFrameWriter) writeFrame(ctx http2writeContext) error {
	return ctx.Flush()
}

func (http2flushFrameWriter) staysWithinBuffer(max int) bool { return false }

type http2writeSettings []http2Setting

func (s http2writeSettings) staysWithinBuffer(max int) bool {
	const settingSize = 6 // uint16 + uint32
	return http2frameHeaderLen+settingSize*len(s) <= max

}

func (s http2writeSettings) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteSettings([]http2Setting(s)...)
}

type http2writeGoAway struct {
	maxStreamID uint32
	code        http2ErrCode
}

func (p *http2writeGoAway) writeFrame(ctx http2writeContext) error {
	err := ctx.Framer().WriteGoAway(p.maxStreamID, p.code, nil)
	if p.code != 0 {
		ctx.Flush()
		time.Sleep(50 * time.Millisecond)
		ctx.CloseConn()
	}
	return err
}

func (*http2writeGoAway) staysWithinBuffer(max int) bool { return false }

type http2writeData struct {
	streamID  uint32
	p         []byte
	endStream bool
}

func (w *http2writeData) String() string {
	return fmt.Sprintf("writeData(stream=%d, p=%d, endStream=%v)", w.streamID, len(w.p), w.endStream)
}

func (w *http2writeData) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteData(w.streamID, w.endStream, w.p)
}

func (w *http2writeData) staysWithinBuffer(max int) bool {
	return http2frameHeaderLen+len(w.p) <= max
}

// handlerPanicRST is the message sent from handler goroutines when
// the handler panics.
type http2handlerPanicRST struct {
	StreamID uint32
}

func (hp http2handlerPanicRST) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteRSTStream(hp.StreamID, http2ErrCodeInternal)
}

func (hp http2handlerPanicRST) staysWithinBuffer(max int) bool { return http2frameHeaderLen+4 <= max }

func (se http2StreamError) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteRSTStream(se.StreamID, se.Code)
}

func (se http2StreamError) staysWithinBuffer(max int) bool { return http2frameHeaderLen+4 <= max }

type http2writePingAck struct{ pf *http2PingFrame }

func (w http2writePingAck) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WritePing(true, w.pf.Data)
}

func (w http2writePingAck) staysWithinBuffer(max int) bool {
	return http2frameHeaderLen+len(w.pf.Data) <= max
}

type http2writeSettingsAck struct{}

func (http2writeSettingsAck) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteSettingsAck()
}

func (http2writeSettingsAck) staysWithinBuffer(max int) bool { return http2frameHeaderLen <= max }

// splitHeaderBlock splits headerBlock into fragments so that each fragment fits
// in a single frame, then calls fn for each fragment. firstFrag/lastFrag are true
// for the first/last fragment, respectively.
func http2splitHeaderBlock(ctx http2writeContext, headerBlock []byte, fn func(ctx http2writeContext, frag []byte, firstFrag, lastFrag bool) error) error {
	// For now we're lazy and just pick the minimum MAX_FRAME_SIZE
	// that all peers must support (16KB). Later we could care
	// more and send larger frames if the peer advertised it, but
	// there's little point. Most headers are small anyway (so we
	// generally won't have CONTINUATION frames), and extra frames
	// only waste 9 bytes anyway.
	const maxFrameSize = 16384

	first := true
	for len(headerBlock) > 0 {
		frag := headerBlock
		if len(frag) > maxFrameSize {
			frag = frag[:maxFrameSize]
		}
		headerBlock = headerBlock[len(frag):]
		if err := fn(ctx, frag, first, len(headerBlock) == 0); err != nil {
			return err
		}
		first = false
	}
	return nil
}

// writeResHeaders is a request to write a HEADERS and 0+ CONTINUATION frames
// for HTTP response headers or trailers from a server handler.
type http2writeResHeaders struct {
	streamID    uint32
	httpResCode int      // 0 means no ":status" line
	h           Header   // may be nil
	trailers    []string // if non-nil, which keys of h to write. nil means all.
	endStream   bool

	date          string
	contentType   string
	contentLength string
}

func http2encKV(enc *hpack.Encoder, k, v string) {
	if http2VerboseLogs {
		log.Printf("http2: server encoding header %q = %q", k, v)
	}
	enc.WriteField(hpack.HeaderField{Name: k, Value: v})
}

func (w *http2writeResHeaders) staysWithinBuffer(max int) bool {

	return false
}

func (w *http2writeResHeaders) writeFrame(ctx http2writeContext) error {
	enc, buf := ctx.HeaderEncoder()
	buf.Reset()

	if w.httpResCode != 0 {
		http2encKV(enc, ":status", http2httpCodeString(w.httpResCode))
	}

	http2encodeHeaders(enc, w.h, w.trailers)

	if w.contentType != "" {
		http2encKV(enc, "content-type", w.contentType)
	}
	if w.contentLength != "" {
		http2encKV(enc, "content-length", w.contentLength)
	}
	if w.date != "" {
		http2encKV(enc, "date", w.date)
	}

	headerBlock := buf.Bytes()
	if len(headerBlock) == 0 && w.trailers == nil {
		panic("unexpected empty hpack")
	}

	return http2splitHeaderBlock(ctx, headerBlock, w.writeHeaderBlock)
}

func (w *http2writeResHeaders) writeHeaderBlock(ctx http2writeContext, frag []byte, firstFrag, lastFrag bool) error {
	if firstFrag {
		return ctx.Framer().WriteHeaders(http2HeadersFrameParam{
			StreamID:      w.streamID,
			BlockFragment: frag,
			EndStream:     w.endStream,
			EndHeaders:    lastFrag,
		})
	} else {
		return ctx.Framer().WriteContinuation(w.streamID, lastFrag, frag)
	}
}

// writePushPromise is a request to write a PUSH_PROMISE and 0+ CONTINUATION frames.
type http2writePushPromise struct {
	streamID uint32   // pusher stream
	method   string   // for :method
	url      *url.URL // for :scheme, :authority, :path
	h        Header

	// Creates an ID for a pushed stream. This runs on serveG just before
	// the frame is written. The returned ID is copied to promisedID.
	allocatePromisedID func() (uint32, error)
	promisedID         uint32
}

func (w *http2writePushPromise) staysWithinBuffer(max int) bool {

	return false
}

func (w *http2writePushPromise) writeFrame(ctx http2writeContext) error {
	enc, buf := ctx.HeaderEncoder()
	buf.Reset()

	http2encKV(enc, ":method", w.method)
	http2encKV(enc, ":scheme", w.url.Scheme)
	http2encKV(enc, ":authority", w.url.Host)
	http2encKV(enc, ":path", w.url.RequestURI())
	http2encodeHeaders(enc, w.h, nil)

	headerBlock := buf.Bytes()
	if len(headerBlock) == 0 {
		panic("unexpected empty hpack")
	}

	return http2splitHeaderBlock(ctx, headerBlock, w.writeHeaderBlock)
}

func (w *http2writePushPromise) writeHeaderBlock(ctx http2writeContext, frag []byte, firstFrag, lastFrag bool) error {
	if firstFrag {
		return ctx.Framer().WritePushPromise(http2PushPromiseParam{
			StreamID:      w.streamID,
			PromiseID:     w.promisedID,
			BlockFragment: frag,
			EndHeaders:    lastFrag,
		})
	} else {
		return ctx.Framer().WriteContinuation(w.streamID, lastFrag, frag)
	}
}

type http2write100ContinueHeadersFrame struct {
	streamID uint32
}

func (w http2write100ContinueHeadersFrame) writeFrame(ctx http2writeContext) error {
	enc, buf := ctx.HeaderEncoder()
	buf.Reset()
	http2encKV(enc, ":status", "100")
	return ctx.Framer().WriteHeaders(http2HeadersFrameParam{
		StreamID:      w.streamID,
		BlockFragment: buf.Bytes(),
		EndStream:     false,
		EndHeaders:    true,
	})
}

func (w http2write100ContinueHeadersFrame) staysWithinBuffer(max int) bool {

	return 9+2*(len(":status")+len("100")) <= max
}

type http2writeWindowUpdate struct {
	streamID uint32 // or 0 for conn-level
	n        uint32
}

func (wu http2writeWindowUpdate) staysWithinBuffer(max int) bool { return http2frameHeaderLen+4 <= max }

func (wu http2writeWindowUpdate) writeFrame(ctx http2writeContext) error {
	return ctx.Framer().WriteWindowUpdate(wu.streamID, wu.n)
}

// encodeHeaders encodes an http.Header. If keys is not nil, then (k, h[k])
// is encoded only only if k is in keys.
func http2encodeHeaders(enc *hpack.Encoder, h Header, keys []string) {
	if keys == nil {
		sorter := http2sorterPool.Get().(*http2sorter)

		defer http2sorterPool.Put(sorter)
		keys = sorter.Keys(h)
	}
	for _, k := range keys {
		vv := h[k]
		k = http2lowerHeader(k)
		if !http2validWireHeaderFieldName(k) {

			continue
		}
		isTE := k == "transfer-encoding"
		for _, v := range vv {
			if !httplex.ValidHeaderFieldValue(v) {

				continue
			}

			if isTE && v != "trailers" {
				continue
			}
			http2encKV(enc, k, v)
		}
	}
}

// WriteScheduler is the interface implemented by HTTP/2 write schedulers.
// Methods are never called concurrently.
type http2WriteScheduler interface {
	// OpenStream opens a new stream in the write scheduler.
	// It is illegal to call this with streamID=0 or with a streamID that is
	// already open -- the call may panic.
	OpenStream(streamID uint32, options http2OpenStreamOptions)

	// CloseStream closes a stream in the write scheduler. Any frames queued on
	// this stream should be discarded. It is illegal to call this on a stream
	// that is not open -- the call may panic.
	CloseStream(streamID uint32)

	// AdjustStream adjusts the priority of the given stream. This may be called
	// on a stream that has not yet been opened or has been closed. Note that
	// RFC 7540 allows PRIORITY frames to be sent on streams in any state. See:
	// https://tools.ietf.org/html/rfc7540#section-5.1
	AdjustStream(streamID uint32, priority http2PriorityParam)

	// Push queues a frame in the scheduler. In most cases, this will not be
	// called with wr.StreamID()!=0 unless that stream is currently open. The one
	// exception is RST_STREAM frames, which may be sent on idle or closed streams.
	Push(wr http2FrameWriteRequest)

	// Pop dequeues the next frame to write. Returns false if no frames can
	// be written. Frames with a given wr.StreamID() are Pop'd in the same
	// order they are Push'd.
	Pop() (wr http2FrameWriteRequest, ok bool)
}

// OpenStreamOptions specifies extra options for WriteScheduler.OpenStream.
type http2OpenStreamOptions struct {
	// PusherID is zero if the stream was initiated by the client. Otherwise,
	// PusherID names the stream that pushed the newly opened stream.
	PusherID uint32
}

// FrameWriteRequest is a request to write a frame.
type http2FrameWriteRequest struct {
	// write is the interface value that does the writing, once the
	// WriteScheduler has selected this frame to write. The write
	// functions are all defined in write.go.
	write http2writeFramer

	// stream is the stream on which this frame will be written.
	// nil for non-stream frames like PING and SETTINGS.
	stream *http2stream

	// done, if non-nil, must be a buffered channel with space for
	// 1 message and is sent the return value from write (or an
	// earlier error) when the frame has been written.
	done chan error
}

// StreamID returns the id of the stream this frame will be written to.
// 0 is used for non-stream frames such as PING and SETTINGS.
func (wr http2FrameWriteRequest) StreamID() uint32 {
	if wr.stream == nil {
		if se, ok := wr.write.(http2StreamError); ok {

			return se.StreamID
		}
		return 0
	}
	return wr.stream.id
}

// DataSize returns the number of flow control bytes that must be consumed
// to write this entire frame. This is 0 for non-DATA frames.
func (wr http2FrameWriteRequest) DataSize() int {
	if wd, ok := wr.write.(*http2writeData); ok {
		return len(wd.p)
	}
	return 0
}

// Consume consumes min(n, available) bytes from this frame, where available
// is the number of flow control bytes available on the stream. Consume returns
// 0, 1, or 2 frames, where the integer return value gives the number of frames
// returned.
//
// If flow control prevents consuming any bytes, this returns (_, _, 0). If
// the entire frame was consumed, this returns (wr, _, 1). Otherwise, this
// returns (consumed, rest, 2), where 'consumed' contains the consumed bytes and
// 'rest' contains the remaining bytes. The consumed bytes are deducted from the
// underlying stream's flow control budget.
func (wr http2FrameWriteRequest) Consume(n int32) (http2FrameWriteRequest, http2FrameWriteRequest, int) {
	var empty http2FrameWriteRequest

	wd, ok := wr.write.(*http2writeData)
	if !ok || len(wd.p) == 0 {
		return wr, empty, 1
	}

	allowed := wr.stream.flow.available()
	if n < allowed {
		allowed = n
	}
	if wr.stream.sc.maxFrameSize < allowed {
		allowed = wr.stream.sc.maxFrameSize
	}
	if allowed <= 0 {
		return empty, empty, 0
	}
	if len(wd.p) > int(allowed) {
		wr.stream.flow.take(allowed)
		consumed := http2FrameWriteRequest{
			stream: wr.stream,
			write: &http2writeData{
				streamID: wd.streamID,
				p:        wd.p[:allowed],

				endStream: false,
			},

			done: nil,
		}
		rest := http2FrameWriteRequest{
			stream: wr.stream,
			write: &http2writeData{
				streamID:  wd.streamID,
				p:         wd.p[allowed:],
				endStream: wd.endStream,
			},
			done: wr.done,
		}
		return consumed, rest, 2
	}

	wr.stream.flow.take(int32(len(wd.p)))
	return wr, empty, 1
}

// String is for debugging only.
func (wr http2FrameWriteRequest) String() string {
	var des string
	if s, ok := wr.write.(fmt.Stringer); ok {
		des = s.String()
	} else {
		des = fmt.Sprintf("%T", wr.write)
	}
	return fmt.Sprintf("[FrameWriteRequest stream=%d, ch=%v, writer=%v]", wr.StreamID(), wr.done != nil, des)
}

// replyToWriter sends err to wr.done and panics if the send must block
// This does nothing if wr.done is nil.
func (wr *http2FrameWriteRequest) replyToWriter(err error) {
	if wr.done == nil {
		return
	}
	select {
	case wr.done <- err:
	default:
		panic(fmt.Sprintf("unbuffered done channel passed in for type %T", wr.write))
	}
	wr.write = nil
}

// writeQueue is used by implementations of WriteScheduler.
type http2writeQueue struct {
	s []http2FrameWriteRequest
}

func (q *http2writeQueue) empty() bool { return len(q.s) == 0 }

func (q *http2writeQueue) push(wr http2FrameWriteRequest) {
	q.s = append(q.s, wr)
}

func (q *http2writeQueue) shift() http2FrameWriteRequest {
	if len(q.s) == 0 {
		panic("invalid use of queue")
	}
	wr := q.s[0]

	copy(q.s, q.s[1:])
	q.s[len(q.s)-1] = http2FrameWriteRequest{}
	q.s = q.s[:len(q.s)-1]
	return wr
}

// consume consumes up to n bytes from q.s[0]. If the frame is
// entirely consumed, it is removed from the queue. If the frame
// is partially consumed, the frame is kept with the consumed
// bytes removed. Returns true iff any bytes were consumed.
func (q *http2writeQueue) consume(n int32) (http2FrameWriteRequest, bool) {
	if len(q.s) == 0 {
		return http2FrameWriteRequest{}, false
	}
	consumed, rest, numresult := q.s[0].Consume(n)
	switch numresult {
	case 0:
		return http2FrameWriteRequest{}, false
	case 1:
		q.shift()
	case 2:
		q.s[0] = rest
	}
	return consumed, true
}

type http2writeQueuePool []*http2writeQueue

// put inserts an unused writeQueue into the pool.
func (p *http2writeQueuePool) put(q *http2writeQueue) {
	for i := range q.s {
		q.s[i] = http2FrameWriteRequest{}
	}
	q.s = q.s[:0]
	*p = append(*p, q)
}

// get returns an empty writeQueue.
func (p *http2writeQueuePool) get() *http2writeQueue {
	ln := len(*p)
	if ln == 0 {
		return new(http2writeQueue)
	}
	x := ln - 1
	q := (*p)[x]
	(*p)[x] = nil
	*p = (*p)[:x]
	return q
}

// RFC 7540, Section 5.3.5: the default weight is 16.
const http2priorityDefaultWeight = 15 // 16 = 15 + 1

// PriorityWriteSchedulerConfig configures a priorityWriteScheduler.
type http2PriorityWriteSchedulerConfig struct {
	// MaxClosedNodesInTree controls the maximum number of closed streams to
	// retain in the priority tree. Setting this to zero saves a small amount
	// of memory at the cost of performance.
	//
	// See RFC 7540, Section 5.3.4:
	//   "It is possible for a stream to become closed while prioritization
	//   information ... is in transit. ... This potentially creates suboptimal
	//   prioritization, since the stream could be given a priority that is
	//   different from what is intended. To avoid these problems, an endpoint
	//   SHOULD retain stream prioritization state for a period after streams
	//   become closed. The longer state is retained, the lower the chance that
	//   streams are assigned incorrect or default priority values."
	MaxClosedNodesInTree int

	// MaxIdleNodesInTree controls the maximum number of idle streams to
	// retain in the priority tree. Setting this to zero saves a small amount
	// of memory at the cost of performance.
	//
	// See RFC 7540, Section 5.3.4:
	//   Similarly, streams that are in the "idle" state can be assigned
	//   priority or become a parent of other streams. This allows for the
	//   creation of a grouping node in the dependency tree, which enables
	//   more flexible expressions of priority. Idle streams begin with a
	//   default priority (Section 5.3.5).
	MaxIdleNodesInTree int

	// ThrottleOutOfOrderWrites enables write throttling to help ensure that
	// data is delivered in priority order. This works around a race where
	// stream B depends on stream A and both streams are about to call Write
	// to queue DATA frames. If B wins the race, a naive scheduler would eagerly
	// write as much data from B as possible, but this is suboptimal because A
	// is a higher-priority stream. With throttling enabled, we write a small
	// amount of data from B to minimize the amount of bandwidth that B can
	// steal from A.
	ThrottleOutOfOrderWrites bool
}

// NewPriorityWriteScheduler constructs a WriteScheduler that schedules
// frames by following HTTP/2 priorities as described in RFC 7340 Section 5.3.
// If cfg is nil, default options are used.
func http2NewPriorityWriteScheduler(cfg *http2PriorityWriteSchedulerConfig) http2WriteScheduler {
	if cfg == nil {

		cfg = &http2PriorityWriteSchedulerConfig{
			MaxClosedNodesInTree:     10,
			MaxIdleNodesInTree:       10,
			ThrottleOutOfOrderWrites: false,
		}
	}

	ws := &http2priorityWriteScheduler{
		nodes:                make(map[uint32]*http2priorityNode),
		maxClosedNodesInTree: cfg.MaxClosedNodesInTree,
		maxIdleNodesInTree:   cfg.MaxIdleNodesInTree,
		enableWriteThrottle:  cfg.ThrottleOutOfOrderWrites,
	}
	ws.nodes[0] = &ws.root
	if cfg.ThrottleOutOfOrderWrites {
		ws.writeThrottleLimit = 1024
	} else {
		ws.writeThrottleLimit = math.MaxInt32
	}
	return ws
}

type http2priorityNodeState int

const (
	http2priorityNodeOpen http2priorityNodeState = iota
	http2priorityNodeClosed
	http2priorityNodeIdle
)

// priorityNode is a node in an HTTP/2 priority tree.
// Each node is associated with a single stream ID.
// See RFC 7540, Section 5.3.
type http2priorityNode struct {
	q            http2writeQueue        // queue of pending frames to write
	id           uint32                 // id of the stream, or 0 for the root of the tree
	weight       uint8                  // the actual weight is weight+1, so the value is in [1,256]
	state        http2priorityNodeState // open | closed | idle
	bytes        int64                  // number of bytes written by this node, or 0 if closed
	subtreeBytes int64                  // sum(node.bytes) of all nodes in this subtree

	// These links form the priority tree.
	parent     *http2priorityNode
	kids       *http2priorityNode // start of the kids list
	prev, next *http2priorityNode // doubly-linked list of siblings
}

func (n *http2priorityNode) setParent(parent *http2priorityNode) {
	if n == parent {
		panic("setParent to self")
	}
	if n.parent == parent {
		return
	}

	if parent := n.parent; parent != nil {
		if n.prev == nil {
			parent.kids = n.next
		} else {
			n.prev.next = n.next
		}
		if n.next != nil {
			n.next.prev = n.prev
		}
	}

	n.parent = parent
	if parent == nil {
		n.next = nil
		n.prev = nil
	} else {
		n.next = parent.kids
		n.prev = nil
		if n.next != nil {
			n.next.prev = n
		}
		parent.kids = n
	}
}

func (n *http2priorityNode) addBytes(b int64) {
	n.bytes += b
	for ; n != nil; n = n.parent {
		n.subtreeBytes += b
	}
}

// walkReadyInOrder iterates over the tree in priority order, calling f for each node
// with a non-empty write queue. When f returns true, this funcion returns true and the
// walk halts. tmp is used as scratch space for sorting.
//
// f(n, openParent) takes two arguments: the node to visit, n, and a bool that is true
// if any ancestor p of n is still open (ignoring the root node).
func (n *http2priorityNode) walkReadyInOrder(openParent bool, tmp *[]*http2priorityNode, f func(*http2priorityNode, bool) bool) bool {
	if !n.q.empty() && f(n, openParent) {
		return true
	}
	if n.kids == nil {
		return false
	}

	if n.id != 0 {
		openParent = openParent || (n.state == http2priorityNodeOpen)
	}

	w := n.kids.weight
	needSort := false
	for k := n.kids.next; k != nil; k = k.next {
		if k.weight != w {
			needSort = true
			break
		}
	}
	if !needSort {
		for k := n.kids; k != nil; k = k.next {
			if k.walkReadyInOrder(openParent, tmp, f) {
				return true
			}
		}
		return false
	}

	*tmp = (*tmp)[:0]
	for n.kids != nil {
		*tmp = append(*tmp, n.kids)
		n.kids.setParent(nil)
	}
	sort.Sort(http2sortPriorityNodeSiblings(*tmp))
	for i := len(*tmp) - 1; i >= 0; i-- {
		(*tmp)[i].setParent(n)
	}
	for k := n.kids; k != nil; k = k.next {
		if k.walkReadyInOrder(openParent, tmp, f) {
			return true
		}
	}
	return false
}

type http2sortPriorityNodeSiblings []*http2priorityNode

func (z http2sortPriorityNodeSiblings) Len() int { return len(z) }

func (z http2sortPriorityNodeSiblings) Swap(i, k int) { z[i], z[k] = z[k], z[i] }

func (z http2sortPriorityNodeSiblings) Less(i, k int) bool {

	wi, bi := float64(z[i].weight+1), float64(z[i].subtreeBytes)
	wk, bk := float64(z[k].weight+1), float64(z[k].subtreeBytes)
	if bi == 0 && bk == 0 {
		return wi >= wk
	}
	if bk == 0 {
		return false
	}
	return bi/bk <= wi/wk
}

type http2priorityWriteScheduler struct {
	// root is the root of the priority tree, where root.id = 0.
	// The root queues control frames that are not associated with any stream.
	root http2priorityNode

	// nodes maps stream ids to priority tree nodes.
	nodes map[uint32]*http2priorityNode

	// maxID is the maximum stream id in nodes.
	maxID uint32

	// lists of nodes that have been closed or are idle, but are kept in
	// the tree for improved prioritization. When the lengths exceed either
	// maxClosedNodesInTree or maxIdleNodesInTree, old nodes are discarded.
	closedNodes, idleNodes []*http2priorityNode

	// From the config.
	maxClosedNodesInTree int
	maxIdleNodesInTree   int
	writeThrottleLimit   int32
	enableWriteThrottle  bool

	// tmp is scratch space for priorityNode.walkReadyInOrder to reduce allocations.
	tmp []*http2priorityNode

	// pool of empty queues for reuse.
	queuePool http2writeQueuePool
}

func (ws *http2priorityWriteScheduler) OpenStream(streamID uint32, options http2OpenStreamOptions) {

	if curr := ws.nodes[streamID]; curr != nil {
		if curr.state != http2priorityNodeIdle {
			panic(fmt.Sprintf("stream %d already opened", streamID))
		}
		curr.state = http2priorityNodeOpen
		return
	}

	parent := ws.nodes[options.PusherID]
	if parent == nil {
		parent = &ws.root
	}
	n := &http2priorityNode{
		q:      *ws.queuePool.get(),
		id:     streamID,
		weight: http2priorityDefaultWeight,
		state:  http2priorityNodeOpen,
	}
	n.setParent(parent)
	ws.nodes[streamID] = n
	if streamID > ws.maxID {
		ws.maxID = streamID
	}
}

func (ws *http2priorityWriteScheduler) CloseStream(streamID uint32) {
	if streamID == 0 {
		panic("violation of WriteScheduler interface: cannot close stream 0")
	}
	if ws.nodes[streamID] == nil {
		panic(fmt.Sprintf("violation of WriteScheduler interface: unknown stream %d", streamID))
	}
	if ws.nodes[streamID].state != http2priorityNodeOpen {
		panic(fmt.Sprintf("violation of WriteScheduler interface: stream %d already closed", streamID))
	}

	n := ws.nodes[streamID]
	n.state = http2priorityNodeClosed
	n.addBytes(-n.bytes)

	q := n.q
	ws.queuePool.put(&q)
	n.q.s = nil
	if ws.maxClosedNodesInTree > 0 {
		ws.addClosedOrIdleNode(&ws.closedNodes, ws.maxClosedNodesInTree, n)
	} else {
		ws.removeNode(n)
	}
}

func (ws *http2priorityWriteScheduler) AdjustStream(streamID uint32, priority http2PriorityParam) {
	if streamID == 0 {
		panic("adjustPriority on root")
	}

	n := ws.nodes[streamID]
	if n == nil {
		if streamID <= ws.maxID || ws.maxIdleNodesInTree == 0 {
			return
		}
		ws.maxID = streamID
		n = &http2priorityNode{
			q:      *ws.queuePool.get(),
			id:     streamID,
			weight: http2priorityDefaultWeight,
			state:  http2priorityNodeIdle,
		}
		n.setParent(&ws.root)
		ws.nodes[streamID] = n
		ws.addClosedOrIdleNode(&ws.idleNodes, ws.maxIdleNodesInTree, n)
	}

	parent := ws.nodes[priority.StreamDep]
	if parent == nil {
		n.setParent(&ws.root)
		n.weight = http2priorityDefaultWeight
		return
	}

	if n == parent {
		return
	}

	for x := parent.parent; x != nil; x = x.parent {
		if x == n {
			parent.setParent(n.parent)
			break
		}
	}

	if priority.Exclusive {
		k := parent.kids
		for k != nil {
			next := k.next
			if k != n {
				k.setParent(n)
			}
			k = next
		}
	}

	n.setParent(parent)
	n.weight = priority.Weight
}

func (ws *http2priorityWriteScheduler) Push(wr http2FrameWriteRequest) {
	var n *http2priorityNode
	if id := wr.StreamID(); id == 0 {
		n = &ws.root
	} else {
		n = ws.nodes[id]
		if n == nil {

			if wr.DataSize() > 0 {
				panic("add DATA on non-open stream")
			}
			n = &ws.root
		}
	}
	n.q.push(wr)
}

func (ws *http2priorityWriteScheduler) Pop() (wr http2FrameWriteRequest, ok bool) {
	ws.root.walkReadyInOrder(false, &ws.tmp, func(n *http2priorityNode, openParent bool) bool {
		limit := int32(math.MaxInt32)
		if openParent {
			limit = ws.writeThrottleLimit
		}
		wr, ok = n.q.consume(limit)
		if !ok {
			return false
		}
		n.addBytes(int64(wr.DataSize()))

		if openParent {
			ws.writeThrottleLimit += 1024
			if ws.writeThrottleLimit < 0 {
				ws.writeThrottleLimit = math.MaxInt32
			}
		} else if ws.enableWriteThrottle {
			ws.writeThrottleLimit = 1024
		}
		return true
	})
	return wr, ok
}

func (ws *http2priorityWriteScheduler) addClosedOrIdleNode(list *[]*http2priorityNode, maxSize int, n *http2priorityNode) {
	if maxSize == 0 {
		return
	}
	if len(*list) == maxSize {

		ws.removeNode((*list)[0])
		x := (*list)[1:]
		copy(*list, x)
		*list = (*list)[:len(x)]
	}
	*list = append(*list, n)
}

func (ws *http2priorityWriteScheduler) removeNode(n *http2priorityNode) {
	for k := n.kids; k != nil; k = k.next {
		k.setParent(n.parent)
	}
	n.setParent(nil)
	delete(ws.nodes, n.id)
}

// NewRandomWriteScheduler constructs a WriteScheduler that ignores HTTP/2
// priorities. Control frames like SETTINGS and PING are written before DATA
// frames, but if no control frames are queued and multiple streams have queued
// HEADERS or DATA frames, Pop selects a ready stream arbitrarily.
func http2NewRandomWriteScheduler() http2WriteScheduler {
	return &http2randomWriteScheduler{sq: make(map[uint32]*http2writeQueue)}
}

type http2randomWriteScheduler struct {
	// zero are frames not associated with a specific stream.
	zero http2writeQueue

	// sq contains the stream-specific queues, keyed by stream ID.
	// When a stream is idle or closed, it's deleted from the map.
	sq map[uint32]*http2writeQueue

	// pool of empty queues for reuse.
	queuePool http2writeQueuePool
}

func (ws *http2randomWriteScheduler) OpenStream(streamID uint32, options http2OpenStreamOptions) {

}

func (ws *http2randomWriteScheduler) CloseStream(streamID uint32) {
	q, ok := ws.sq[streamID]
	if !ok {
		return
	}
	delete(ws.sq, streamID)
	ws.queuePool.put(q)
}

func (ws *http2randomWriteScheduler) AdjustStream(streamID uint32, priority http2PriorityParam) {

}

func (ws *http2randomWriteScheduler) Push(wr http2FrameWriteRequest) {
	id := wr.StreamID()
	if id == 0 {
		ws.zero.push(wr)
		return
	}
	q, ok := ws.sq[id]
	if !ok {
		q = ws.queuePool.get()
		ws.sq[id] = q
	}
	q.push(wr)
}

func (ws *http2randomWriteScheduler) Pop() (http2FrameWriteRequest, bool) {

	if !ws.zero.empty() {
		return ws.zero.shift(), true
	}

	for _, q := range ws.sq {
		if wr, ok := q.consume(math.MaxInt32); ok {
			return wr, true
		}
	}
	return http2FrameWriteRequest{}, false
}
