package lnsocket

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestInitiatorHandshakeBOLT8Vector(t *testing.T) {
	local, _ := btcec.PrivKeyFromBytes(mustHex(t, "1111111111111111111111111111111111111111111111111111111111111111"))
	ephemeral, _ := btcec.PrivKeyFromBytes(mustHex(t, "1212121212121212121212121212121212121212121212121212121212121212"))
	remote, err := btcec.ParsePubKey(mustHex(t, "028d7500dd4c12685d1f568b4c2b5048e8534b873319f3a8daa612b469132ec7f7"))
	if err != nil {
		t.Fatal(err)
	}
	wantOne := mustHex(t, "00036360e856310ce5d294e8be33fc807077dc56ac80d95d9cd4ddbd21325eff73f70df6086551151f58b8afe6c195782c6a")
	actTwo := mustHex(t, "0002466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f276e2470b93aac583c9ef6eafca3f730ae")
	wantThree := mustHex(t, "00b9e3a702e93e3a9948c2ed6e5fd7590a6e1c3a0344cfc9d5b57357049aa22355361aa02e55a8fc28fef5bd6d71ad0c38228dc68b1c466263b47fdf31e560e139ba")
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverErr := make(chan error, 1)
	go func() {
		one := make([]byte, 50)
		if _, err := io.ReadFull(server, one); err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(one, wantOne) {
			serverErr <- &mismatchError{"act one", one, wantOne}
			return
		}
		if err := writeFull(server, actTwo); err != nil {
			serverErr <- err
			return
		}
		three := make([]byte, 66)
		if _, err := io.ReadFull(server, three); err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(three, wantThree) {
			serverErr <- &mismatchError{"act three", three, wantThree}
			return
		}
		serverErr <- nil
	}()
	transport, err := initiatorHandshake(client, local, remote, ephemeral)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	wantSend := mustHex(t, "969ab31b4d288cedf6218839b27a3e2140827047f2c0f01bf5c04435d43511a9")
	wantReceive := mustHex(t, "bb9020b8965f4df047e07f955f3c4b88418984aadc5cdb35096b9ea8fa5c3442")
	if !bytes.Equal(transport.send.key[:], wantSend) {
		t.Fatalf("send key = %x", transport.send.key)
	}
	if !bytes.Equal(transport.receive.key[:], wantReceive) {
		t.Fatalf("receive key = %x", transport.receive.key)
	}
}

type mismatchError struct {
	name      string
	got, want []byte
}

func (e *mismatchError) Error() string { return e.name + " did not match the BOLT 8 test vector" }

func TestTransportEncryptionBOLT8VectorAndRotation(t *testing.T) {
	chain := array32(mustHex(t, "919219dbb2920afa8db80f9a51787a840bcf111ed8d588caf9ab4be716e42b01"))
	key := array32(mustHex(t, "969ab31b4d288cedf6218839b27a3e2140827047f2c0f01bf5c04435d43511a9"))
	state := cipherState{key: key, chain: chain}
	wants := map[int][]byte{
		0:    mustHex(t, "cf2b30ddf0cf3f80e7c35a6e6730b59fe802473180f396d88a8fb0db8cbcf25d2f214cf9ea1d95"),
		1:    mustHex(t, "72887022101f0b6753e0c7de21657d35a4cb2a1f5cde2650528bbc8f837d0f0d7ad833b1a256a1"),
		500:  mustHex(t, "178cb9d7387190fa34db9c2d50027d21793c9bc2d40b1e14dcf30ebeeeb220f48364f7a4c68bf8"),
		501:  mustHex(t, "1b186c57d44eb6de4c057c49940d79bb838a145cb528d6e8fd26dbe50a60ca2c104b56b60e45bd"),
		1000: mustHex(t, "4a2f3cc3b5e78ddb83dcb426d9863d9d9a723b0337c89dd0b005d89f8d3c05c52b76b29b740f09"),
	}
	for i := 0; i <= 1000; i++ {
		length, err := state.crypt(nil, []byte{0, 5}, false)
		if err != nil {
			t.Fatal(err)
		}
		message, err := state.crypt(nil, []byte("hello"), false)
		if err != nil {
			t.Fatal(err)
		}
		if want := wants[i]; want != nil && !bytes.Equal(append(length, message...), want) {
			t.Fatalf("message %d did not match BOLT 8 vector", i)
		}
	}
}

func TestPerformInitSendsBothFeatureVectors(t *testing.T) {
	left, right := net.Pipe()
	chain := array32(bytes.Repeat([]byte{1}, 32))
	a := array32(bytes.Repeat([]byte{2}, 32))
	b := array32(bytes.Repeat([]byte{3}, 32))
	clientTransport := &transport{conn: left, send: cipherState{key: a, chain: chain}, receive: cipherState{key: b, chain: chain}}
	serverTransport := &transport{conn: right, send: cipherState{key: b, chain: chain}, receive: cipherState{key: a, chain: chain}}
	client := &LNSocket{Conn: left, transport: clientTransport, pending: make(map[uint64]chan rpcResult), done: make(chan struct{})}
	defer client.Close()
	defer right.Close()

	serverErr := make(chan error, 1)
	go func() {
		message, err := serverTransport.readMessage()
		if err != nil {
			serverErr <- err
			return
		}
		want := []byte{0, 16, 0, 0, 0, 0}
		if !bytes.Equal(message, want) {
			serverErr <- &mismatchError{"init message", message, want}
			return
		}
		serverErr <- serverTransport.writeMessage(want)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.PerformInitContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRPCResponsesAreCorrelated(t *testing.T) {
	left, right := net.Pipe()
	chain := array32(bytes.Repeat([]byte{1}, 32))
	a := array32(bytes.Repeat([]byte{2}, 32))
	b := array32(bytes.Repeat([]byte{3}, 32))
	clientTransport := &transport{conn: left, send: cipherState{key: a, chain: chain}, receive: cipherState{key: b, chain: chain}}
	serverTransport := &transport{conn: right, send: cipherState{key: b, chain: chain}, receive: cipherState{key: a, chain: chain}}
	client := &LNSocket{Conn: left, transport: clientTransport, pending: make(map[uint64]chan rpcResult), done: make(chan struct{})}
	go client.readLoop()
	defer client.Close()
	serverErr := make(chan error, 1)
	go func() {
		requests := make([][]byte, 2)
		for i := range requests {
			var err error
			requests[i], err = serverTransport.readMessage()
			if err != nil {
				serverErr <- err
				return
			}
		}
		for i := len(requests) - 1; i >= 0; i-- {
			response := make([]byte, 10)
			binary.BigEndian.PutUint16(response[:2], COMMANDO_REPLY_TERM)
			copy(response[2:10], requests[i][2:10])
			response = append(response, requests[i][10:]...)
			if err := serverTransport.writeMessage(response); err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}()
	var wg sync.WaitGroup
	results := make([]string, 2)
	for i, method := range []string{"first", "second"} {
		wg.Add(1)
		go func(i int, method string) {
			defer wg.Done()
			value, err := client.RpcContext(context.Background(), "rune", method, "[]")
			if err != nil {
				t.Errorf("RPC: %v", err)
				return
			}
			results[i] = value
		}(i, method)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent RPCs timed out")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	for i, method := range []string{"first", "second"} {
		if !bytes.Contains([]byte(results[i]), []byte(`"method":"`+method+`"`)) {
			t.Fatalf("response %d was mis-correlated: %s", i, results[i])
		}
	}
}

func array32(value []byte) [32]byte { var out [32]byte; copy(out[:], value); return out }
