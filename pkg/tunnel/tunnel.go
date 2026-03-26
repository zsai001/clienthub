package tunnel

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/cltx/clienthub/pkg/crypto"
	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
)

const readBufSize = 64 * 1024

type ConnWriter struct {
	mu     sync.Mutex
	conn   net.Conn
	cipher *crypto.Cipher
	logger *zap.Logger
}

func NewConnWriter(conn net.Conn, cipher *crypto.Cipher, logger *zap.Logger) *ConnWriter {
	return &ConnWriter{conn: conn, cipher: cipher, logger: logger}
}

func (cw *ConnWriter) WriteMessage(msg *proto.Message) error {
	raw := msg.Encode()
	encrypted, err := cw.cipher.Encrypt(raw)
	if err != nil {
		return err
	}

	// 4-byte big-endian length prefix + encrypted payload, written in one call.
	frame := make([]byte, 4+len(encrypted))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(encrypted)))
	copy(frame[4:], encrypted)

	cw.mu.Lock()
	defer cw.mu.Unlock()
	_, err = cw.conn.Write(frame)
	return err
}

// NewBufReader wraps a net.Conn in a bufio.Reader for efficient reads.
func NewBufReader(conn net.Conn) *bufio.Reader {
	return bufio.NewReaderSize(conn, readBufSize)
}

// ReadEncryptedMessage reads one framed encrypted message from r.
// r should be a *bufio.Reader wrapping the connection for best performance.
func ReadEncryptedMessage(r io.Reader, cipher *crypto.Cipher) (*proto.Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}

	frameLen := int(binary.BigEndian.Uint32(lenBuf[:]))
	if frameLen <= 0 || frameLen > proto.MaxPayloadSize+1024 {
		return nil, proto.ErrInvalidMessage
	}

	encrypted := make([]byte, frameLen)
	if _, err := io.ReadFull(r, encrypted); err != nil {
		return nil, err
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}

	return proto.ReadMessageFromBytes(decrypted)
}
