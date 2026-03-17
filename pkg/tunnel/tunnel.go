package tunnel

import (
	"io"
	"net"
	"sync"

	"github.com/cltx/clienthub/pkg/crypto"
	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
)

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

	frame := make([]byte, 4+len(encrypted))
	frame[0] = byte(len(encrypted) >> 24)
	frame[1] = byte(len(encrypted) >> 16)
	frame[2] = byte(len(encrypted) >> 8)
	frame[3] = byte(len(encrypted))
	copy(frame[4:], encrypted)

	cw.mu.Lock()
	defer cw.mu.Unlock()
	_, err = cw.conn.Write(frame)
	return err
}

func ReadEncryptedMessage(r io.Reader, cipher *crypto.Cipher) (*proto.Message, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}

	frameLen := int(lenBuf[0])<<24 | int(lenBuf[1])<<16 | int(lenBuf[2])<<8 | int(lenBuf[3])
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
