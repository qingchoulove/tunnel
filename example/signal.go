package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"tunnel"

	"github.com/gorilla/websocket"
)

type MockSignal struct {
	reader io.Reader
	writer io.Writer
}

func NewMockSignal() tunnel.Signal {
	return &MockSignal{
		reader: os.Stdin,
		writer: os.Stdout,
	}
}

func (s *MockSignal) SendSignal(detail *tunnel.NATDetail) error {
	fmt.Printf("Send local nat detail:\n")
	w := bufio.NewWriter(s.writer)
	bytes, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	_, err = w.Write(bytes)
	if err != nil {
		return err
	}
	err = w.Flush()
	if err != nil {
		return err
	}
	return nil
}

func (s *MockSignal) ReadSignal() (*tunnel.NATDetail, error) {
	fmt.Printf("Please input remote nat detail: \n")
	r := bufio.NewReader(s.reader)
	var in []byte
	for {
		var err error
		in, err = r.ReadBytes('\n')
		if err != io.EOF {
			if err != nil {
				return nil, err
			}
		}
		if len(in) > 0 {
			break
		}
	}
	var detail tunnel.NATDetail
	err := json.Unmarshal(in, &detail)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

type request struct {
	Token   string `json:"token"`
	Payload string `json:"payload"`
}

type WebsocketSignal struct {
	conn   *websocket.Conn
	c      chan *tunnel.NATDetail
	server string
	ctx    context.Context
}

func (w *WebsocketSignal) SendSignal(detail *tunnel.NATDetail) error {
	payload, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	req := &request{
		Token:   detail.Token,
		Payload: string(payload),
	}
	return w.conn.WriteJSON(req)
}

func (w *WebsocketSignal) ReadSignal() (*tunnel.NATDetail, error) {
	select {
	case <-w.ctx.Done():
		return nil, w.ctx.Err()
	case natDetail := <-w.c:
		return natDetail, nil
	}
}

func NewWebsocketSignal(ctx context.Context, server string) (tunnel.Signal, error) {
	conn, _, err := websocket.DefaultDialer.Dial(server, nil)
	if err != nil {
		return nil, err
	}

	w := &WebsocketSignal{
		conn:   conn,
		c:      make(chan *tunnel.NATDetail),
		server: server,
		ctx:    ctx,
	}
	go w.handle()
	return w, nil
}

func (w *WebsocketSignal) handle() {
	for {
		messageType, message, err := w.conn.ReadMessage()
		if err != nil {
			fmt.Printf("WebsocketSignal read: %s\n", err)
			break
		}
		if messageType != websocket.TextMessage {
			fmt.Printf("WebsocketSignal message type: %d\n", messageType)
			continue
		}
		var natDetail tunnel.NATDetail
		err = json.Unmarshal(message, &natDetail)
		if err != nil {
			fmt.Printf("WebsocketSignal unmarshal: %s\n", err)
			continue
		}
		w.c <- &natDetail
	}
}

// CloudflareSignal uses a Cloudflare Worker as the signaling server.
// The room token is shared out-of-band between server and client.
// role must be "server" or "client".
type CloudflareSignal struct {
	workerURL string
	room      string
	role      string
	ctx       context.Context
}

func NewCloudflareSignal(ctx context.Context, workerURL, room, role string) tunnel.Signal {
	return &CloudflareSignal{
		workerURL: workerURL,
		room:      room,
		role:      role,
		ctx:       ctx,
	}
}

func (c *CloudflareSignal) SendSignal(detail *tunnel.NATDetail) error {
	body, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/signal/%s/%s", c.workerURL, c.room, c.role)
	req, err := http.NewRequestWithContext(c.ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("worker error %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func (c *CloudflareSignal) ReadSignal() (*tunnel.NATDetail, error) {
	url := fmt.Sprintf("%s/signal/%s/%s", c.workerURL, c.room, c.role)
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("worker error %d: %s", resp.StatusCode, msg)
	}
	var detail tunnel.NATDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	return &detail, nil
}
