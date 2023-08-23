package tunnel

import (
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"
	"time"
)

// TODO: better timeout
const timeout = time.Second * 30

// probability of success is 98.34%
const triesNum = 512

func handshake(tunnel *Tunnel) chan error {
	cDone := make(chan error, 1)
	local := tunnel.localNAT
	remote := tunnel.remoteNAT

	if local.NATType != NATTypeSymmetric && remote.NATType != NATTypeSymmetric {
		// both are not symmetric NAT
		// handshake
		go handshakeNonSymmetric(tunnel, cDone)
	} else if local.NATType == NATTypeSymmetric {
		// local is symmetric NAT
		// select local port
		go handshakeLocalSymmetric(tunnel, cDone)
	} else if remote.NATType == NATTypeSymmetric {
		// remote is symmetric NAT
		// select remote port
		go handshakeRemoteSymmetric(tunnel, cDone)
	}
	return cDone
}

func handshakeLocalSymmetric(tunnel *Tunnel, done chan error) {
	log.Debugln("handshake local symmetric ...")
	remote := tunnel.remoteNAT
	local := tunnel.localNAT
	remoteAddr, err := net.ResolveUDPAddr("udp4", remote.Addr)
	if err != nil {
		done <- err
		return
	}
	c := make(chan *net.UDPAddr, 1)
	stopChan := make(chan int, 1)
	var selected int32 = 0
	// birthday attack
	for i := 0; i < triesNum; i++ {
		time.Sleep(time.Millisecond)
		go func() {
			conn, err := net.ListenUDP("udp4", nil)
			if err != nil {
				log.Debugf("udp listen err, %s\n", err)
				return
			}
			defer func() {
				_ = conn.Close()
			}()
			// send handshake
			go udpWrite(conn, remoteAddr, NewHandshakeMessage(local.Token), stopChan)
			// rev response
			msg, _, err := udpRead(conn)
			if err != nil {
				return
			}
			if msg.token != remote.Token {
				log.Debugf("token fail, token: %s\n", msg.token)
				return
			}
			// only select the first one
			if !atomic.CompareAndSwapInt32(&selected, 0, 1) {
				return
			}
			localAddr := conn.LocalAddr().(*net.UDPAddr)
			// ensure conn is closed
			_ = conn.Close()
			close(stopChan)
			c <- localAddr
		}()
	}
	select {
	case <-time.After(timeout):
		done <- fmt.Errorf("timeout")
	case localAddr := <-c:
		conn, err := net.ListenUDP("udp4", localAddr)
		if err != nil {
			done <- err
			return
		}
		tunnel.conn = *conn
		tunnel.remoteAddr = *remoteAddr
		close(done)
	}
}

func handshakeRemoteSymmetric(tunnel *Tunnel, done chan error) {
	log.Debugln("handshake remote symmetric ...")
	remote := tunnel.remoteNAT
	local := tunnel.localNAT
	conn, err := net.ListenUDP("udp4", &tunnel.localAddr)
	if err != nil {
		done <- err
		return
	}
	remoteAddr, err := net.ResolveUDPAddr("udp4", remote.Addr)
	if err != nil {
		done <- err
		return
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	randPorts := r.Perm(65535)
	stopChan := make(chan int, 1)
	// send handshake
	go func() {
	OUTER:
		for i := 0; i < triesNum; i++ {
			time.Sleep(time.Millisecond)
			select {
			case <-stopChan:
				break OUTER
			default:
				dst := &net.UDPAddr{
					IP:   remoteAddr.IP,
					Port: randPorts[i],
				}
				go udpWrite(conn, dst, NewHandshakeMessage(local.Token), stopChan)
			}
		}
	}()
	// rev response
	msg, dst, err := udpRead(conn)
	close(stopChan)
	if err != nil {
		done <- err
		return
	}
	if msg.token != remote.Token {
		done <- fmt.Errorf("token fail, token: %s", msg.token)
		return
	}
	tunnel.remoteAddr = *dst
	tunnel.conn = *conn
	close(done)
}

func handshakeNonSymmetric(tunnel *Tunnel, done chan error) {
	remote := tunnel.remoteNAT
	local := tunnel.localNAT
	conn, err := net.ListenUDP("udp4", &tunnel.localAddr)
	if err != nil {
		done <- err
		return
	}
	addr, err := net.ResolveUDPAddr("udp4", remote.Addr)
	if err != nil {
		done <- err
		return
	}
	stopChan := make(chan int, 1)
	// send handshake
	go udpWrite(conn, addr, NewHandshakeMessage(local.Token), stopChan)
	// rev response
	msg, _, err := udpRead(conn)
	close(stopChan)
	if err != nil {
		log.Debugf("udp read err, %s\n", err)
		done <- err
		return
	}
	if msg.token != remote.Token {
		done <- fmt.Errorf("token fail, token: %s", msg.token)
		return
	}
	tunnel.remoteAddr = *addr
	tunnel.conn = *conn
	close(done)
}

func udpWrite(conn *net.UDPConn, addr *net.UDPAddr, msg *Message, stopChan chan int) {
	bytes, _ := msg.Marshal()
	timeout := time.After(timeout)
	tick := time.Tick(time.Second)
OUTER:
	for {
		select {
		case <-stopChan:
			break OUTER
		case <-timeout:
			break OUTER
		case <-tick:
			_, err := conn.WriteTo(bytes, addr)
			if err != nil {
				break OUTER
			}
		}
	}
}

func udpRead(conn *net.UDPConn) (*Message, *net.UDPAddr, error) {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, nil, err
	}
	bytes := make([]byte, 128)
	n, dst, err := conn.ReadFrom(bytes)
	if err != nil {
		return nil, nil, err
	}
	msg, err := UnmarshalMessage(bytes[:n])
	if err != nil {
		return nil, nil, err
	}
	return msg, dst.(*net.UDPAddr), nil
}
