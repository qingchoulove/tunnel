package tunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/quic-go/quic-go"
	"io"
	"math/big"
	"net"
	"time"
)

func quicServer(localAddr net.UDPAddr) {
	conn, err := net.ListenPacket("udp", localAddr.String())
	if err != nil {
		log.Debugf("listen error: %v\n", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Debugf("close conn error: %s\n", err.Error())
		}
	}()
	tlsCfg, err := generateTLSConfig()
	if err != nil {
		log.Debugf("generate tls config error: %v\n", err)
		return
	}
	tr := quic.Transport{
		Conn: conn,
	}
	listener, err := tr.Listen(tlsCfg, nil)
	if err != nil {
		log.Debugf("listen error: %v\n", err)
		return
	}
	defer listener.Close()
	ctx := context.Background()
	for {
		log.Debugf("quic server accept\n")
		session, err := listener.Accept(ctx)
		if err != nil {
			log.Debugf("listener error: %v", err)
			continue
		}
		go serverSessionHandler(ctx, session)
	}
}

func quicClient(localAddr net.UDPAddr, remoteAddr net.UDPAddr) {
	conn, err := net.ListenPacket("udp", localAddr.String())
	if err != nil {
		log.Debugf("listen error: %v\n", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Debugf("close conn error: %s\n", err.Error())
		}
	}()
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"tunnel"},
	}
	tr := quic.Transport{
		Conn: conn,
	}
	ctx := context.Background()
	connection, err := tr.Dial(ctx, &remoteAddr, tlsConf, nil)
	if err != nil {
		log.Debugf("dial error: %v\n", err)
		return
	}
	stream, err := connection.OpenStreamSync(ctx)
	if err != nil {
		log.Debugf("open stream error: %v\n", err)
		return
	}
	tick := time.Tick(time.Second)
OUTER:
	for {
		select {
		case <-tick:
			_, err = stream.Write([]byte("foobar"))
			if err != nil {
				log.Debugf("write error: %s\n", err.Error())
				break OUTER
			}
			log.Debugln("client write")
		}
	}
	log.Debugln("client exit")
}

func serverSessionHandler(ctx context.Context, session quic.Connection) {
	log.Debugln("handling session...")
	defer func() {
		if err := session.CloseWithError(0, "close"); err != nil {
			log.Debugf("session close error: %v\n", err)
		}
	}()
	for {
		stream, err := session.AcceptStream(ctx)
		if err != nil {
			log.Debugf("session error: %v\n", err)
			break
		}
		go serverStreamHandler(stream)
	}
}

func streamReadHandler(stream io.ReadWriteCloser) {
	log.Debugf("handling stream...")
	bytes := make([]byte, 1024)
	for {
		n, err := stream.Read(bytes)
		if err != nil {
			log.Debugf("read error: %s\n", err.Error())
			break
		}
		log.Debugf("read: %s\n", string(bytes[:n]))
	}
}

func serverStreamHandler(stream io.ReadWriteCloser) {
	log.Debugf("handling stream...")
	defer stream.Close()
	bytes := make([]byte, 1024)
OUTER:
	for {
		n, err := stream.Read(bytes)
		if err != nil {
			log.Debugf("read error: %s\n", err.Error())
			break OUTER
		}
		log.Debugf("server read: %s\n", string(bytes[:n]))
		_, err = stream.Write(bytes[:n])
		if err != nil {
			log.Debugf("write error: %s\n", err.Error())
			break OUTER
		}
	}
}

func generateTLSConfig() (*tls.Config, error) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"tunnel"},
	}, nil
}
