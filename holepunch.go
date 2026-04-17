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
			err = udpWrite(conn, remoteAddr, NewHandshakeMessage(local.Token))
			if err != nil {
				return
			}
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
		tunnel.conn = conn
		tunnel.remoteAddr = *remoteAddr
		close(done)
	}
}

func handshakeRemoteSymmetric(tunnel *Tunnel, done chan error) {
	log.Debugln("handshake remote symmetric ...")
	remote := tunnel.remoteNAT
	local := tunnel.localNAT
	candidates := candidateAddrs(remote)

	conn, err := net.ListenUDP("udp4", &tunnel.localAddr)
	if err != nil {
		done <- err
		return
	}

	stopChan := make(chan struct{})
	// spray handshakes at all candidates concurrently on the same conn
	for _, baseAddr := range candidates {
		baseAddr := baseAddr
		go func() {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			randPorts := r.Perm(65535)
			for i := 0; i < triesNum; i++ {
				time.Sleep(time.Millisecond)
				select {
				case <-stopChan:
					return
				default:
					dst := &net.UDPAddr{IP: baseAddr.IP, Port: randPorts[i]}
					_ = udpWrite(conn, dst, NewHandshakeMessage(local.Token))
				}
			}
		}()
	}

	for {
		msg, dst, err := udpRead(conn)
		if err != nil {
			close(stopChan)
			conn.Close()
			done <- err
			return
		}
		if msg.token != remote.Token {
			continue
		}
		close(stopChan)
		_ = udpWrite(conn, dst, NewHandshakeMessage(local.Token))
		tunnel.conn = conn
		tunnel.remoteAddr = *dst
		close(done)
		return
	}
}

func handshakeNonSymmetric(tunnel *Tunnel, done chan error) {
	remote := tunnel.remoteNAT
	local := tunnel.localNAT
	candidates := candidateAddrs(remote)

	conn, err := net.ListenUDP("udp4", &tunnel.localAddr)
	if err != nil {
		done <- err
		return
	}

	stopChan := make(chan struct{})
	// keep sending handshake to all candidates until we get a response
	for _, addr := range candidates {
		addr := addr
		go func() {
			for {
				select {
				case <-stopChan:
					return
				default:
					if err := udpWrite(conn, addr, NewHandshakeMessage(local.Token)); err != nil {
						log.Debugf("write err for %s: %s\n", addr, err)
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
		}()
	}

	// read loop: reply to first handshake received, then wait for the reply-ack
	var remoteAddr *net.UDPAddr
	for {
		msg, src, err := udpRead(conn)
		if err != nil {
			close(stopChan)
			conn.Close()
			log.Debugf("udp read err, %s\n", err)
			done <- err
			return
		}
		if msg.token != remote.Token {
			continue
		}
		if remoteAddr == nil {
			// first valid handshake received — reply so the other side can finish
			remoteAddr = src
			_ = udpWrite(conn, src, NewHandshakeMessage(local.Token))
		}
		// once we've sent our reply (or received theirs), we're done
		close(stopChan)
		tunnel.conn = conn
		tunnel.remoteAddr = *remoteAddr
		close(done)
		return
	}
}

// candidateAddrs returns all addresses to try for the remote peer:
// the public (STUN-mapped) address first, followed by any LAN addresses.
func candidateAddrs(remote *NATDetail) []*net.UDPAddr {
	seen := map[string]bool{}
	var addrs []*net.UDPAddr
	for _, s := range append([]string{remote.Addr}, remote.LocalAddrs...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		a, err := net.ResolveUDPAddr("udp4", s)
		if err != nil {
			continue
		}
		addrs = append(addrs, a)
	}
	return addrs
}

func udpWrite(conn *net.UDPConn, addr *net.UDPAddr, msg *Message) error {
	bytes, _ := msg.Marshal()
	_, err := conn.WriteTo(bytes, addr)
	if err != nil {
		return err
	}
	return nil
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
