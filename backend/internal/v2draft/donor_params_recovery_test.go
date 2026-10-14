package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestDonorParamsRecoveryCommitAndOwnedBytes(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, mode := range []string{"ok", "revoked", "duplicate", "role", "commit", "oversize", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			tx := &donorDescriptorTestTx{role: "photo_scheduler", raw: []byte(`{ "params_jcs": "{}" }`), failCommit: mode == "commit"}
			switch mode {
			case "revoked":
				tx.raw = nil
			case "duplicate":
				tx.raw = []byte(`{"x":1,"x":2}`)
			case "role":
				tx.role = "photo_api"
			case "oversize":
				tx.raw = make([]byte, (1<<20)+1)
			}
			db := &donorDescriptorTestDB{tx: tx}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			result, err := (SQLDonorParamsPublisher{DB: db}).ReadIntent(ctx, who, uuid.New(), uuid.New())
			if (err == nil) != (mode == "ok") {
				t.Fatalf("err=%v", err)
			}
			if mode == "ok" {
				if string(result) != `{"params_jcs":"{}"}` || tx.commits != 1 {
					t.Fatal("uncommitted/unnormalized result")
				}
				tx.raw[0] = '!'
				if result[0] != '{' {
					t.Fatal("borrowed result")
				}
			}
			if err != nil && result != nil {
				t.Fatal("partial recovery grant")
			}
			if mode == "role" && tx.calls != 1 {
				t.Fatal("wrong role read recovery")
			}
			if mode == "cancelled" && db.begins != 0 {
				t.Fatal("cancelled read began transaction")
			}
		})
	}
}
