package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	flag "github.com/spf13/pflag"

	"sehlabs.com/db/internal/db"
)

func fatal(code int, m string) {
	fmt.Fprintln(os.Stderr, m)
	os.Exit(code)
}

var (
	serverAddress      net.IP
	serverPort         string
	tlsCertificateFile string
	tlsPrivateKeyFile  string
)

func fatalf(code int, format string, a ...interface{}) {
	w := os.Stderr
	if _, err := fmt.Fprintf(w, format, a...); err == nil {
		fmt.Fprintln(w)
	}
	os.Exit(code)
}

func init() {
	flag.IPVar(&serverAddress, "server-address", nil,
		`IP address on which to serve HTTP requests`)
	flag.StringVar(&serverPort, "server-port", "",
		`Port on which to serve HTTP requests`)
	flag.StringVar(&tlsCertificateFile, "tls-cert-file", "",
		`File containing the X.509 certificates with which to serve HTTPS,
containing certificates for this server, any intermediate CAs, and the CA`)
	flag.StringVar(&tlsPrivateKeyFile, "tls-private-key-file", "",
		`File containing the X.509 private key for the first X.509 certificate
in --tls-cert-file`)
}

type tlsConfig struct {
	certificateFilePath string
	privateKeyFilePath  string
}

func joinIPAddressAndPort(address net.IP, port string) string {
	var host string
	var empty net.IP
	if !address.Equal(empty) {
		host = address.String()
	}
	return net.JoinHostPort(host, port)
}

func runHTTPServer(address net.IP, port string, tlsConf *tlsConfig, handler http.Handler, stop <-chan struct{}) error {
	server := &http.Server{
		Addr:    joinIPAddressAndPort(address, port),
		Handler: handler,
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-stop
		// Don't bother imposing a timeout here.
		if err := server.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to shut down HTTP server: %v\n", err)
		}
	}()
	var err error
	if tlsConf != nil {
		err = server.ListenAndServeTLS(tlsConf.certificateFilePath, tlsConf.privateKeyFilePath)
	} else {
		err = server.ListenAndServe()
	}
	if err != http.ErrServerClosed {
		return err
	}
	wg.Wait()
	return nil
}

func main() {
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var serverTLSConfig *tlsConfig
	if len(tlsCertificateFile) > 0 {
		if len(tlsPrivateKeyFile) == 0 {
			fatal(2, "--tls-private-key-file must be nonempty when --tls-cert-file is specified")
		}
		serverTLSConfig = &tlsConfig{
			certificateFilePath: tlsCertificateFile,
			privateKeyFilePath:  tlsPrivateKeyFile,
		}
	} else if len(tlsPrivateKeyFile) > 0 {
		fatal(2, "--tls-cert-file must be nonempty when --tls-private-key-file is specified")
	}

	if len(serverPort) == 0 {
		if serverTLSConfig != nil {
			serverPort = "443"
		} else {
			serverPort = "80"
		}
	}
	// TODO(seh): Wrap with OpenTelemetry instrumentation.
	store, err := db.MakeShardedStore()
	if err != nil {
		fatalf(1, "Failed to create database: %v", err)
	}
	handler := makeHandler(store)
	if err := runHTTPServer(serverAddress, serverPort, serverTLSConfig, handler, ctx.Done()); err != nil {
		fatalf(1, "HTTP server failed: %v", err)
	}
}
