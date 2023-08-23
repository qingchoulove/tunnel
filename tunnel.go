package tunnel

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Tunnel struct {
	ctx        context.Context
	conn       net.UDPConn
	localAddr  net.UDPAddr
	remoteAddr net.UDPAddr
	localNAT   *NATDetail
	remoteNAT  *NATDetail
	signal     Signal
}

func NewTunnel(signal Signal) (*Tunnel, error) {
	return &Tunnel{
		ctx:    context.Background(),
		signal: signal,
	}, nil
}

func (t *Tunnel) Connect() error {
	err := t.initTunnel()
	if err != nil {
		return err
	}
	c := handshake(t)
	err, notClosed := <-c
	if !notClosed {
		log.Debugln("tunnel hole punch success")
		log.Debugf("local addr: %s, remote addr: %s\n", t.localAddr.String(), t.remoteAddr.String())
		err = t.validate()
		if err != nil {
			return err
		}
		log.Debugln("tunnel connect success")
		return nil
	}
	return err
}

// TODO: temporary code
func (t *Tunnel) Ping() {
	go func() {
		bytes := make([]byte, 1024)
		for {
			err := t.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
			if err != nil {
				log.Debugf("tunnel ping err, %s\n", err)
				break
			}
			n, _, err := t.conn.ReadFrom(bytes)
			if err != nil {
				log.Debugf("tunnel ping err, %s\n", err)
				break
			}
			msg, err := UnmarshalMessage(bytes[:n])
			if err != nil {
				log.Debugf("tunnel ping err, %s\n", err)
				break
			}
			if msg.mType == MessageTypePing || msg.mType == MessageTypeHandshake {
				log.Debugln("tunnel ping rev, ping or handshake")
			} else {
				log.Debugf("tunnel ping rev, %s\n", string(msg.payload))
			}
		}
	}()
	msg, err := NewDataMessage(t.localNAT.Token, []byte("ping")).Marshal()
	if err != nil {
		log.Debugf("tunnel ping err, %s\n", err)
		return
	}
	for {
		time.Sleep(time.Second)
		_, _ = t.conn.WriteTo(msg, &t.remoteAddr)
	}
}

func (t *Tunnel) validate() error {
	// send handshake message will receive a ping message
	msg, err := NewHandshakeMessage(t.localNAT.Token).Marshal()
	if err != nil {
		return err
	}
	pingMsg, err := NewPingMessage(t.localNAT.Token).Marshal()
	if err != nil {
		return err
	}
	timeout := time.After(10 * time.Second)
	bytes := make([]byte, 1024)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("validate timeout")
		default:
			time.Sleep(time.Second)
			n, err := t.conn.WriteTo(msg, &t.remoteAddr)
			if err != nil {
				return err
			}
			// rev
			err = t.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			if err != nil {
				return err
			}
			n, _, err = t.conn.ReadFrom(bytes)
			if err != nil {
				break
			}
			rev, err := UnmarshalMessage(bytes[:n])
			if err != nil {
				break
			}
			_, _ = t.conn.WriteTo(pingMsg, &t.remoteAddr)
			if rev.mType == MessageTypePing {
				return nil
			}
		}
	}
}

func (t *Tunnel) initTunnel() error {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()
	resolver, err := NewResolver(conn)
	if err != nil {
		return err
	}
	defer resolver.Close()
	localNAT, err := resolver.Resolve()
	if err != nil {
		return err
	}
	t.localNAT = localNAT
	err = t.signal.SendSignal(localNAT)
	if err != nil {
		return err
	}
	remoteNAT, err := t.signal.ReadSignal()
	if err != nil {
		return err
	}
	log.Debugf("remote nat type: %d, token: %s, addr: %s\n", remoteNAT.NATType, remoteNAT.Token, remoteNAT.Addr)
	// if both NATs are symmetric, we can't do anything
	if remoteNAT.NATType == NATTypeSymmetric && localNAT.NATType == NATTypeSymmetric {
		return fmt.Errorf("symmetric NAT not supported")
	}
	t.localNAT = localNAT
	t.remoteNAT = remoteNAT
	t.localAddr = net.UDPAddr{
		IP:   net.IPv4zero,
		Port: conn.LocalAddr().(*net.UDPAddr).Port,
	}
	return nil
}
