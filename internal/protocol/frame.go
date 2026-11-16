package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

// MaxFrame is the largest frame (including JSON payload) accepted in either
// direction. A welcome can contain up to 100 chat messages, so allow a little
// headroom
const MaxFrame = 256 << 10

var ErrFrameTooLarge = errors.New("frame too large")

// reads one length-prefixed frame.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}

	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrame {
		return nil, ErrFrameTooLarge
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// writes one length-prefixed frame.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxFrame {
		return ErrFrameTooLarge
	}

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
