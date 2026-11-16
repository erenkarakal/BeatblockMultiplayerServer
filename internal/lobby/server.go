package lobby

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"bb-mp-server/internal/protocol"
)

// wires a Manager to a TCP listener
type Server struct {
	manager   *Manager
	closeOnce sync.Once

	mu sync.Mutex
	ln net.Listener
	wg sync.WaitGroup
}

func NewServer(opts Options) *Server {
	return &Server{manager: NewManager(opts)}
}

// returns the lobby and connection registry
func (s *Server) Manager() *Manager {
	return s.manager
}

// accepts connections until the listener is closed
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		client := s.manager.attach(conn)
		if client == nil {
			rejectConnection(conn, "SERVER_FULL", "The server is full.")
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			client.run()
		}()
	}
}

func rejectConnection(conn net.Conn, code, message string) {
	payload, err := json.Marshal(protocol.ServerMessage{
		Type:    "error",
		Code:    code,
		Message: message,
	})
	if err == nil {
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		_ = protocol.WriteFrame(conn, payload)
	}

	// half-close and drain briefly so the error frame survives the peer's
	// pending hello instead of being discarded by a reset
	go func() {
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _ = io.Copy(io.Discard, conn)
		_ = conn.Close()
	}()
}

// closes the listener and all connections
func (s *Server) Shutdown() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		ln := s.ln
		s.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}

		s.manager.Shutdown()
		s.wg.Wait()
	})
}
