package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"tunnel"
)

var (
	name       = flag.String("name", "peer", "display name in chat")
	signalType = flag.String("signal", "mock", "signal type: mock or cloudflare")
	workerURL  = flag.String("worker", "", "Cloudflare Worker URL (required when -signal=cloudflare)")
	room       = flag.String("room", "", "room token; auto-generated and printed if empty (first peer)")
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

	s, err := newSignal(ctx)
	if err != nil {
		fmt.Printf("signal error: %s\n", err)
		return
	}

	t, err := tunnel.NewTunnel(ctx, s)
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}
	if err = t.Connect(); err != nil {
		fmt.Printf("Error: %s\n", err)
		return
	}

	peer, err := t.ConnectHTTP2()
	if err != nil {
		fmt.Printf("ConnectHTTP2 error: %s\n", err)
		return
	}

	runChat(ctx, peer)
}

func newSignal(ctx context.Context) (tunnel.Signal, error) {
	switch *signalType {
	case "cloudflare":
		if *workerURL == "" {
			return nil, fmt.Errorf("-worker is required for cloudflare signal")
		}
		roomToken := *room
		if roomToken == "" {
			b := make([]byte, 3)
			if _, err := rand.Read(b); err != nil {
				return nil, err
			}
			roomToken = hex.EncodeToString(b)
			fmt.Printf("Room token: %s\n", roomToken)
			fmt.Printf("Start peer with: -signal=cloudflare -worker=%s -room=%s\n", *workerURL, roomToken)
			// first peer uses role "server" in the KV key scheme
			return NewCloudflareSignal(ctx, *workerURL, roomToken, "server"), nil
		}
		// second peer
		return NewCloudflareSignal(ctx, *workerURL, roomToken, "client"), nil
	default:
		return NewMockSignal(), nil
	}
}

// runChat wires up a symmetric chat: both peers can send and receive messages.
func runChat(ctx context.Context, peer *tunnel.Peer) {
	displayName := *name

	// outbox fans out messages to all connected remote peers
	type subscriber struct {
		ch   chan string
		done <-chan struct{}
	}
	subCh := make(chan subscriber, 8)

	go func() {
		var subs []subscriber
		inbox := make(chan string, 32)

		// stdin → inbox
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := scanner.Text()
				if line != "" {
					inbox <- fmt.Sprintf("[%s] %s", displayName, line)
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case sub := <-subCh:
				subs = append(subs, sub)
			case msg := <-inbox:
				fmt.Println(msg)
				alive := subs[:0]
				for _, s := range subs {
					select {
					case <-s.done:
					default:
						select {
						case s.ch <- msg:
						default:
						}
						alive = append(alive, s)
					}
				}
				subs = alive
			}
		}
	}()

	// register /chat handler so the remote peer can send messages to us
	peer.Handle("/chat", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		outCh := make(chan string, 16)
		subCh <- subscriber{ch: outCh, done: r.Context().Done()}

		// remote → local stdout
		go func() {
			scanner := bufio.NewScanner(r.Body)
			for scanner.Scan() {
				if line := scanner.Text(); line != "" {
					fmt.Println(line)
				}
			}
		}()

		// local outbox → remote
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-outCh:
				fmt.Fprintln(w, msg)
				flusher.Flush()
			}
		}
	})

	// open a /chat stream toward the remote peer
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://tunnel/chat", pr)
	if err != nil {
		fmt.Printf("request error: %s\n", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain")

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			msg := fmt.Sprintf("[%s] %s\n", displayName, line)
			if _, err := pw.Write([]byte(msg)); err != nil {
				break
			}
		}
		pw.Close()
	}()

	resp, err := peer.Client.Do(req)
	if err != nil {
		fmt.Printf("chat error: %s\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Connected. Type messages and press Enter.")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
}
