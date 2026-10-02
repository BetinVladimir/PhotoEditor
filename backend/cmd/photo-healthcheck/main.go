package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	ca := flag.String("client-ca", "", "PEM CA for an mTLS endpoint")
	cert := flag.String("client-cert", "", "PEM client certificate")
	key := flag.String("client-key", "", "PEM client key")
	serverName := flag.String("server-name", "", "expected TLS server name")
	flag.Parse()
	if flag.NArg() != 1 || check(flag.Arg(0), *ca, *cert, *key, *serverName) != nil {
		os.Exit(1)
	}
}

func check(raw, caPath, certPath, keyPath, serverName string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return errors.New("invalid health URL")
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("health URL must use loopback")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if caPath != "" || certPath != "" || keyPath != "" {
		if caPath == "" || certPath == "" || keyPath == "" || u.Scheme != "https" {
			return errors.New("incomplete mTLS configuration")
		}
		ca, err := os.ReadFile(caPath)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return errors.New("invalid CA")
		}
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return err
		}
		tlsConfig.RootCAs, tlsConfig.Certificates = pool, []tls.Certificate{pair}
	}
	client := http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	request, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return errors.New("unhealthy")
	}
	return nil
}
