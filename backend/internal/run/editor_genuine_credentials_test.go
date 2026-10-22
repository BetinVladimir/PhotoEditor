package run

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

type genuineCredential struct {
	Login string `json:"login"`
	DSN   string `json:"dsn"`
}

func genuineCredentialConfig(raw, kind string) (*pgxpool.Config, string, error) {
	fail := func() (*pgxpool.Config, string, error) {
		return nil, "", errors.New("dedicated application credential required")
	}
	roles := map[string]string{"auth": "photo_auth", "api": "photo_api", "scheduler": "photo_scheduler", "gateway": "photo_gateway", "worker": "photo_worker"}
	role, ok := roles[kind]
	if !ok {
		return fail()
	}
	var entries map[string]genuineCredential
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&entries) != nil || decoder.Decode(new(any)) != io.EOF || len(entries) != len(roles) {
		return fail()
	}
	seen := map[string]bool{}
	for k, e := range entries {
		if roles[k] == "" || !regexp.MustCompile(`^editor_owned_[a-z0-9_]{16,48}$`).MatchString(e.Login) || seen[e.Login] {
			return fail()
		}
		seen[e.Login] = true
	}
	entry := entries[kind]
	cfg, e := pgxpool.ParseConfig(entry.DSN)
	if e != nil || cfg.ConnConfig.User != entry.Login || cfg.ConnConfig.RuntimeParams["options"] != "-c role="+role || (cfg.ConnConfig.RuntimeParams["role"] != "" && cfg.ConnConfig.RuntimeParams["role"] != role) {
		return fail()
	}
	// pgx retains DSN options verbatim; GuardApplicationRole reads role directly.
	// Select the same already validated application role, never change the login.
	cfg.ConnConfig.RuntimeParams["role"] = role
	database.GuardApplicationRole(cfg)
	previous := cfg.AfterConnect
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		if e := previous(ctx, c); e != nil {
			return e
		}
		var exact bool
		if e := c.QueryRow(ctx, `SELECT session_user=$1 AND current_user=$2`, entry.Login, role).Scan(&exact); e != nil {
			return e
		}
		if !exact {
			return errors.New("dedicated login/role identity mismatch")
		}
		return nil
	}
	return cfg, role, nil
}
func genuineDedicatedPool(t *testing.T, ctx context.Context, kind string) *pgxpool.Pool {
	t.Helper()
	cfg, _, e := genuineCredentialConfig(os.Getenv("PHOTO_EDITOR_APPLICATION_CREDENTIALS"), kind)
	if e != nil {
		t.Fatal(e)
	}
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("dedicated application pool")
	}
	t.Cleanup(p.Close)
	// Acquire forces AfterConnect attestation before any application call.
	c, e := p.Acquire(ctx)
	if e != nil {
		t.Fatal("dedicated physical connection attestation")
	}
	c.Release()
	return p
}
func TestGenuineCredentialConfigRejectsOwnerSetRole(t *testing.T) {
	entries := map[string]genuineCredential{}
	for _, k := range []string{"auth", "api", "scheduler", "gateway", "worker"} {
		login := "editor_owned_abcdefghijklmnop_" + k
		entries[k] = genuineCredential{login, "host=localhost user=" + login + " dbname=owned options='-c role=photo_" + k + "'"}
	}
	raw, _ := json.Marshal(entries)
	if _, _, e := genuineCredentialConfig(string(raw), "api"); e != nil {
		t.Fatal(e)
	}
	entries["api"] = genuineCredential{"editor_owned_abcdefghijklmnop_api", "host=localhost user=photo_test dbname=owned options='-c role=photo_api'"}
	raw, _ = json.Marshal(entries)
	if _, _, e := genuineCredentialConfig(string(raw), "api"); e == nil {
		t.Fatal("owner SET ROLE accepted")
	}
}

func TestIntegrationVerifyGenuineApplicationCredentials(t *testing.T) {
	if os.Getenv("PHOTO_EDITOR_APPLICATION_CREDENTIALS") == "" {
		t.Skip("owned dedicated five-role credentials required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, kind := range []string{"auth", "api", "scheduler", "gateway", "worker"} {
		t.Run(kind, func(t *testing.T) { genuineDedicatedPool(t, ctx, kind) })
	}
}
