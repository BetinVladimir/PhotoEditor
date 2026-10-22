package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
)

func text(row map[string]json.RawMessage, k string) string {
	var s string
	_ = json.Unmarshal(row[k], &s)
	return s
}
func (s scope) normalize(table string, row map[string]json.RawMessage) (bool, error) {
	if table == "tasks" {
		if b, ok := s.deferred[text(row, "id")]; ok {
			if text(row, "tenant_id") != b.Tenant || text(row, "run_id") != b.Run || text(row, "operation") != "asset.verify" || text(row, "resource_class") != "media" || text(row, "state") != "ready" {
				return false, errors.New("six public immutable scope/state changed")
			}
			reason := text(row, "terminal_reason")
			if reason != "" && reason != "waiting_worker" {
				return false, errors.New("six public reason changed")
			}
			delete(row, "admission_after")
			delete(row, "terminal_reason")
		}
	}
	tenant := text(row, "tenant_id")
	ownTenant := tenant == s.Tenant.String()
	ownTask := false
	for _, t := range s.Tasks {
		if text(row, "task_id") == t.ID.String() || table == "tasks" && text(row, "id") == t.ID.String() {
			ownTask = true
		}
	}
	if table == "tasks" && ownTask {
		if !ownTenant || text(row, "run_id") != s.Run.String() {
			return false, errors.New("foreign task binding")
		}
		return true, nil
	}
	if ownTenant && (table == "runs" && text(row, "id") == s.Run.String() || table == "apply_builds" && text(row, "run_id") == s.Run.String() || table == "apply_v4_stages" && text(row, "run_id") == s.Run.String()) {
		return true, nil
	}
	if ownTenant && ownTask && (table == "task_attempts" || table == "resource_reservations" || table == "task_results") {
		return true, nil
	}
	if table == "outbox" {
		var e map[string]json.RawMessage
		if json.Unmarshal(row["payload"], &e) == nil && text(e, "run_id") == s.Run.String() && text(e, "tenant_id") == s.Tenant.String() {
			for _, t := range s.Tasks {
				if text(e, "task_id") == t.ID.String() && ownTenant {
					return true, nil
				}
			}
		}
	}
	if table == "resource_slots" {
		for _, n := range s.Nodes {
			if text(row, "node_id") == n {
				if text(row, "task_id") != "" && (!ownTask || !ownTenant) {
					return false, errors.New("foreign resource slot")
				}
				for _, k := range []string{"task_id", "tenant_id", "lease_until", "fencing_token"} {
					delete(row, k)
				}
			}
		}
	}
	if table == "scheduler_tenants" && ownTenant {
		for _, k := range []string{"virtual_finish", "last_dispatch_at", "preview_streak"} {
			delete(row, k)
		}
	}
	return false, nil
}

type snapshot map[string]string

// Scan protected rows in memory only; emit digests, never row payloads. All photo
// tables are protected except liveness-only fields on registered workers/nodes.
// Bounds reject large databases; the root must review actual deployment inventory.
func capture(ctx context.Context, tx pgx.Tx, s scope) (snapshot, error) {
	rows, e := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind IN ('r','p') AND NOT c.relispartition ORDER BY c.relname`)
	if e != nil {
		return nil, e
	}
	var tables []string
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			rows.Close()
			return nil, e
		}
		tables = append(tables, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	result := snapshot{}
	total := 0
	bytesSeen := 0
	for _, table := range tables {
		q := fmt.Sprintf("SELECT to_jsonb(t) FROM %s t LIMIT 10001", pgx.Identifier{"photo", table}.Sanitize())
		r, err := tx.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		var hashes []string
		n := 0
		for r.Next() {
			var raw []byte
			if err = r.Scan(&raw); err != nil {
				r.Close()
				return nil, err
			}
			n++
			total++
			bytesSeen += len(raw)
			if n > 10000 || total > 100000 || bytesSeen > 64<<20 {
				r.Close()
				return nil, errors.New("protected snapshot bounded limit")
			}
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil {
				r.Close()
				return nil, errors.New("invalid database row")
			}
			skip, err := s.normalize(table, row)
			if err != nil {
				r.Close()
				return nil, err
			}
			if skip {
				continue
			}
			canonical, err := json.Marshal(row)
			if err != nil {
				r.Close()
				return nil, err
			}
			h := sha256.Sum256(canonical)
			hashes = append(hashes, hex.EncodeToString(h[:]))
		}
		err = r.Err()
		r.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(hashes)
		h := sha256.Sum256([]byte(fmt.Sprint(hashes)))
		result[table] = hex.EncodeToString(h[:])
	}
	return result, nil
}
func equalSnapshots(a, b snapshot) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}
