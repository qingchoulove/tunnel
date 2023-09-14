package tunnel

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
)

type magic uint32
type MessageType byte

const magicHeader magic = 0xE1E1

const (
	MessageTypeHandshake MessageType = iota + 1
	MessageTypePing
	MessageTypeData
)

type Message struct {
	header  magic
	version byte
	token   string
	mType   MessageType
	len     uint16
	payload []byte
}

func NewHandshakeMessage(token string) *Message {
	return &Message{
		header:  magicHeader,
		version: 1,
		token:   token,
		mType:   MessageTypeHandshake,
		len:     0,
	}
}

func NewPingMessage(token string) *Message {
	return &Message{
		header:  magicHeader,
		version: 1,
		token:   token,
		mType:   MessageTypePing,
		len:     0,
	}
}

func NewDataMessage(token string, data []byte) *Message {
	if len(data) > 1000 {
		panic("data length too long")
	}
	return &Message{
		header:  magicHeader,
		version: 1,
		token:   token,
		mType:   MessageTypeData,
		len:     uint16(len(data)),
		payload: data,
	}
}

func (m *Message) Marshal() ([]byte, error) {
	token, err := hex.DecodeString(m.token)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	headerBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(headerBytes, uint32(m.header))
	buf.Write(headerBytes)
	buf.WriteByte(m.version)
	buf.Write(token[:4])
	buf.WriteByte(byte(m.mType))
	lenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(lenBytes, m.len)
	buf.Write(lenBytes)
	if m.len > 0 {
		buf.Write(m.payload)
	}
	hash := crc32.ChecksumIEEE(buf.Bytes())
	hashBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(hashBytes, hash)
	buf.Write(hashBytes)
	return buf.Bytes(), nil
}

func (m *Message) String() string {
	return fmt.Sprintf("header: %d, version: %d, token: %s, type: %d, len: %d, payload: %s",
		m.header, m.version, m.token, m.mType, m.len, string(m.payload))
}

func UnmarshalMessage(bytes []byte) (*Message, error) {
	if len(bytes) < 12 {
		return nil, fmt.Errorf("failed to unmarshal message, bytes: %s", bytes)
	}
	// message format:
	// 4 bytes magic header
	// 1 byte version
	// 4 bytes token
	// 1 byte message type
	// 2 bytes length
	// 0-n bytes payload
	// 4 bytes hash
	cursor := 0
	header := magic(binary.LittleEndian.Uint32(bytes[cursor : cursor+4]))
	if header != magicHeader {
		return nil, fmt.Errorf("magic header not match, data invalid")
	}
	cursor = cursor + 4
	version := bytes[cursor]
	cursor = cursor + 1
	token := hex.EncodeToString(bytes[cursor : cursor+4])
	cursor = cursor + 4
	mType := MessageType(bytes[cursor])
	cursor = cursor + 1
	dataLen := binary.LittleEndian.Uint16(bytes[cursor : cursor+2])
	cursor = cursor + 2
	var payload []byte
	if dataLen > 0 {
		payload = bytes[cursor : cursor+int(dataLen)]
		cursor = cursor + int(dataLen)
	}
	reCalHash := crc32.ChecksumIEEE(bytes[:cursor])
	hash := binary.LittleEndian.Uint32(bytes[cursor : cursor+4])
	if reCalHash != hash {
		return nil, fmt.Errorf("hash not match, data invalid")
	}
	msg := &Message{
		header:  header,
		version: version,
		token:   token,
		mType:   mType,
		len:     dataLen,
	}
	if dataLen > 0 {
		msg.payload = payload
	}
	return msg, nil
}
