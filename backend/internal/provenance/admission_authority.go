package provenance

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

type SigningAdmissionTransaction interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckSigningAdmission runs inside the caller's API transaction after ledger
// and quota locks. It pins current source/lineage/policy rows against GC. It
// neither creates a workflow nor attests native resource measurements itself.
func CheckSigningAdmission(ctx context.Context, tx SigningAdmissionTransaction, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	i := prepared.intent
	if tx == nil || who.UserID != i.actor || who.SessionID != i.session || ready.Tenant != i.tenant || ready.Project != i.project || ready.ExportID != i.export || ready.Object.ContentType != prepared.mime {
		return ErrInvalid
	}
	params, err := DecodeSignParams(i.params)
	if err != nil {
		return err
	}
	if params.Export != ready.Ref {
		return ErrInvalid
	}
	pins, err := signingLineageStoragePins(who, ready, params.Lineage, i.lineage, bucket)
	if err != nil {
		return err
	}
	task := prepared.compiled.Task()
	doc, body := prepared.compiled.ParamsDocument()
	if task.ID != i.task || task.Operation != "provenance.sign" || task.OperationVersion != "1" || task.ResourceClass != "cpu" || task.ParamsArtifactID != doc.ArtifactID || task.ParamsSHA != doc.SHA256 || bodyDigest(body) != doc.SHA256 || string(body) != string(i.params) {
		return ErrInvalid
	}
	digest, err := canonical.Hash(prepared.compiled.Inputs())
	if err != nil || digest != task.InputDigest {
		return ErrInvalid
	}
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_admission_ready($1,$2,$3,$4,$5::jsonb,$6,$7)`, i.tenant, who.UserID, who.SessionID, i.export, string(pins), bucket, string(body)).Scan(&accepted); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}
