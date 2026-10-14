package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// SchedulerExportPersonOriginInputs reads only receipts sealed into the claimed
// export task. The returned object pins must be checked again after physical IO.
type SchedulerExportPersonOriginInputs struct {
	Claims SQLExportAdoptionClaimStore
	Bucket string
}

func (s SchedulerExportPersonOriginInputs) Read(ctx context.Context, claim ExportAdoptionClaim) ([]exportPersonOriginStoredInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Claims.DB == nil || claim.validate() != nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	current, err := s.Claims.CurrentExportAdoptionClaim(ctx, claim)
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, v2input.ErrInvalid
	}
	rows, err := s.Claims.DB.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type
 FROM photo.v30_export_adoption_person_origin_inputs($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		claim.Tenant, claim.Workflow, claim.Run, claim.Task, claim.Result,
		claim.Actor, claim.Key, claim.RequestSHA, claim.Token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inputs := make([]exportPersonOriginStoredInput, 0)
	for rows.Next() {
		if len(inputs) >= 5000 {
			return nil, v2input.ErrInvalid
		}
		var input exportPersonOriginStoredInput
		var bucket string
		if err := rows.Scan(&input.ref.ArtifactID, &input.ref.SHA256, &bucket,
			&input.object.Key, &input.object.VersionID, &input.object.Bytes,
			&input.object.ContentType); err != nil {
			return nil, err
		}
		input.object.SHA256 = input.ref.SHA256
		if err := validSchedulerPersonOriginInput(input, bucket, s.Bucket); err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	current, err = s.Claims.CurrentExportAdoptionClaim(ctx, claim)
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, v2input.ErrInvalid
	}
	return inputs, ctx.Err()
}

func validSchedulerPersonOriginInput(input exportPersonOriginStoredInput, bucket, expectedBucket string) error {
	if bucket != expectedBucket || !validImportRef(input.ref) ||
		input.object.Key == "" || input.object.VersionID == "" || input.object.VersionID == "null" ||
		input.object.Bytes < 2 || input.object.Bytes > 16384 ||
		input.object.ContentType != "application/json" || input.object.SHA256 != input.ref.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}
