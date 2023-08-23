package tunnel

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/pion/logging"
	"github.com/pion/stun"
	"github.com/pion/turn/v2"
	"net"
	"sync"
	"time"
)

type request struct {
	stun       string
	changeIp   bool
	changePort bool
}

var stunServers = []request{
	{
		stun:       "stun.miwifi.com:3478",
		changeIp:   false,
		changePort: false,
	},
	{
		stun:       "stun.syncthing.net:3478",
		changeIp:   false,
		changePort: false,
	},
	{
		stun:       "stun.miwifi.com:3478",
		changeIp:   true,
		changePort: true,
	},
	{
		stun:       "stun.miwifi.com:3478",
		changeIp:   false,
		changePort: true,
	},
}

type NATType int

const (
	NATTypeFullCone NATType = iota + 1
	NATTypeRestrictedCone
	NATTypePortRestrictedCone
	NATTypeSymmetric
)

type NATDetail struct {
	Addr    string  `json:"addr"`
	NATType NATType `json:"nat_type"`
	Token   string  `json:"token"`
}

type Resolver struct {
	conn   net.PacketConn
	client *turn.Client
}

func (r Resolver) Resolve() (*NATDetail, error) {

	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	log.Debugf("generate local token: %s\n", token)
	mappedAddrs := make([]string, len(stunServers))

	var wg sync.WaitGroup

	for idx, req := range stunServers {
		wg.Add(1)
		go func(idx int, req request) {
			defer wg.Done()
			mappedAddr, err := r.test(req.stun, req.changeIp, req.changePort)
			if err != nil {
				return
			}
			mappedAddrs[idx] = mappedAddr
		}(idx, req)
	}
	log.Debugln("wait for stun server response")
	wg.Wait()
	var nType NATType
	if mappedAddrs[0] == "" || mappedAddrs[1] == "" {
		return nil, fmt.Errorf("failed to resolve stun server")
	}

	if mappedAddrs[0] != mappedAddrs[1] {
		nType = NATTypeSymmetric
	} else if mappedAddrs[0] == mappedAddrs[2] {
		nType = NATTypeFullCone
	} else if mappedAddrs[0] == mappedAddrs[3] {
		nType = NATTypeRestrictedCone
	} else {
		nType = NATTypePortRestrictedCone
	}

	return &NATDetail{
		Addr:    mappedAddrs[0],
		NATType: nType,
		Token:   token,
	}, nil
}

func (r Resolver) test(stunServer string, changeIp bool, changePort bool) (string, error) {
	toAddr, err := net.ResolveUDPAddr("udp4", stunServer)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %s", stunServer, err)
	}
	msg, err := buildMsg(changeIp, changePort)
	if err != nil {
		return "", fmt.Errorf("failed to build STUN message: %s", err)
	}
	res, err := r.client.PerformTransaction(msg, toAddr, false)
	if err != nil {
		return "", fmt.Errorf("failed to perform transaction: %s", err)
	}

	var mappedAddr stun.XORMappedAddress
	if err = mappedAddr.GetFrom(res.Msg); err != nil {
		return "", fmt.Errorf("failed to get MAPPED-ADDRESS: %s", err)
	}
	return mappedAddr.String(), nil
}

func (r Resolver) Close() {
	r.client.Close()
}

func NewResolver(conn net.PacketConn) (r *Resolver, err error) {
	cfg := &turn.ClientConfig{
		Conn:          conn,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		RTO:           time.Second,
	}
	client, err := turn.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	err = client.Listen()
	if err != nil {
		return nil, err
	}
	return &Resolver{
		conn:   conn,
		client: client,
	}, nil
}

func buildMsg(changeIp bool, changePort bool) (*stun.Message, error) {
	attrs := []stun.Setter{
		stun.TransactionID,
		stun.BindingRequest,
	}
	msg, err := stun.Build(attrs...)
	if err != nil {
		return nil, fmt.Errorf("failed to build STUN message: %s", err)
	}
	if !changePort && !changeIp {
		return msg, nil
	}

	var attr uint32

	if changeIp {
		attr |= 0x4 // changeIp
	}

	if changePort {
		attr |= 0x2 // changePort
	}

	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, attr)
	msg.Add(stun.AttrChangeRequest, bytes)
	return msg, nil
}

func GenerateToken() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
