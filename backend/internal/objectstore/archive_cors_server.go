package objectstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxCORSAdminResponse = 128 * 1024

// boundedAdminTransport bounds encrypted configuration before the SDK decrypts
// it. Credentials and decrypted configuration never become diagnostic text.
type boundedAdminTransport struct {
	base    http.RoundTripper
	onClose func(error)
}

func (t boundedAdminTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, ErrUnavailable
	}
	if (response.StatusCode >= 300 && response.StatusCode < 400) || response.ContentLength > maxCORSAdminResponse {
		response.Body.Close()
		return nil, ErrIntegrity
	}
	response.Body = &boundedAdminBody{ReadCloser: response.Body, remaining: maxCORSAdminResponse, onClose: t.onClose}
	return response, nil
}

type boundedAdminBody struct {
	io.ReadCloser
	remaining int64
	onClose   func(error)
}

func (b *boundedAdminBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			return 0, ErrIntegrity
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *boundedAdminBody) Close() error {
	err := b.ReadCloser.Close()
	if b.onClose != nil {
		b.onClose(err)
	}
	return err
}

func corsServerReader(endpoint *url.URL, creds *credentials.Credentials, transport http.RoundTripper) (func(context.Context) ([]byte, error), error) {
	// Construct per invocation so close errors cannot leak between concurrent
	// bootstraps. The SDK closes its decrypted response before returning.
	return func(ctx context.Context) ([]byte, error) {
		var closeErr error
		admin, err := madmin.NewWithOptions(endpoint.Host, &madmin.Options{Creds: creds, Secure: endpoint.Scheme == "https", Transport: boundedAdminTransport{base: transport, onClose: func(err error) { closeErr = errors.Join(closeErr, err) }}})
		if err != nil {
			return nil, ErrInvalid
		}
		data, err := admin.GetConfigKV(ctx, "api")
		if err != nil || closeErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrUnavailable
		}
		if len(data) > maxCORSAdminResponse {
			return nil, ErrIntegrity
		}
		return data, nil
	}, nil
}

func corsServerOriginsMatch(data []byte, expected []string, development bool) bool {
	if len(data) == 0 || len(data) > maxCORSAdminResponse || len(expected) == 0 {
		return false
	}
	// The SDK parser intentionally tolerates dangling env comments and duplicate
	// keys. Policy attestation rejects those ambiguous envelopes before parsing.
	seenConfig, envCount := false, 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# MINIO_") {
			if seenConfig || !strings.HasPrefix(line, "# MINIO_API_") {
				return false
			}
			if strings.HasPrefix(line, "# MINIO_API_CORS_ALLOW_ORIGIN=") {
				envCount++
				if envCount > 1 {
					return false
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if seenConfig || strings.Count(line, "cors_allow_origin=") != 1 {
			return false
		}
		seenConfig = true
	}
	if !seenConfig {
		return false
	}
	configs, err := madmin.ParseServerConfigOutput(string(data))
	if err != nil || len(configs) != 1 || configs[0].SubSystem != "api" || configs[0].Target != "" {
		return false
	}
	raw, present := configs[0].Lookup("cors_allow_origin")
	if !present || raw == "" {
		return false
	}
	// Require a complete exact set. Duplicate and empty entries are malformed,
	// even though ordinary application-origin configuration is deduplicated.
	values := strings.Split(raw, ",")
	actual, err := archiveCORSOrigins(values, development)
	if err != nil || len(actual) != len(values) || len(actual) != len(expected) {
		return false
	}
	allowed := make(map[string]bool, len(expected))
	for _, origin := range expected {
		allowed[origin] = true
	}
	for _, origin := range actual {
		if !allowed[origin] {
			return false
		}
		delete(allowed, origin)
	}
	return len(allowed) == 0
}

func corsHeaderContains(value, expected string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), expected) {
			return true
		}
	}
	return false
}

// Probes supplement privileged complete-policy readback. They cannot replace it.
func verifyCORSServerPreflight(ctx context.Context, client *http.Client, endpoint, bucket string, origins []string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || len(origins) == 0 {
		return ErrInvalid
	}
	u.Path = "/" + bucket + "/photo-cors-bootstrap-probe"
	foreign := "https://photo-cors-probe.invalid"
	for {
		found := false
		for _, origin := range origins {
			if origin == foreign {
				found = true
				break
			}
		}
		if !found {
			break
		}
		foreign = "https://extra." + strings.TrimPrefix(foreign, "https://")
	}
	headers := []string{"content-type", "x-amz-content-sha256", "x-amz-meta-sha256", "if-none-match"}
	check := func(origin, method string, trusted bool) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodOptions, u.String(), nil)
		if err != nil {
			return ErrInvalid
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", method)
		request.Header.Set("Access-Control-Request-Headers", strings.Join(headers, ","))
		response, err := client.Do(request)
		if err != nil {
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrUnavailable
		}
		if response == nil || response.Body == nil {
			return ErrUnavailable
		}
		// Do not drain arbitrary untrusted bodies; Close releases the response.
		closeErr := response.Body.Close()
		if closeErr != nil {
			return ErrUnavailable
		}
		allow := response.Header.Values("Access-Control-Allow-Origin")
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return ErrIntegrity
		}
		if !trusted {
			if len(allow) != 0 {
				return ErrIntegrity
			}
			return nil
		}
		if len(allow) != 1 || allow[0] != origin || !corsHeaderContains(response.Header.Get("Access-Control-Allow-Methods"), method) {
			return ErrIntegrity
		}
		for _, header := range headers {
			if !corsHeaderContains(response.Header.Get("Access-Control-Allow-Headers"), header) && !corsHeaderContains(response.Header.Get("Access-Control-Allow-Headers"), "*") {
				return ErrIntegrity
			}
		}
		return nil
	}
	for _, origin := range origins {
		for _, method := range []string{"GET", "HEAD", "PUT"} {
			if err := check(origin, method, true); err != nil {
				return err
			}
		}
	}
	for _, method := range []string{"GET", "HEAD", "PUT"} {
		if err := check(foreign, method, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) bootstrapCORSServer(ctx context.Context) error {
	if s.corsServerRead == nil || s.corsHTTP == nil || len(s.corsOrigins) == 0 {
		return ErrInvalid
	}
	data, err := s.corsServerRead(ctx)
	if err != nil {
		return err
	}
	if !corsServerOriginsMatch(data, s.corsOrigins, s.corsDevelopment) {
		return ErrIntegrity
	}
	return verifyCORSServerPreflight(ctx, s.corsHTTP, s.corsPublicEndpoint, s.bucket, s.corsOrigins)
}
