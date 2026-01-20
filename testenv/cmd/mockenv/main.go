package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	bind := flag.String("bind", "127.0.0.1", "Bind address")
	httpPort := flag.Int("http", 8080, "HTTP port")
	httpsPort := flag.Int("https", 8443, "HTTPS port")
	rawPort := flag.Int("raw", 5666, "Raw TCP port")
	allow := flag.String("allow", "allowed.test", "Comma-separated Host headers that return 200")
	flag.Parse()

	log.SetFlags(0)

	allowedHosts := parseAllowedHosts(*allow)
	handler := hostHandler(allowedHosts)

	httpAddr := fmt.Sprintf("%s:%d", *bind, *httpPort)
	httpsAddr := fmt.Sprintf("%s:%d", *bind, *httpsPort)
	rawAddr := fmt.Sprintf("%s:%d", *bind, *rawPort)

	tlsConfig, err := selfSignedTLSConfig()
	if err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}

	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: handler,
	}
	httpsSrv := &http.Server{
		Addr:      httpsAddr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rawLn, err := net.Listen("tcp", rawAddr)
	if err != nil {
		log.Fatalf("RAW listen failed: %v", err)
	}

	go func() {
		log.Printf("HTTP listening on %s", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP error: %v", err)
		}
	}()
	go func() {
		log.Printf("HTTPS listening on %s", httpsAddr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTPS error: %v", err)
		}
	}()
	go func() {
		log.Printf("RAW listening on %s", rawAddr)
		serveRaw(ctx, rawLn)
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = httpSrv.Shutdown(shutdownCtx)
	_ = httpsSrv.Shutdown(shutdownCtx)
	_ = rawLn.Close()
}

func parseAllowedHosts(input string) map[string]bool {
	allowed := make(map[string]bool)
	for _, host := range strings.Split(input, ",") {
		host = strings.TrimSpace(strings.ToLower(host))
		if host != "" {
			allowed[host] = true
		}
	}
	return allowed
}

func hostHandler(allowed map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := normalizeHost(r.Host)
		title := "Forbidden"
		body := "Host not allowed"
		status := http.StatusForbidden

		if allowed[host] {
			title = "OK"
			body = "Host allowed"
			status = http.StatusOK
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprintf(w, "<html><head><title>%s</title></head><body>%s</body></html>", title, body)
	}
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

func serveRaw(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("RAW accept error: %v", err)
				continue
			}
		}
		go handleRaw(conn)
	}
}

func handleRaw(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 256)
	_, _ = conn.Read(buf)
	_, _ = conn.Write([]byte("NRPE TEST BANNER\n"))
}

func selfSignedTLSConfig() (*tls.Config, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "local.test",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "local.test"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
