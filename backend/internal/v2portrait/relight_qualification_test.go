package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type relightQualificationTx struct {
	donorIdentityTx
	queries []string
}

func (tx *relightQualificationTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return tx.donorIdentityTx.QueryRow(ctx, query, args...)
}

func TestRelightQualificationUsesDedicatedSQLAuthority(t *testing.T) {
	_, pin, _, _, _ := relightQualificationFixture(t)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	for _, mode := range []string{"ready", "stale", "role", "pin", "cancelled"} {
		p := pin
		ctx := context.Background()
		tx := &relightQualificationTx{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", ready: true}}
		switch mode {
		case "stale":
			tx.ready = false
		case "role":
			tx.role = "photo_api"
		case "pin":
			p.PolicyID = uuid.Nil
		case "cancelled":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		err := LockRelightQualification(ctx, tx, who, tenant, project, p)
		if mode == "ready" {
			if err != nil || len(tx.queries) != 2 || tx.queries[1] != "SELECT photo.lock_v28_relight_qualification($1,$2,$3,$4,$5::jsonb)" {
				t.Fatal("relight gate", err)
			}
			var frozen RelightQualificationPin
			if len(tx.args) != 5 || tx.args[0] != tenant || tx.args[1] != project || tx.args[2] != who.UserID || tx.args[3] != who.SessionID || json.Unmarshal([]byte(tx.args[4].(string)), &frozen) != nil || frozen != pin {
				t.Fatal("SQL binding changed")
			}
		} else if err == nil {
			t.Fatal("invalid authority accepted", mode)
		}
		if (mode == "pin" || mode == "cancelled") && len(tx.queries) != 0 {
			t.Fatal("invalid authority performed SQL")
		}
		if mode == "role" && len(tx.queries) != 1 {
			t.Fatal("API reached definer")
		}
	}
	if err := LockRelightQualification(context.Background(), nil, who, tenant, project, pin); err == nil {
		t.Fatal("nil transaction")
	}
}

func TestRelightSourceUsesDedicatedSQLAuthority(t *testing.T) {
	p, _, _, _, _ := relightQualificationFixture(t)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	for _, mode := range []string{"ready", "stale", "role", "binding"} {
		tx := &relightQualificationTx{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", ready: true}}
		base := p.Base
		switch mode {
		case "stale":
			tx.ready = false
		case "role":
			tx.role = "photo_api"
		case "binding":
			base.CanvasHash = "invalid"
		}
		err := LockRelightSource(context.Background(), tx, who, tenant, project, snapshot, base)
		if mode == "ready" {
			if err != nil || len(tx.queries) != 2 || tx.queries[1] != "SELECT photo.lock_v28_relight_source($1,$2,$3,$4,$5,$6::jsonb)" {
				t.Fatal("source gate", err)
			}
			var frozen v2input.SessionVersionBinding
			if len(tx.args) != 6 || tx.args[2] != snapshot || json.Unmarshal([]byte(tx.args[5].(string)), &frozen) != nil || frozen != base {
				t.Fatal("source binding changed")
			}
		} else if err == nil {
			t.Fatal("invalid source scope", mode)
		}
		if mode == "binding" && len(tx.queries) != 0 {
			t.Fatal("invalid binding SQL")
		}
	}
}
