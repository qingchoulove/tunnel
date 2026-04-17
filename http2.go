package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/quic-go/quic-go"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// quicStreamConn wraps a quic.Stream as a net.Conn for use with http2 libraries.
type quicStreamConn struct {
	quic.Stream
	localAddr  net.Addr
	remoteAddr net.Addr
}

func newQuicStreamConn(stream quic.Stream, conn quic.Connection) *quicStreamConn {
	return &quicStreamConn{
		Stream:     stream,
		localAddr:  conn.LocalAddr(),
		remoteAddr: conn.RemoteAddr(),
	}
}

func (c *quicStreamConn) LocalAddr() net.Addr  { return c.localAddr }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.remoteAddr }

func (c *quicStreamConn) Close() error {
	c.Stream.CancelRead(0)
	return c.Stream.Close()
}

// quicStreamListener implements net.Listener over a QUIC session's streams.
type quicStreamListener struct {
	session quic.Connection
	ctx     context.Context
	connCh  chan net.Conn
	once    sync.Once
	done    chan struct{}
}

func newQuicStreamListener(ctx context.Context, session quic.Connection) *quicStreamListener {
	l := &quicStreamListener{
		session: session,
		ctx:     ctx,
		connCh:  make(chan net.Conn, 16),
		done:    make(chan struct{}),
	}
	go l.run()
	return l
}

func (l *quicStreamListener) run() {
	defer l.once.Do(func() { close(l.done) })
	for {
		stream, err := l.session.AcceptStream(l.ctx)
		if err != nil {
			return
		}
		l.connCh <- newQuicStreamConn(stream, l.session)
	}
}

func (l *quicStreamListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-l.ctx.Done():
		return nil, l.ctx.Err()
	}
}

func (l *quicStreamListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *quicStreamListener) Addr() net.Addr { return l.session.LocalAddr() }

// Peer represents an established P2P HTTP/2 connection.
// Both sides can register handlers and send requests — the roles are symmetric.
type Peer struct {
	// Client sends HTTP/2 requests to the remote peer.
	Client *http.Client

	srv *http.Server
	ln  net.Listener
	mux *http.ServeMux
}

// Handle registers a handler on the local HTTP/2 server (served to the remote peer).
func (p *Peer) Handle(pattern string, handler http.HandlerFunc) {
	p.mux.HandleFunc(pattern, handler)
}

func (p *Peer) serve() {
	go p.srv.Serve(p.ln) //nolint:errcheck
}

// ConnectHTTP2 establishes a symmetric HTTP/2 connection over the P2P tunnel.
// Both sides concurrently open a stream (for sending) and accept a stream (for receiving).
// No role negotiation needed — each side uses its own outbound stream as the HTTP/2
// client transport, and serves inbound streams with h2c.
func (t *Tunnel) ConnectHTTP2() (*Peer, error) {
	qw := upgrade(t)
	return qw.connectHTTP2()
}

func (q *QuicWrapper) connectHTTP2() (*Peer, error) {
	ctx := q.ctx

	// Establish QUIC connection: token-greater peer dials, other listens.
	// This is only to get a single shared QUIC connection — HTTP/2 roles are symmetric.
	session, err := q.quicConnect(ctx)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	h2srv := &http2.Server{}
	srv := &http.Server{Handler: h2c.NewHandler(mux, h2srv)}

	// Each peer opens one stream and accepts one stream.
	// OpenStreamSync + NewClientConn must run concurrently with AcceptStream:
	// NewClientConn writes the HTTP/2 preface immediately, which sends the first
	// STREAM frame and unblocks the peer's AcceptStream.
	type streamResult struct {
		stream quic.Stream
		err    error
	}
	type ccResult struct {
		cc  *http2.ClientConn
		err error
	}
	ccCh := make(chan ccResult, 1)
	acceptCh := make(chan streamResult, 1)

	go func() {
		s, err := session.OpenStreamSync(ctx)
		if err != nil {
			ccCh <- ccResult{err: err}
			return
		}
		conn := newQuicStreamConn(s, session)
		cc, err := (&http2.Transport{}).NewClientConn(conn)
		if err != nil {
			conn.Close()
		}
		ccCh <- ccResult{cc: cc, err: err}
	}()

	go func() {
		s, err := session.AcceptStream(ctx)
		acceptCh <- streamResult{s, err}
	}()

	ccRes := <-ccCh
	if ccRes.err != nil {
		session.CloseWithError(0, "")
		return nil, fmt.Errorf("open stream: %w", ccRes.err)
	}
	acceptRes := <-acceptCh
	if acceptRes.err != nil {
		session.CloseWithError(0, "")
		return nil, fmt.Errorf("accept stream: %w", acceptRes.err)
	}

	inConn := newQuicStreamConn(acceptRes.stream, session)
	inLn := &oneShotListener{conn: inConn, done: make(chan struct{})}

	peer := &Peer{
		Client: &http.Client{Transport: ccRes.cc},
		srv:    srv,
		ln:     inLn,
		mux:    mux,
	}
	peer.serve()
	return peer, nil
}

// quicConnect returns a QUIC session to the remote peer.
// Token comparison deterministically assigns roles: greater token dials, lesser listens.
// This ensures exactly one connection is established with unambiguous direction.
func (q *QuicWrapper) quicConnect(ctx context.Context) (quic.Connection, error) {
	localToken := q.tunnel.localNAT.Token
	remoteToken := q.tunnel.remoteNAT.Token

	if localToken > remoteToken {
		return q.tr.Dial(ctx, &q.tunnel.remoteAddr, &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"h2", "tunnel"},
		}, nil)
	}

	tlsCfg, err := generateTLSConfig()
	if err != nil {
		return nil, err
	}
	listener, err := q.tr.Listen(tlsCfg, nil)
	if err != nil {
		return nil, err
	}
	session, err := listener.Accept(ctx)
	if err != nil {
		listener.Close()
		return nil, err
	}
	// Do not close listener — closing it terminates all accepted sessions.
	return session, nil
}

// oneShotListener serves exactly one pre-accepted net.Conn then returns ErrClosed.
type oneShotListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func (l *oneShotListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn; close(l.done) })
	if c != nil {
		return c, nil
	}
	return nil, net.ErrClosed
}

func (l *oneShotListener) Close() error   { return nil }
func (l *oneShotListener) Addr() net.Addr { return l.conn.LocalAddr() }
