package tunnel

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Tunnel struct {
	ctx        context.Context
	conn       net.PacketConn
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
		return nil
	}
	return err
}

// TODO: temporary code
func (t *Tunnel) Ping(client bool) {
	go t.runHandler()
	go t.keepAlive()
	tick := time.Tick(time.Second * 5)
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tick:
			msg, err := NewDataMessage(t.localNAT.Token, []byte("hello world")).Marshal()
			if err != nil {
				log.Debugf("marshal message error: %s\n", err)
				continue
			}
			_, _ = t.conn.WriteTo(msg, &t.remoteAddr)
		}
	}
}

func (t *Tunnel) runHandler() {
	bytes := make([]byte, 1024)
	for {
		err := t.conn.SetReadDeadline(time.Now().Add(time.Second * 10))
		if err != nil {
			log.Debugf("set read deadline error: %s\n", err)
			continue
		}
		n, addr, err := t.conn.ReadFrom(bytes)
		if err != nil {
			log.Debugf("read from conn error: %s\n", err)
			continue
		}
		if addr.String() != t.remoteAddr.String() {
			log.Debugf("received message from unexpected addr: %s\n", addr.String())
			continue
		}
		msg, err := UnmarshalMessage(bytes[:n])
		if err != nil {
			log.Debugf("unmarshal message error: %s\n", err)
			continue
		}
		switch msg.mType {
		case MessageTypeHandshake:
			log.Debugf("received handshake message from %s\n", addr.String())
		case MessageTypePing:
			log.Debugf("received ping message from %s\n", addr.String())
		case MessageTypeData:
			log.Debugf("received data message from %s, payload: %s\n", addr.String(), string(msg.payload))
		}
	}
}

func (t *Tunnel) keepAlive() {
	// send and wait receive ping message
	pingMsg, err := NewPingMessage(t.localNAT.Token).Marshal()
	if err != nil {
		return
	}
	tick := time.Tick(time.Second)
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tick:
			_, _ = t.conn.WriteTo(pingMsg, &t.remoteAddr)
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
