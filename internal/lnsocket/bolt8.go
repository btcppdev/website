package lnsocket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"golang.org/x/crypto/chacha20poly1305"
)

const maxLightningMessage = 65535

type cipherState struct {
	key, chain [32]byte
	nonce      uint64
}

func (s *cipherState) crypt(ad, input []byte, decrypt bool) ([]byte, error) {
	aead, err := chacha20poly1305.New(s.key[:])
	if err != nil {
		return nil, err
	}
	var nonce [chacha20poly1305.NonceSize]byte
	binary.LittleEndian.PutUint64(nonce[4:], s.nonce)
	var out []byte
	if decrypt {
		out, err = aead.Open(nil, nonce[:], input, ad)
	} else {
		out = aead.Seal(nil, nonce[:], input, ad)
	}
	if err != nil {
		return nil, err
	}
	s.nonce++
	if s.nonce == 1000 {
		first, second := hkdf2(s.chain[:], s.key[:])
		s.chain, s.key, s.nonce = first, second, 0
	}
	return out, nil
}

type transport struct {
	conn            net.Conn
	send, receive   cipherState
	writeMu, readMu sync.Mutex
}

func (t *transport) writeMessage(message []byte) error {
	if len(message) > maxLightningMessage {
		return fmt.Errorf("lightning message is %d bytes; maximum is %d", len(message), maxLightningMessage)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(message)))
	encSize, err := t.send.crypt(nil, size[:], false)
	if err != nil {
		return err
	}
	encMessage, err := t.send.crypt(nil, message, false)
	if err != nil {
		return err
	}
	return writeFull(t.conn, append(encSize, encMessage...))
}

func (t *transport) readMessage() ([]byte, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	encSize := make([]byte, 18)
	if _, err := io.ReadFull(t.conn, encSize); err != nil {
		return nil, err
	}
	size, err := t.receive.crypt(nil, encSize, true)
	if err != nil {
		return nil, fmt.Errorf("decrypt lightning message length: %w", err)
	}
	encMessage := make([]byte, int(binary.BigEndian.Uint16(size))+16)
	if _, err := io.ReadFull(t.conn, encMessage); err != nil {
		return nil, err
	}
	message, err := t.receive.crypt(nil, encMessage, true)
	if err != nil {
		return nil, fmt.Errorf("decrypt lightning message: %w", err)
	}
	return message, nil
}

func initiatorHandshake(conn net.Conn, local *btcec.PrivateKey, remote *btcec.PublicKey, ephemeral *btcec.PrivateKey) (*transport, error) {
	h := sha256.Sum256([]byte("Noise_XK_secp256k1_ChaChaPoly_SHA256"))
	chain := h
	h = hashConcat(h[:], []byte("lightning"))
	h = hashConcat(h[:], remote.SerializeCompressed())
	ePub := ephemeral.PubKey().SerializeCompressed()
	h = hashConcat(h[:], ePub)
	shared, err := ecdh(ephemeral, remote)
	if err != nil {
		return nil, err
	}
	chain, tempKey := hkdf2(chain[:], shared[:])
	tag, err := cryptOnce(tempKey, 0, h[:], nil, false)
	if err != nil {
		return nil, err
	}
	h = hashConcat(h[:], tag)
	actOne := append(append([]byte{0}, ePub...), tag...)
	if err := writeFull(conn, actOne); err != nil {
		return nil, fmt.Errorf("write BOLT 8 act one: %w", err)
	}
	actTwo := make([]byte, 50)
	if _, err := io.ReadFull(conn, actTwo); err != nil {
		return nil, fmt.Errorf("read BOLT 8 act two: %w", err)
	}
	if actTwo[0] != 0 {
		return nil, fmt.Errorf("unsupported BOLT 8 handshake version %d", actTwo[0])
	}
	re, err := btcec.ParsePubKey(actTwo[1:34])
	if err != nil {
		return nil, fmt.Errorf("parse responder ephemeral key: %w", err)
	}
	h = hashConcat(h[:], actTwo[1:34])
	shared, err = ecdh(ephemeral, re)
	if err != nil {
		return nil, err
	}
	chain, tempKey = hkdf2(chain[:], shared[:])
	if _, err := cryptOnce(tempKey, 0, h[:], actTwo[34:], true); err != nil {
		return nil, fmt.Errorf("authenticate BOLT 8 act two: %w", err)
	}
	h = hashConcat(h[:], actTwo[34:])
	cipherStatic, err := cryptOnce(tempKey, 1, h[:], local.PubKey().SerializeCompressed(), false)
	if err != nil {
		return nil, err
	}
	h = hashConcat(h[:], cipherStatic)
	shared, err = ecdh(local, re)
	if err != nil {
		return nil, err
	}
	chain, tempKey = hkdf2(chain[:], shared[:])
	finalTag, err := cryptOnce(tempKey, 0, h[:], nil, false)
	if err != nil {
		return nil, err
	}
	if err := writeFull(conn, append(append([]byte{0}, cipherStatic...), finalTag...)); err != nil {
		return nil, fmt.Errorf("write BOLT 8 act three: %w", err)
	}
	sendKey, receiveKey := hkdf2(chain[:], nil)
	return &transport{conn: conn, send: cipherState{key: sendKey, chain: chain}, receive: cipherState{key: receiveKey, chain: chain}}, nil
}

func ecdh(private *btcec.PrivateKey, public *btcec.PublicKey) ([32]byte, error) {
	var point, result btcec.JacobianPoint
	public.AsJacobian(&point)
	btcec.ScalarMultNonConst(&private.Key, &point, &result)
	if result.Z.IsZero() {
		return [32]byte{}, errors.New("invalid ECDH shared point")
	}
	result.ToAffine()
	return sha256.Sum256(btcec.NewPublicKey(&result.X, &result.Y).SerializeCompressed()), nil
}

func hkdf2(salt, input []byte) ([32]byte, [32]byte) {
	extract := hmac.New(sha256.New, salt)
	_, _ = extract.Write(input)
	prk := extract.Sum(nil)
	one := hmac.New(sha256.New, prk)
	_, _ = one.Write([]byte{1})
	firstBytes := one.Sum(nil)
	two := hmac.New(sha256.New, prk)
	_, _ = two.Write(firstBytes)
	_, _ = two.Write([]byte{2})
	secondBytes := two.Sum(nil)
	var first, second [32]byte
	copy(first[:], firstBytes)
	copy(second[:], secondBytes)
	return first, second
}

func cryptOnce(key [32]byte, nonce uint64, ad, input []byte, decrypt bool) ([]byte, error) {
	return (&cipherState{key: key, nonce: nonce}).crypt(ad, input, decrypt)
}
func hashConcat(parts ...[]byte) [32]byte {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[n:]
	}
	return nil
}
