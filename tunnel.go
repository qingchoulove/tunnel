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
			log.Debugf("tunnel ping rev, %s\n", string(bytes[:n]))
		}
	}()
	for {
		time.Sleep(time.Second)
		_, _ = t.conn.WriteTo([]byte("ping"), &t.remoteAddr)
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
	addr, err := net.ResolveUDPAddr("udp4", localNAT.Addr)
	if err != nil {
		return err
	}
	t.localAddr = net.UDPAddr{
		IP:   net.IPv4zero,
		Port: addr.Port,
	}
	return nil
}
