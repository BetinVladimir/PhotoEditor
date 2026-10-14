package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/provenance"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// readSigningIdentityFile rejects symlinks, hardlinks, mutable permissions and
// replacement while reading process-owned mTLS material.
func readSigningIdentityFile(ctx context.Context, path string, private bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("signing identity path must be absolute and normalized")
	}
	valid := func(info os.FileInfo) bool {
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 || info.Mode().Perm()&0022 != 0 || private && info.Mode().Perm()&0077 != 0 {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		return ok && stat.Nlink == 1
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !valid(before) {
		return nil, errors.New("signing identity file is unsafe")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !valid(opened) || !os.SameFile(before, opened) {
		return nil, errors.New("signing identity file changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, err
	}
	final, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !valid(final) || !valid(current) || !os.SameFile(opened, final) || !os.SameFile(opened, current) || opened.Size() != int64(len(raw)) || opened.Size() != final.Size() || opened.Size() != current.Size() || !opened.ModTime().Equal(final.ModTime()) || !opened.ModTime().Equal(current.ModTime()) {
		return nil, errors.New("signing identity file changed")
	}
	return raw, nil
}

func installProcessSigningRuntime(ctx context.Context, path, pin, certPath, keyPath string, dependencies apiProcessDependencies) (apiProcessDependencies, func(), error) {
	noop := func() {}
	if path == "" && pin == "" {
		if certPath != "" || keyPath != "" {
			return dependencies, noop, errors.New("signing identity requires pinned deployment")
		}
		return dependencies, noop, nil
	}
	if err := validateAPISigningDeclaration(path, pin, apiProcessDependencies{SigningTenants: map[uuid.UUID]provenance.SigningAPITenantRuntime{uuid.Nil: {}}}); err != nil {
		return dependencies, noop, err
	}
	if len(dependencies.SigningTenants) != 0 {
		if certPath != "" || keyPath != "" {
			return dependencies, noop, errors.New("signing identity conflicts with injected runtime")
		}
		return dependencies, noop, nil
	}
	if certPath == "" || keyPath == "" {
		return dependencies, noop, errors.New("signing deployment requires API mTLS certificate and private key")
	}
	doc, err := provenance.LoadSigningAPIDeploymentDocument(ctx, path, pin)
	if err != nil {
		return dependencies, noop, err
	}
	certPEM, err := readSigningIdentityFile(ctx, certPath, false)
	if err != nil {
		return dependencies, noop, err
	}
	keyPEM, err := readSigningIdentityFile(ctx, keyPath, true)
	if err != nil {
		return dependencies, noop, err
	}
	identity, err := tls.X509KeyPair(certPEM, keyPEM)
	clear(keyPEM)
	if err != nil || len(identity.Certificate) == 0 {
		return dependencies, noop, errors.New("signing API mTLS identity invalid")
	}
	leafSHA := sha256.Sum256(identity.Certificate[0])
	identityID := hex.EncodeToString(leafSHA[:])
	checks := make([]*provenance.HTTPSigningNativeCheck, 0, len(doc.Tenants))
	closeChecks := func() {
		for _, check := range checks {
			check.Close()
		}
	}
	tenants := make(map[uuid.UUID]provenance.SigningAPITenantRuntime, len(doc.Tenants))
	for _, entry := range doc.Tenants {
		if err := ctx.Err(); err != nil {
			closeChecks()
			return dependencies, noop, err
		}
		if entry.IdentityID != identityID {
			closeChecks()
			return dependencies, noop, errors.New("signing API identity differs from pinned deployment")
		}
		for _, wire := range entry.Policies {
			var policy provenance.SigningPolicy
			if json.Unmarshal(wire.Body, &policy) != nil || policy.TimestampMode == "required" {
				closeChecks()
				return dependencies, noop, errors.New("required TSA verifier is not installed")
			}
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(entry.ChallengeRoots)) {
			closeChecks()
			return dependencies, noop, errors.New("signing challenge roots invalid")
		}
		check, err := provenance.NewHTTPSigningNativeCheck(entry.ChallengeOrigin, roots, identity,
			entry.ChallengeServerSHA, time.Duration(entry.TimeoutSeconds)*time.Second)
		if err != nil {
			closeChecks()
			return dependencies, noop, err
		}
		checks = append(checks, check)
		tenants[entry.TenantID] = provenance.SigningAPITenantRuntime{
			WorkerID: entry.WorkerID, QualificationSHA: entry.QualificationSHA,
			IdentityID: identityID, Identity: identity, Native: check.Check,
		}
	}
	dependencies.SigningTenants = tenants
	return dependencies, closeChecks, nil
}
