package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

type sceneRoleTicket struct {
	DatabaseOID uint32   `json:"database_oid"`
	Name        string   `json:"name"`
	Nonce       string   `json:"nonce"`
	OID         uint32   `json:"oid"`
	Roles       []string `json:"roles"`
	State       string   `json:"state"`
}

func sceneRoleWrite(t *testing.T, path string, ticket sceneRoleTicket) {
	t.Helper()
	raw, err := json.Marshal(ticket)
	if err != nil {
		t.Fatal("scene login ticket unavailable")
	}
	tmp := path + ".next"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("scene login ticket unavailable")
	}
	_, we := f.Write(raw)
	se := f.Sync()
	ce := f.Close()
	if we != nil || se != nil || ce != nil || os.Rename(tmp, path) != nil {
		t.Fatal("scene login ticket persistence failed")
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		t.Fatal("scene login ticket directory unavailable")
	}
	de := d.Sync()
	d.Close()
	if de != nil {
		t.Fatal("scene login ticket durability failed")
	}
}
func sceneDSNValue(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'") + "'"
}
func sceneCreateLoginDSN(t *testing.T, dsn, user, password string) string {
	t.Helper()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal("scene login DSN invalid")
		}
		u.User = url.UserPassword(user, password)
		q := u.Query()
		q.Del("user")
		q.Del("password")
		q.Del("options")
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " user=" + sceneDSNValue(user) + " password=" + sceneDSNValue(password) + " options=''"
}
func sceneCreateRoleDSN(t *testing.T, dsn, role string) string {
	t.Helper()
	if role != "photo_api" && role != "photo_scheduler" {
		t.Fatal("scene login role refused")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal("scene login DSN invalid")
		}
		q := u.Query()
		q.Set("options", "-c role="+role)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " options=" + sceneDSNValue("-c role="+role)
}

// The fixture owner is retained solely for witness/provisioning operations.
// Runtime principals get only existing service-role memberships, never object
// ownership, direct table grants, CREATE, superuser or BYPASSRLS.
func sceneCreateApplicationLogins(t *testing.T, ctx context.Context, ownerDSN string) (string, string, string) {
	t.Helper()
	owner, err := pgxpool.New(ctx, ownerDSN)
	if err != nil {
		t.Fatal("scene login owner unavailable")
	}
	t.Cleanup(owner.Close)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal("scene login root unavailable")
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "work"), "scene-create-roles-private-")
	if err != nil || os.Chmod(dir, 0700) != nil {
		t.Fatal("scene login ledger unavailable")
	}
	var databaseOID uint32
	if owner.QueryRow(ctx, "SELECT oid FROM pg_database WHERE datname=current_database()").Scan(&databaseOID) != nil {
		t.Fatal("scene login database identity unavailable")
	}
	makeLogin := func(roles []string) string {
		nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
		name := "photo_test_scene_" + nonce
		password := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
		ident := pgx.Identifier{name}.Sanitize()
		marker := "scene-create-fixture:" + nonce
		ticket := sceneRoleTicket{DatabaseOID: databaseOID, Name: name, Nonce: nonce, Roles: roles, State: "planned"}
		path := filepath.Join(dir, nonce+".json")
		sceneRoleWrite(t, path, ticket)
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var oid uint32
			var safe bool
			e := owner.QueryRow(cleanup, `SELECT r.oid,NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreaterole AND NOT r.rolcreatedb AND NOT r.rolreplication AND NOT r.rolinherit AND shobj_description(r.oid,'pg_authid')=$2 AND (SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid)=$3 AND NOT EXISTS(SELECT FROM pg_auth_members m JOIN pg_roles p ON p.oid=m.roleid WHERE m.member=r.oid AND (m.admin_option OR NOT(p.rolname=ANY($4::text[])))) FROM pg_roles r WHERE r.rolname=$1`, name, marker, len(roles), roles).Scan(&oid, &safe)
			if e == pgx.ErrNoRows {
				ticket.State = "removed"
				sceneRoleWrite(t, path, ticket)
				return
			}
			if e != nil || (ticket.OID != 0 && oid != ticket.OID) || !safe {
				t.Error("scene login cleanup identity refused")
				return
			}
			// Pools and the executable are closed before this registered cleanup.
			if _, e = owner.Exec(cleanup, "DROP ROLE "+ident); e != nil {
				t.Error("scene login cleanup failed")
				return
			}
			ticket.State = "removed"
			sceneRoleWrite(t, path, ticket)
		})
		// One transaction makes CREATE+nonce COMMENT atomic even if the process dies
		// before its OID receipt is persisted. Planned recovery must verify marker.
		tx, e := owner.Begin(ctx)
		if e != nil {
			t.Fatal("scene login transaction unavailable")
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, "CREATE ROLE "+ident+" LOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD '"+password+"'"); e != nil {
			t.Fatal("scene login creation failed")
		}
		if _, e = tx.Exec(ctx, "COMMENT ON ROLE "+ident+" IS '"+marker+"'"); e != nil {
			t.Fatal("scene login marker failed")
		}
		for _, role := range roles {
			if _, e = tx.Exec(ctx, "GRANT "+pgx.Identifier{role}.Sanitize()+" TO "+ident); e != nil {
				t.Fatal("scene login membership failed")
			}
		}
		if e = tx.QueryRow(ctx, "SELECT oid FROM pg_roles WHERE rolname=$1", name).Scan(&ticket.OID); e != nil {
			t.Fatal("scene login identity unavailable")
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal("scene login commit failed")
		}

		ticket.State = "created"
		sceneRoleWrite(t, path, ticket)
		dsn := sceneCreateLoginDSN(t, ownerDSN, name, password)
		for _, role := range roles {
			check, e := pgxpool.ParseConfig(dsn)
			if e != nil {
				t.Fatal("scene login preflight invalid")
			}
			if check.ConnConfig.User != name || check.ConnConfig.Password != password {
				t.Fatal("scene login identity rewrite refused")
			}
			check.ConnConfig.RuntimeParams["role"] = role
			database.GuardApplicationRole(check)
			pool, e := pgxpool.NewWithConfig(ctx, check)
			if e != nil {
				t.Fatal("scene login preflight unavailable")
			}
			e = database.RequireApplicationRole(ctx, pool, role)
			pool.Close()
			if e != nil {
				t.Fatal("scene login isolation preflight refused")
			}
		}
		return dsn
	}
	app := makeLogin([]string{"photo_api", "photo_portal"})
	auth := makeLogin([]string{"photo_auth"})
	scheduler := makeLogin([]string{"photo_scheduler"})
	return app, auth, sceneCreateRoleDSN(t, scheduler, "photo_scheduler")
}
