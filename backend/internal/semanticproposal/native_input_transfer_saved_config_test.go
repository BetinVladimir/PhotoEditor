package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeInputTransferSavedConfigRejectsAbsentAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if configuration, err := (SQLTransferSourceReader{}).restoreNativeInputTransferProtectionConfiguration(ctx, auth.Identity{}, Request{}, TransferPreparation{}); !errors.Is(err, context.Canceled) || configuration.TargetProtection != nil {
		t.Fatal("cancelled protection restoration reached IO", err)
	}
	metadata, err := (SQLTransferSourceReader{}).readNativeInputTransferSavedConfig(ctx, auth.Identity{}, Request{}, TransferPreparation{})
	if !errors.Is(err, context.Canceled) || metadata.params != nil {
		t.Fatalf("cancelled saved reader: %v", err)
	}
	metadata, err = (SQLTransferSourceReader{}).readNativeInputTransferSavedConfig(context.Background(), auth.Identity{}, Request{}, TransferPreparation{})
	if !errors.Is(err, v2input.ErrInvalid) || metadata.params != nil {
		t.Fatalf("unscoped saved reader: %v", err)
	}
	for _, raw := range [][]byte{nil, []byte("{}"), []byte("{")} {
		metadata, err := decodeNativeInputTransferSavedConfig(raw, "", uuid.New(), auth.Identity{}, Request{}, TransferPreparation{})
		if err == nil || metadata.params != nil || metadata.snapshot.ArtifactID != uuid.Nil {
			t.Fatalf("invalid config exposed pins: %v", err)
		}
	}
}

func TestNativeInputTransferSavedConfigBindsOriginalOrdinalPins(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{ActorID: who.UserID, WorkflowID: uuid.New()}
	request.Intent.TargetSnapshotID = uuid.New()
	project := uuid.New()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	configuration := TransferPreparation{SourceProtection: ref(), TargetProtection: []v2input.Ref{ref(), ref()}, Qualification: v2input.SemanticQualificationPin{PolicyID: uuid.New()}}
	snapshot := ref()
	config := v2input.SemanticDraftConfig{ProjectID: project, SnapshotID: request.Intent.TargetSnapshotID, Semantic: v2input.SemanticFrozenNodes{AuthSessionID: who.SessionID, Snapshot: &snapshot, SnapshotDigest: strings.Repeat("b", 64), Qualification: &configuration.Qualification}}
	for i := 0; i < 2; i++ {
		task, _ := v2input.TaskID(request.WorkflowID, "semantic.transfer", i)
		param := ref()
		config.Semantic.Nodes = append(config.Semantic.Nodes, v2input.SemanticFrozenNode{TaskID: task, ParamsArtifactID: param.ArtifactID, ParamsSHA: param.SHA256, SourceProtection: configuration.SourceProtection, TargetProtection: configuration.TargetProtection[i]})
	}
	raw, _ := canonical.Marshal(config)
	sha, _ := canonical.Hash(config)
	saved, err := decodeNativeInputTransferSavedConfig(raw, sha, project, who, request, configuration)
	if err != nil || saved.snapshot != snapshot || len(saved.params) != 2 || saved.params[1].ArtifactID != config.Semantic.Nodes[1].ParamsArtifactID {
		t.Fatalf("original config refused: %v", err)
	}
	batchConfig := config
	batchConfig.QueueClass = "batch"
	batchRaw, _ := canonical.Marshal(batchConfig)
	batchSHA, _ := canonical.Hash(batchConfig)
	if got, err := decodeNativeInputTransferSavedConfig(batchRaw, batchSHA, project, who, request, configuration); err != nil || got.snapshot != snapshot { t.Fatal("batch saved transfer refused", err) }
	if got, err := decodeNativeInputTransferProtectionConfiguration(batchRaw, batchSHA, project, who, request, configuration); err != nil || len(got.TargetProtection) != 2 { t.Fatal("batch protection config refused", err) }
	dynamic := ownTransferPreparation(configuration)
	dynamic.SourceProtection = v2input.Ref{}
	dynamic.TargetProtection = nil
	restored, err := decodeNativeInputTransferProtectionConfiguration(raw, sha, project, who, request, dynamic)
	if err != nil || restored.SourceProtection != configuration.SourceProtection || len(restored.TargetProtection) != 2 || restored.TargetProtection[1] != configuration.TargetProtection[1] || dynamic.SourceProtection != (v2input.Ref{}) || dynamic.TargetProtection != nil {
		t.Fatal("original per-request protection locators refused or aliased", err)
	}
	restored.TargetProtection[0] = ref()
	if configuration.TargetProtection[0] != config.Semantic.Nodes[0].TargetProtection {
		t.Fatal("restored locators mutated original configuration")
	}
	mixed := config
	mixed.Semantic.Nodes = append([]v2input.SemanticFrozenNode(nil), config.Semantic.Nodes...)
	mixed.Semantic.Nodes[1].SourceProtection = ref()
	mixedRaw, _ := canonical.Marshal(mixed)
	mixedSHA, _ := canonical.Hash(mixed)
	if denied, err := decodeNativeInputTransferProtectionConfiguration(mixedRaw, mixedSHA, project, who, request, dynamic); err == nil || denied.TargetProtection != nil {
		t.Fatal("mixed original source protection accepted")
	}
	for _, mode := range []string{"session", "ordinal", "duplicate-params", "snapshot", "protection", "hash", "queue_interactive", "queue_training"} {
		changed := config
		changed.Semantic.Nodes = append([]v2input.SemanticFrozenNode(nil), config.Semantic.Nodes...)
		switch mode {
		case "queue_interactive":
			changed.QueueClass = "interactive"
		case "queue_training":
			changed.QueueClass = "training"
		case "session":
			changed.Semantic.AuthSessionID = uuid.New()
		case "ordinal":
			changed.Semantic.Nodes[0].TaskID = changed.Semantic.Nodes[1].TaskID
		case "duplicate-params":
			changed.Semantic.Nodes[1].ParamsArtifactID = changed.Semantic.Nodes[0].ParamsArtifactID
		case "snapshot":
			changed.SnapshotID = uuid.New()
		case "protection":
			changed.Semantic.Nodes[0].TargetProtection = ref()
		}
		raw, _ := canonical.Marshal(changed)
		sha, _ := canonical.Hash(changed)
		if mode == "hash" {
			sha = strings.Repeat("c", 64)
		}
		saved, err := decodeNativeInputTransferSavedConfig(raw, sha, project, who, request, configuration)
		if err == nil || saved.params != nil {
			t.Fatalf("accepted %s", mode)
		}
		if denied, err := decodeNativeInputTransferProtectionConfiguration(raw, sha, project, who, request, configuration); err == nil || denied.TargetProtection != nil {
			t.Fatalf("protection restoration accepted %s", mode)
		}
	}
}
