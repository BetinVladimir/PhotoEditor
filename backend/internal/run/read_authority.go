package run

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/project"
)

// Correlated to runs alias r. UNION retains every project, rather than allowing
// one source lane to hide another project's restricted authority via COALESCE.
const runReadProjects = `
 SELECT project_id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND child_run_id=r.id
 UNION SELECT project_id FROM photo.v2_workflows WHERE tenant_id=r.tenant_id AND run_id=r.id
 UNION SELECT project_id FROM photo.editor_effect_builds WHERE tenant_id=r.tenant_id AND run_id=r.id
 UNION SELECT project_id FROM photo.collections WHERE tenant_id=r.tenant_id AND id=r.output_collection_id
 UNION SELECT project_id FROM photo.upload_sessions WHERE tenant_id=r.tenant_id AND ingest_run_id=r.id
 UNION SELECT project_id FROM photo.ingest_upload_bindings WHERE tenant_id=r.tenant_id AND run_id=r.id
 UNION SELECT s.project_id FROM photo.styles s WHERE s.tenant_id=r.tenant_id AND s.id=CASE WHEN r.config_snapshot->>'style_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (r.config_snapshot->>'style_id')::uuid END
 UNION SELECT s.project_id FROM photo.snapshots snap JOIN photo.training_sets t ON t.tenant_id=snap.tenant_id AND t.id=snap.training_set_id JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE snap.tenant_id=r.tenant_id AND snap.id=r.input_snapshot_id
 UNION SELECT a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=r.tenant_id AND v.id=CASE WHEN r.config_snapshot->>'asset_version_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (r.config_snapshot->>'asset_version_id')::uuid END
 UNION SELECT c.project_id FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 UNION SELECT a.project_id FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 UNION SELECT CASE WHEN r.config_snapshot->>'project_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (r.config_snapshot->>'project_id')::uuid END
`
const runReadConfigValid = `(NOT (r.config_snapshot ? 'project_id') OR r.config_snapshot->>'project_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') AND (NOT (r.config_snapshot ? 'asset_version_id') OR r.config_snapshot->>'asset_version_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')`

// Used before LIMIT so inaccessible rows cannot exhaust a page or leak counts.
const runReadVisible = `COALESCE((`+runReadConfigValid+`),false)
 AND EXISTS (SELECT 1 FROM (`+runReadProjects+`) rp WHERE rp.project_id IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM (`+runReadProjects+`) rp LEFT JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id=rp.project_id
 WHERE rp.project_id IS NOT NULL AND (p.id IS NULL OR p.archived_at IS NOT NULL OR
 (NOT $6::boolean AND p.visibility='restricted' AND NOT EXISTS (SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p.tenant_id AND acl.project_id=p.id AND acl.user_id=$7))))`

func authorizeRunRead(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID,id uuid.UUID, role domain.Role) error {
 return authorizeRunProjects(ctx,tx,who,tenantID,id,role,domain.Viewer)
}

func authorizeRunProjects(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID,id uuid.UUID, role,required domain.Role) error {
 var projects []uuid.UUID
 var valid bool
 err:=tx.QueryRow(ctx,`SELECT COALESCE((`+runReadConfigValid+`),false), ARRAY(SELECT rp.project_id FROM (`+runReadProjects+`) rp WHERE rp.project_id IS NOT NULL ORDER BY rp.project_id) FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`,tenantID,id).Scan(&valid,&projects)
 if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)}
 if err!=nil{return err}
 if !valid||len(projects)==0{return domain.NewError("NOT_FOUND",404)}
 for _,p:=range projects {if p==uuid.Nil{return domain.NewError("NOT_FOUND",404)};if _,err=project.Authorize(ctx,tx,tenantID,p,who.UserID,role,required,false);err!=nil{return err}}
 return nil
}
