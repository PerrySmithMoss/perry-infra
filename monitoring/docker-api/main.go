// A narrowly scoped native Docker reader. Administrator commands never enter
// the infrastructure release account's forced-command/sudo boundary.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func serve() error {
	if os.Getenv("LISTEN_FDS") != "1" || os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) {
		return errors.New("require one systemd activation socket")
	}
	file := os.NewFile(3, "systemd-socket")
	listener, err := net.FileListener(file)
	file.Close()
	if err != nil {
		return err
	}
	defer listener.Close()
	if listener.Addr().Network() != "unix" {
		return errors.New("require a Unix activation socket")
	}
	server := &http.Server{Handler: proxyHandler(unixClient("/var/run/docker.sock")), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "serve" {
		return serve()
	}
	return admin(args)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
