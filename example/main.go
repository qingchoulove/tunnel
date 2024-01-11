package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"tunnel"
)

var (
	mode = flag.String("mode", "client", "usage mode")
)

func main() {
	flag.Parse()

	ctx, cancelFunc := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		cancelFunc()
	}()

	s := NewMockSignal()

	t, err := tunnel.NewTunnel(ctx, s)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}
	err = t.Connect()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}
	t.Test(*mode)
}
