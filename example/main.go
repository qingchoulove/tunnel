package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"tunnel"
)

var (
	mode         = flag.String("mode", "client", "usage mode")
	signalServer = flag.String("signal", "ws://127.0.0.1:10086/ws", "signal server")
)

func main() {
	flag.Parse()

	ctx, cancelFunc := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	go func() {
		<-c
		cancelFunc()
	}()

	ws, err := NewWebsocketSignal(ctx, *signalServer)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	t, err := tunnel.NewTunnel(ctx, ws)
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
