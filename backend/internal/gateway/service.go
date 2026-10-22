package gateway

import (
 "photoeditor/backend/internal/database"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/http"
	"os"
	"photoeditor/backend/internal/objectstore"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

type ServiceConfig struct {
	DatabaseURL, Listen, ClientCA, Certificate, PrivateKey, SigningKeys, Issuer string
	Storage                                                                     objectstore.Config
	MaxDownloadBytes, MaxUploadBytes                                            int64
}

func ConfigFromEnv(dsn, listen string, development bool) (ServiceConfig, error) {
	c := ServiceConfig{DatabaseURL: dsn, Listen: listen, ClientCA: os.Getenv("GATEWAY_CLIENT_CA_FILE"), Certificate: os.Getenv("GATEWAY_CERT_FILE"), PrivateKey: os.Getenv("GATEWAY_KEY_FILE"), SigningKeys: os.Getenv("GATEWAY_SIGNING_KEYS_FILE"), Issuer: os.Getenv("GATEWAY_ISSUER"), Storage: objectstore.ConfigFromEnv(development)}
	if c.Issuer == "" {
		c.Issuer = "photoeditor-gateway"
	}
	var err error
	c.MaxDownloadBytes, err = strconv.ParseInt(os.Getenv("GATEWAY_MAX_DOWNLOAD_BYTES"), 10, 64)
	if err != nil {
		return c, errors.New("GATEWAY_MAX_DOWNLOAD_BYTES required")
	}
	c.MaxUploadBytes, err = strconv.ParseInt(os.Getenv("GATEWAY_MAX_UPLOAD_BYTES"), 10, 64)
	if err != nil {
		return c, errors.New("GATEWAY_MAX_UPLOAD_BYTES required")
	}
	if c.MaxDownloadBytes < 1 || c.MaxUploadBytes < 1 || c.MaxDownloadBytes > 9007199254740991 || c.MaxUploadBytes > 9007199254740991 {
		return c, errors.New("invalid gateway byte limits")
	}
	if c.DatabaseURL == "" || c.ClientCA == "" || c.Certificate == "" || c.PrivateKey == "" || c.SigningKeys == "" {
		return c, errors.New("gateway database and TLS/key files required")
	}
	if _, _, err = net.SplitHostPort(c.Listen); err != nil {
		return c, errors.New("invalid gateway listen address")
	}
	return c, nil
}
func readCredential(path string, secret bool) ([]byte, error) {
	invalid := errors.New("invalid gateway credential file or permissions")
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > 65536 || (secret && before.Mode().Perm()&0077 != 0) {
		return nil, invalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, invalid
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() > 65536 || (secret && after.Mode().Perm()&0077 != 0) {
		return nil, invalid
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, invalid
	}
	return data, nil
}
func LoadSigningKeys(path, issuer string) (CapabilitySigner, error) {
	invalid := errors.New("invalid gateway signing keys")
	data, err := readCredential(path, true)
	if err != nil {
		return CapabilitySigner{}, err
	}
	// Traverse tokens before typed decoding to reject duplicate keys at every level.
	d := json.NewDecoder(bytes.NewReader(data))
	if err = uniqueKeys(d); err != nil {
		return CapabilitySigner{}, invalid
	}
	if _, err = d.Token(); err != io.EOF {
		return CapabilitySigner{}, invalid
	}
	var file struct {
		Active string            `json:"active"`
		Seed   string            `json:"seed"`
		Public map[string]string `json:"public"`
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&file) != nil || issuer == "" {
		return CapabilitySigner{}, invalid
	}
	seed, err := base64.StdEncoding.DecodeString(file.Seed)
	if err != nil || len(seed) != 32 || len(file.Public) > 32 {
		return CapabilitySigner{}, invalid
	}
	signer := CapabilitySigner{Active: file.Active, Private: ed25519.NewKeyFromSeed(seed), Issuer: issuer, Public: map[string]ed25519.PublicKey{}}
	kidPattern := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for kid, encoded := range file.Public {
		key, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil || len(key) != 32 || !kidPattern.MatchString(kid) {
			return CapabilitySigner{}, invalid
		}
		signer.Public[kid] = ed25519.PublicKey(key)
	}
	if !kidPattern.MatchString(file.Active) || !bytes.Equal(signer.Public[file.Active], signer.Private.Public().(ed25519.PublicKey)) {
		return CapabilitySigner{}, invalid
	}
	return signer, nil
}
func uniqueKeys(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("invalid JSON")
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate key")
			}
			seen[name] = true
		}
		if err := uniqueKeys(d); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil {
		return err
	}
	if (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errors.New("invalid JSON")
	}
	return nil
}
func LoadServerTLS(c ServiceConfig) (*tls.Config, error) {
	ca, err := readCredential(c.ClientCA, false)
	if err != nil {
		return nil, err
	}
	cert, err := readCredential(c.Certificate, false)
	if err != nil {
		return nil, err
	}
	key, err := readCredential(c.PrivateKey, true)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, errors.New("invalid gateway TLS key pair")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid gateway client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{pair}}, nil
}
func Run(ctx context.Context, c ServiceConfig) error {
	signer, err := LoadSigningKeys(c.SigningKeys, c.Issuer)
	if err != nil {
		return err
	}
	tlsConfig, err := LoadServerTLS(c)
	if err != nil {
		return err
	}
	storage, err := objectstore.New(c.Storage)
	if err != nil {
		return err
	}
	config, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid gateway database configuration")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_gateway"
	database.GuardApplicationRole(config)
	config.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("gateway database unavailable")
	}
	defer pool.Close()
	if err = database.RequireApplicationRole(ctx, pool, "photo_gateway"); err != nil { return err }
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = pool.Ping(startup)
	if err == nil {
		err = RequireOutputFences(startup, pool)
	}
	if err == nil {
		err = storage.Ready(startup)
	}
	cancel()
	if err != nil {
		return errors.New("gateway dependencies unavailable")
	}
	authorizer := Authorizer{DB: pool}
	api := &HTTP{Tasks: authorizer, Inputs: authorizer, Objects: storage, Bucket: c.Storage.Bucket, Signer: signer, MaxDownloadBytes: c.MaxDownloadBytes, MaxUploadBytes: c.MaxUploadBytes, Uploads: &UploadService{Repository: SQLOutputs{DB: pool, Bucket: c.Storage.Bucket}, Objects: storage}}
	mux := http.NewServeMux()
	mux.Handle("/internal/", api.Handler())
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		check, done := context.WithTimeout(r.Context(), 5*time.Second)
		defer done()
		if pool.Ping(check) != nil || storage.Ready(check) != nil {
			gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		w.WriteHeader(200)
	})
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return errors.New("gateway listen failed")
	}
	server := &http.Server{Handler: mux, TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024, BaseContext: func(net.Listener) context.Context { return ctx }}
	defer server.Close()
	failures := make(chan error, 1)
	go func() { failures <- server.ServeTLS(listener, "", "") }()
	select {
	case <-ctx.Done():
		stop, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		return server.Shutdown(stop)
	case err := <-failures:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("gateway listener stopped")
	}
}
