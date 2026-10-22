package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"time"
)

type acceptedAPI struct {
	Schema   int    `json:"schema"`
	Status   int    `json:"http_status"`
	BodySHA  string `json:"body_sha256"`
	Response struct {
		Run    uuid.UUID `json:"run_id"`
		Output uuid.UUID `json:"output_collection_id"`
		Status string    `json:"status"`
	} `json:"response"`
}

// A readonly future-run collector. The API receipt is private/exact-CAS, but its
// provenance still requires independent review; this program does not issue one.
func readbackOnly() error {
	var a acceptedAPI
	if e := privateJSON(os.Getenv("APPLY_API_RECEIPT_FILE"), os.Getenv("APPLY_API_RECEIPT_SHA256"), &a); e != nil {
		return e
	}
	if a.Schema != 1 || a.Status != 202 || a.Response.Status != "queued" || a.Response.Run == uuid.Nil || a.Response.Output == uuid.Nil || a.Response.Output == collection {
		return errors.New("PlanNotSubmitted")
	}
	ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
	defer c()
	p, e := operatorPool(ctx)
	if e != nil {
		return e
	}
	defer p.Close()
	tx, e := p.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	s := scope{Schema: 1, ObservedAt: time.Now().UTC(), Run: a.Response.Run, Output: a.Response.Output}
	var count int
	var revision int64
	e = tx.QueryRow(ctx, `SELECT (SELECT max(version_id) FROM goose_db_version WHERE is_applied),r.tenant_id,c.project_id,b.source_collection_id,b.style_version_id,sv.config_hash,r.config_hash,b.phase,b.graph_version,b.item_count,c.source_revision
 FROM photo.runs r JOIN photo.apply_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id
 JOIN photo.collections c ON c.tenant_id=r.tenant_id AND c.id=r.output_collection_id
 JOIN photo.style_versions sv ON sv.tenant_id=r.tenant_id AND sv.id=b.style_version_id
 WHERE r.id=$1 AND r.output_collection_id=$2 AND r.kind='apply' AND sv.approval_status='accepted'`, s.Run, s.Output).Scan(&s.Head, &s.Tenant, &s.Project, &s.Collection, &s.Style, &s.StyleSHA, &s.ConfigSHA, &s.Phase, &s.Graph, &count, &revision)
	if e != nil {
		return e
	}
	if count != 3 || revision != 4 || s.Head != 2654 || s.Project != project || s.Collection != collection || s.Style != fixedStyle || s.StyleSHA != fixedStyleSHA {
		return errors.New("actual202 readback outside approved plan")
	}
	rows, e := tx.Query(ctx, `SELECT i.item_id,v.asset_id,i.asset_version_id,i.artifact_id,i.sha256 FROM photo.runs r JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256 WHERE r.tenant_id=$1 AND r.id=$2 ORDER BY i.ordinal`, s.Tenant, s.Run)
	if e != nil {
		return e
	}
	for rows.Next() {
		var b itemBinding
		if e = rows.Scan(&b.Item, &b.Asset, &b.Version, &b.Master, &b.MasterSHA); e != nil {
			rows.Close()
			return e
		}
		s.Items = append(s.Items, b)
		if len(s.Items) > 3 {
			rows.Close()
			return errors.New("more than three inputs")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = tx.Query(ctx, "SELECT id,operation,resource_class,execution_profile::text FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 ORDER BY id", s.Tenant, s.Run)
	if e != nil {
		return e
	}
	for rows.Next() {
		var b taskBinding
		var raw []byte
		if e = rows.Scan(&b.ID, &b.Operation, &b.Class, &raw); e != nil {
			rows.Close()
			return e
		}
		h := sha256.Sum256(raw)
		b.ProfileSHA = hex.EncodeToString(h[:])
		s.Tasks = append(s.Tasks, b)
		if len(s.Tasks) > 12 {
			rows.Close()
			return errors.New("task ceiling")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = tx.Query(ctx, "SELECT DISTINCT node_id FROM photo.worker_registrations WHERE tenant_id=$1 AND enabled ORDER BY node_id", s.Tenant)
	if e != nil {
		return e
	}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			rows.Close()
			return e
		}
		s.Nodes = append(s.Nodes, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if e = validateThree(s); e != nil {
		return e
	}
	out := map[string]any{"schema": 1, "observed_at": s.ObservedAt, "head": s.Head, "tenant_id": s.Tenant, "project_id": s.Project, "collection_id": s.Collection, "style_version_id": s.Style, "style_config_sha256": s.StyleSHA, "run_id": s.Run, "output_collection_id": s.Output, "graph_version": s.Graph, "phase": s.Phase, "items": s.Items, "tasks": s.Tasks, "resource_node_ids": s.Nodes, "run_config_sha256": s.ConfigSHA, "source_revision": revision}
	// Canonical empty arrays, not null, keep future private binder schema closed.
	if s.Tasks == nil {
		out["tasks"] = []taskBinding{}
	}
	if s.Nodes == nil {
		out["resource_node_ids"] = []string{}
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
