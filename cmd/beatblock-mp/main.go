package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bb-mp-server/internal/lobby"
	"bb-mp-server/internal/protocol"
)

func defaultListen() string {
	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		return addr
	}
	// Pterodactyl and most panels pass the allocated port as SERVER_PORT
	if port := os.Getenv("SERVER_PORT"); port != "" {
		return "0.0.0.0:" + port
	}
	return ":25656"
}

func defaultMaxPlayers() int {
	if raw := os.Getenv("MAX_PLAYERS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 32
}

func defaultMaxConnections() int {
	if raw := os.Getenv("MAX_CONNECTIONS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 128
}

func main() {
	listen := flag.String("listen", defaultListen(), "TCP address to listen on")
	maxPlayers := flag.Int("max-players", defaultMaxPlayers(), "maximum number of players per lobby")
	maxConns := flag.Int("max-connections", defaultMaxConnections(), "maximum simultaneous connections across all lobbies")
	roundTimeout := flag.Duration("round-timeout", 15*time.Minute, "maximum round length before remaining players are marked DNF")
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	flag.Parse()

	logger := newLogger(os.Stdout)

	srv := lobby.NewServer(lobby.Options{
		MaxPlayers:     *maxPlayers,
		MaxConnections: *maxConns,
		RoundTimeout:   *roundTimeout,
		Verbose:        *verbose,
		Logger:         logger,
	})

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		logger.Error(fmt.Sprintf("listen %s failed: %v", *listen, err))
		os.Exit(1)
	}

	port := 25656
	if _, p, err := net.SplitHostPort(ln.Addr().String()); err == nil {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	logger.Info(fmt.Sprintf("listening on %s (protocol %d, %d players per lobby, %d connections max, round timeout %s)",
		ln.Addr().String(), protocol.Version, *maxPlayers, *maxConns, roundTimeout.String()))
	for _, ip := range lanIPv4s() {
		logger.Info(fmt.Sprintf("LAN address: %s:%d", ip, port))
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-signals:
		logger.Info(fmt.Sprintf("shutting down (%s)", sig.String()))
		srv.Shutdown()
	case err := <-serveErr:
		if err != nil {
			logger.Error(fmt.Sprintf("server stopped: %v", err))
			srv.Shutdown()
			os.Exit(1)
		}
	}
}

func lanIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}
