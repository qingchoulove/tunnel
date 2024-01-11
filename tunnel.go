package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
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
	cancelFunc context.CancelFunc
}

func NewTunnel(ctx context.Context, signal Signal) (*Tunnel, error) {
	ctx, cancelFunc := context.WithCancel(ctx)
	return &Tunnel{
		ctx:        ctx,
		signal:     signal,
		cancelFunc: cancelFunc,
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

// TODO: temporary code ↓
func (t *Tunnel) Test(mode string) {
	if mode == "server" {
		t.runHandler()
	} else {
		go t.keepAlive()
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
				fmt.Printf("Message: ")
				r := bufio.NewReader(os.Stdin)
				var in []byte
				for {
					var err error
					in, err = r.ReadBytes('\n')
					if err != io.EOF {
						if err != nil {
							break
						}
					}
					if len(in) > 0 {
						break
					}
				}
				msg, err := NewDataMessage(t.localNAT.Token, in).Marshal()
				if err != nil {
					log.Debugf("marshal message error: %s\n", err)
					continue
				}
				_, err = t.conn.WriteTo(msg, &t.remoteAddr)
				if err != nil {
					log.Debugf("send message error: %s\n", err)
					continue
				}
			}
		}
	}
}

func (t *Tunnel) runHandler() {
	bytes := make([]byte, 1024)
	timeout := time.NewTimer(time.Second * 10)
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-timeout.C:
			t.Close()
			return
		default:
			err := t.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
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
			if msg.token != t.remoteNAT.Token {
				log.Debugf("received message with unexpected token: %s\n", msg.token)
				continue
			}
			switch msg.mType {
			case MessageTypeHandshake:
				log.Debugf("received handshake message from %s\n", addr.String())
			case MessageTypePing:
				if !timeout.Stop() {
					<-timeout.C
				}
				timeout.Reset(time.Second * 10)
			case MessageTypeData:
				log.Debugf("received data message from %s, payload: %s\n", addr.String(), string(msg.payload))
			}
		}
	}
}

func (t *Tunnel) keepAlive() {
	// send and wait receive ping message
	pingMsg, err := NewPingMessage(t.localNAT.Token).Marshal()
	if err != nil {
		return
	}
	tick := time.NewTicker(time.Second)
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tick.C:
			_, _ = t.conn.WriteTo(pingMsg, &t.remoteAddr)
		}
	}
}

// TODO: temporary code ↑

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

func (t *Tunnel) Close() {
	t.cancelFunc()
	err := t.conn.Close()
	if err != nil {
		log.Debugf("close tunnel error: %s\n", err)
	}
}
