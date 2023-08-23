package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"tunnel"
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

func main() {

	signal := NewMockSignal()

	t, err := tunnel.NewTunnel(signal)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	err = t.Connect()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}
	t.Ping()

}
