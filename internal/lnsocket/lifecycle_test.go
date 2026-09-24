package lnsocket

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

func TestReadLoopAfterClose(t *testing.T) {
	client := &LNSocket{}
	client.readLoop()
	if err := client.replyPong([]byte{0, 0, 0, 0}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("pong after close: %v", err)
	}
}

func TestHandshakeDeadlineAndCancellation(t *testing.T) {
	for _, cancelOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancellation"}[cancelOnly], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					accepted <- conn
				}
			}()
			key, err := btcec.NewPrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			// Cold elliptic-curve initialization can exceed a short deadline under
			// the race detector. Finish key setup before timing the handshake.
			pubkey := hex.EncodeToString(key.PubKey().SerializeCompressed())
			client := &LNSocket{}
			client.GenKey()
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if cancelOnly {
				cancel()
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- client.ConnectContext(ctx, listener.Addr().String(), pubkey)
			}()
			select {
			case conn := <-accepted:
				defer conn.Close()
			case err := <-result:
				t.Fatalf("connection failed before the stalled handshake: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("connection not accepted")
			}
			if cancelOnly {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("stalled handshake succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stalled handshake ignored context")
			}
		})
	}
}

func TestCloseDrainsReaderBeforeReuse(t *testing.T) {
	client := &LNSocket{}
	for i := 0; i < 20; i++ {
		left, right := net.Pipe()
		client.Conn = left
		client.transport = &transport{conn: left}
		client.pending = make(map[uint64]chan rpcResult)
		client.done = make(chan struct{})
		client.readErr = nil
		client.closeOnce = sync.Once{}
		readerDone := make(chan struct{})
		client.readerDone = readerDone
		go func() { defer close(readerDone); client.readLoop() }()
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
		right.Close()
		select {
		case <-readerDone:
		default:
			t.Fatal("close left reader running")
		}
		if _, err := client.RpcContext(context.Background(), "rune", "bkpr-listbalances", "[]"); !errors.Is(err, ErrNotConnected) {
			t.Fatalf("RPC after close: %v", err)
		}
	}
}

func TestInterruptedConnectionReleasesPendingRPC(t *testing.T) {
	left, right := net.Pipe()
	client := &LNSocket{Conn: left, transport: &transport{conn: left}, pending: make(map[uint64]chan rpcResult), done: make(chan struct{}), readerDone: make(chan struct{})}
	server := &transport{conn: right}
	defer right.Close()
	defer client.Close()
	go func() { defer close(client.readerDone); client.readLoop() }()
	result := make(chan error, 1)
	go func() {
		_, err := client.RpcContext(context.Background(), "test-rune", "bkpr-listbalances", "[]")
		result <- err
	}()
	_ = right.SetDeadline(time.Now().Add(time.Second))
	if _, err := server.readMessage(); err != nil {
		t.Fatal(err)
	}
	right.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("interrupted RPC succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupted connection left RPC waiting")
	}
}
