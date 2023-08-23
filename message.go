package tunnel

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

type magic uint32
type MessageType byte

const magicHeader magic = 0xE1E1

const (
	MessagePing MessageType = iota + 1
	MessagePong
)

type Message struct {
	header  magic
	version byte
	token   string
	mType   MessageType
	hash    []byte
}

func NewPingMessage(token string) *Message {
	return &Message{
		magicHeader,
		1,
		token,
		MessagePing,
		make([]byte, 4),
	}
}

func NewPongMessage(token string) *Message {
	return &Message{
		magicHeader,
		1,
		token,
		MessagePong,
		make([]byte, 4),
	}
}

func (m *Message) Marshal() ([]byte, error) {
	token, err := hex.DecodeString(m.token)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, m.header)
	buf.WriteByte(m.version)
	buf.Write(token[:4])
	buf.WriteByte(byte(m.mType))
	buf.Write(m.hash)
	return buf.Bytes(), nil
}

func Unmarshal(bytes []byte) (*Message, error) {
	if len(bytes) < 12 {
		return nil, fmt.Errorf("failed to unmarshal message, bytes: %s", bytes)
	}
	// 4 bytes magic header
	// 1 byte version
	// 4 bytes token
	// 1 byte message type
	// 4 bytes hash
	header := magic(binary.BigEndian.Uint32(bytes[0:4]))
	if header != magicHeader {
		return nil, fmt.Errorf("")
	}
	version := bytes[4]
	token := hex.EncodeToString(bytes[5:9])
	mType := MessageType(bytes[9])
	hash := bytes[9:12]
	return &Message{
		header,
		version,
		token,
		mType,
		hash,
	}, nil
}
