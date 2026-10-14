package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Begin saves the immutable intent and full quota reservation without creating
// tasks, sealing the graph or activating disabled portrait operations.
func (s SQLLocalPortraitOwnerScope) Begin(ctx context.Context, requestRaw []byte, policySHA string, reservedBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := localPortraitOwnerConfig(requestRaw, policySHA, reservedBytes)
	if err != nil {
		return err
	}
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return database.InTransaction(bounded, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
        var queue *string
        if err:=tx.QueryRow(bounded,"SELECT photo.v32_local_portrait_queue($1::jsonb)",string(requestRaw)).Scan(&queue);err!=nil{return err}
        if queue==nil{return v2input.ErrExpired}
        config,err:=localPortraitOwnerConfigForQueue(requestRaw,policySHA,reservedBytes,*queue)
        if err!=nil{return err}
		var accepted bool
		if err := tx.QueryRow(bounded, "SELECT photo.begin_v28_local_portrait_draft($1,$2,$3)", string(config), policySHA, reservedBytes).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return bounded.Err()
	})
}

func localPortraitOwnerConfig(raw []byte, policySHA string, reservedBytes int64) ([]byte, error) {
 return localPortraitOwnerConfigForQueue(raw,policySHA,reservedBytes,"")
}

func localPortraitOwnerConfigForQueue(raw []byte, policySHA string, reservedBytes int64, queue string) ([]byte,error) {
 if queue!=""&&queue!="interactive" { return nil,v2input.ErrInvalid }
	if _, err := domain.ParseDigest(policySHA); err != nil {
		return nil, v2input.ErrInvalid
	}
	if len(raw) < 2 || len(raw) > 1048576 || reservedBytes < 1 || reservedBytes > 9007199254740991 {
		return nil, v2input.ErrInvalid
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 || request.Intent.ReservedBytes < 1 || request.Intent.ReservedBytes > 9007199254740991-65536-2097152 {
		return nil, v2input.ErrInvalid
	}
	if reservedBytes < request.Intent.ReservedBytes+65536+2097152 {
		return nil, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, raw) {
		return nil, v2input.ErrInvalid
	}
	value:=map[string]any{"project_id": request.Project, "input_snapshot_id": request.Snapshot, "v28_local_portrait": request}
 if queue!="" { value["queue_class"]=queue }
 body, err := canonical.Marshal(value)
	if err != nil || len(body) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	return body, nil
}
