package lnsocket

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

const (
	COMMANDO_CMD             = 0x4c4f
	COMMANDO_REPLY_CONTINUES = 0x594b
	COMMANDO_REPLY_TERM      = 0x594d
)

var ErrNotConnected = errors.New("lnsocket is not connected and initialized")

type rpcResult struct {
	value string
	err   error
}

// LNSocket is a concurrent Commando client over Lightning's native BOLT 8 transport.
type LNSocket struct {
	Conn       net.Conn
	mu         sync.Mutex
	private    *btcec.PrivateKey
	transport  *transport
	pending    map[uint64]chan rpcResult
	done       chan struct{}
	readerDone chan struct{}
	closeOnce  sync.Once
	readErr    error
}

func (ln *LNSocket) GenKey() {
	key, err := btcec.NewPrivateKey()
	if err != nil {
		panic(fmt.Sprintf("generate secp256k1 key: %v", err))
	}
	ln.mu.Lock()
	ln.private = key
	ln.mu.Unlock()
}
func (ln *LNSocket) Connect(hostname, pubkey string) error {
	return ln.ConnectContext(context.Background(), hostname, pubkey)
}

func (ln *LNSocket) ConnectContext(ctx context.Context, hostname, pubkey string) error {
	remoteBytes, err := hex.DecodeString(pubkey)
	if err != nil {
		return fmt.Errorf("decode node public key: %w", err)
	}
	remote, err := btcec.ParsePubKey(remoteBytes)
	if err != nil {
		return fmt.Errorf("parse node public key: %w", err)
	}
	if err := ln.Close(); err != nil {
		return err
	}
	ln.mu.Lock()
	local := ln.private
	ln.mu.Unlock()
	if local == nil {
		local, err = btcec.NewPrivateKey()
		if err != nil {
			return fmt.Errorf("generate local key: %w", err)
		}
		ln.mu.Lock()
		ln.private = local
		ln.mu.Unlock()
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", hostname)
	if err != nil {
		return err
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	handshakeDeadline := time.Now().Add(10 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}
	if err := conn.SetDeadline(handshakeDeadline); err != nil {
		_ = conn.Close()
		return err
	}
	ephemeral, err := btcec.NewPrivateKey()
	if err != nil {
		_ = conn.Close()
		return err
	}
	t, err := initiatorHandshake(conn, local, remote, ephemeral)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if !stopCancel() || ctx.Err() != nil {
		_ = conn.Close()
		return ctx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return err
	}
	ln.mu.Lock()
	ln.Conn, ln.transport, ln.pending, ln.done, ln.readErr = conn, t, make(map[uint64]chan rpcResult), make(chan struct{}), nil
	ln.closeOnce = sync.Once{}
	ln.mu.Unlock()
	return nil
}

func (ln *LNSocket) PerformInit() error {
	return ln.PerformInitContext(context.Background())
}

func (ln *LNSocket) PerformInitContext(ctx context.Context) error {
	ln.mu.Lock()
	t := ln.transport
	conn := ln.Conn
	ln.mu.Unlock()
	if t == nil {
		return ErrNotConnected
	}
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	// A minimal BOLT #1 init contains two empty feature vectors: the legacy
	// globalfeatures field followed by the local features field.
	if err := t.writeMessage([]byte{0, 16, 0, 0, 0, 0}); err != nil {
		return err
	}
	for {
		message, err := t.readMessage()
		if err != nil {
			return fmt.Errorf("read peer init: %w", err)
		}
		if len(message) < 2 {
			return errors.New("peer sent a truncated Lightning message")
		}
		switch binary.BigEndian.Uint16(message[:2]) {
		case 16:
			if len(message) < 6 {
				return errors.New("truncated init feature vectors")
			}
			globals := int(binary.BigEndian.Uint16(message[2:4]))
			if 4+globals+2 > len(message) {
				return errors.New("invalid init global features")
			}
			locals := int(binary.BigEndian.Uint16(message[4+globals : 6+globals]))
			if 6+globals+locals > len(message) {
				return errors.New("invalid init local features")
			}
			ln.mu.Lock()
			ln.readerDone = make(chan struct{})
			readerDone := ln.readerDone
			ln.mu.Unlock()
			go func() { defer close(readerDone); ln.readLoop() }()
			return nil
		case 18:
			if err := ln.replyPong(message[2:]); err != nil {
				return err
			}
		}
	}
}

func (ln *LNSocket) ConnectAndInit(hostname, pubkey string) error {
	return ln.ConnectAndInitContext(context.Background(), hostname, pubkey)
}

func (ln *LNSocket) ConnectAndInitContext(ctx context.Context, hostname, pubkey string) error {
	if err := ln.ConnectContext(ctx, hostname, pubkey); err != nil {
		return err
	}
	if err := ln.PerformInitContext(ctx); err != nil {
		_ = ln.Close()
		return err
	}
	return nil
}
func (ln *LNSocket) Rpc(token, method, params string) (string, error) {
	return ln.RpcContext(context.Background(), token, method, params)
}

func (ln *LNSocket) RpcContext(ctx context.Context, token, method, params string) (string, error) {
	rawParams := json.RawMessage(params)
	if !json.Valid(rawParams) {
		return "", errors.New("Commando params must be valid JSON")
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Rune    string          `json:"rune"`
		ID      string          `json:"id"`
		JSONRPC string          `json:"jsonrpc"`
	}{method, rawParams, token, fmt.Sprintf("lnsocket:%016x", id), "2.0"})
	if err != nil {
		return "", err
	}
	result := make(chan rpcResult, 1)
	ln.mu.Lock()
	if ln.transport == nil || ln.done == nil {
		ln.mu.Unlock()
		return "", ErrNotConnected
	}
	if _, exists := ln.pending[id]; exists {
		ln.mu.Unlock()
		return "", errors.New("random Commando request ID collision")
	}
	ln.pending[id] = result
	t, done := ln.transport, ln.done
	ln.mu.Unlock()
	message := make([]byte, 10+len(payload))
	binary.BigEndian.PutUint16(message[:2], COMMANDO_CMD)
	binary.BigEndian.PutUint64(message[2:10], id)
	copy(message[10:], payload)
	if err := t.writeMessage(message); err != nil {
		ln.removePending(id)
		return "", err
	}
	select {
	case response := <-result:
		return response.value, response.err
	case <-ctx.Done():
		ln.removePending(id)
		return "", ctx.Err()
	case <-done:
		ln.mu.Lock()
		err := ln.readErr
		ln.mu.Unlock()
		if err == nil {
			err = net.ErrClosed
		}
		return "", err
	}
}

func (ln *LNSocket) readLoop() {
	chunks := make(map[uint64][]byte)
	for {
		ln.mu.Lock()
		t := ln.transport
		ln.mu.Unlock()
		if t == nil {
			return
		}
		message, err := t.readMessage()
		if err != nil {
			ln.fail(err)
			return
		}
		if len(message) < 2 {
			ln.fail(errors.New("peer sent a truncated Lightning message"))
			return
		}
		typ := binary.BigEndian.Uint16(message[:2])
		switch typ {
		case COMMANDO_REPLY_CONTINUES, COMMANDO_REPLY_TERM:
			if len(message) < 10 {
				ln.fail(errors.New("peer sent a truncated Commando response"))
				return
			}
			id := binary.BigEndian.Uint64(message[2:10])
			if !ln.isPending(id) {
				delete(chunks, id)
				continue
			}
			chunks[id] = append(chunks[id], message[10:]...)
			if len(chunks[id]) > 16<<20 {
				ln.completeError(id, errors.New("Commando response exceeds 16 MiB"))
				delete(chunks, id)
				continue
			}
			if typ == COMMANDO_REPLY_TERM {
				ln.complete(id, string(chunks[id]))
				delete(chunks, id)
			}
		case 18:
			if err := ln.replyPong(message[2:]); err != nil {
				ln.fail(err)
				return
			}
		}
	}
}

func (ln *LNSocket) isPending(id uint64) bool {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	_, ok := ln.pending[id]
	return ok
}

func (ln *LNSocket) replyPong(payload []byte) error {
	if len(payload) < 4 {
		return errors.New("peer sent a truncated ping")
	}
	ignored := int(binary.BigEndian.Uint16(payload[2:4]))
	if ignored != len(payload)-4 {
		return errors.New("invalid ping length")
	}
	size := int(binary.BigEndian.Uint16(payload[:2]))
	if size >= 65532 {
		return nil
	}
	message := make([]byte, 4+size)
	binary.BigEndian.PutUint16(message[:2], 19)
	binary.BigEndian.PutUint16(message[2:4], uint16(size))
	ln.mu.Lock()
	t := ln.transport
	ln.mu.Unlock()
	if t == nil {
		return ErrNotConnected
	}
	return t.writeMessage(message)
}
func (ln *LNSocket) complete(id uint64, value string) {
	ln.mu.Lock()
	result := ln.pending[id]
	delete(ln.pending, id)
	ln.mu.Unlock()
	if result != nil {
		result <- rpcResult{value: value}
	}
}

func (ln *LNSocket) completeError(id uint64, err error) {
	ln.mu.Lock()
	result := ln.pending[id]
	delete(ln.pending, id)
	ln.mu.Unlock()
	if result != nil {
		result <- rpcResult{err: err}
	}
}
func (ln *LNSocket) removePending(id uint64) { ln.mu.Lock(); delete(ln.pending, id); ln.mu.Unlock() }
func (ln *LNSocket) fail(err error) {
	ln.mu.Lock()
	if ln.readErr == nil {
		ln.readErr = err
	}
	pending := ln.pending
	ln.pending = make(map[uint64]chan rpcResult)
	done := ln.done
	ln.mu.Unlock()
	for _, result := range pending {
		result <- rpcResult{err: err}
	}
	ln.closeOnce.Do(func() {
		if done != nil {
			close(done)
		}
	})
}
func (ln *LNSocket) Close() error {
	ln.mu.Lock()
	conn := ln.Conn
	readerDone := ln.readerDone
	ln.mu.Unlock()
	if conn == nil {
		return nil
	}
	err := conn.Close()
	ln.fail(net.ErrClosed)
	if readerDone != nil {
		<-readerDone
	}
	ln.mu.Lock()
	ln.Conn, ln.transport, ln.readerDone = nil, nil, nil
	ln.mu.Unlock()
	return err
}
func (ln *LNSocket) Disconnect() { _ = ln.Close() }
func (ln *LNSocket) Recv() (uint16, []byte, error) {
	ln.mu.Lock()
	t := ln.transport
	ln.mu.Unlock()
	if t == nil {
		return 0, nil, ErrNotConnected
	}
	message, err := t.readMessage()
	if err != nil {
		return 0, nil, err
	}
	if len(message) < 2 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return binary.BigEndian.Uint16(message[:2]), message[2:], nil
}
func ParseMsgType(message []byte) uint16 {
	if len(message) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(message[:2])
}
func randomID() (uint64, error) {
	var value [8]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(value[:]), nil
}
