package workflow

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type deliveryStageObjects map[string][]byte

func (o deliveryStageObjects) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	body, ok := o[object.Key]
	if !ok {
		return nil, fmt.Errorf("missing test object")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type deliveryStageReadDB struct {
	responses [][]byte
	calls     int
}

func (d *deliveryStageReadDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if d.calls >= len(d.responses) {
		panic("unexpected producer read")
	}
	raw := d.responses[d.calls]
	d.calls++
	return deliveryActivationRow{value: raw}
}

type deliveryStageRecordDB struct {
 args []any
	accepted bool
	calls    int
	producer string
	query    string
}

func (d *deliveryStageRecordDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.query = query
 d.args=append([]any(nil),args...)
	d.calls++
	if producer, ok := args[4].(string); ok { d.producer = producer }
	return deliveryActivationRow{value: d.accepted}
}
func deliveryTestRGBNPY(w, h int64) []byte {
	header := fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': (%d, %d, 3), }", h, w)
	padding := (16 - (10+len(header)+1)%16) % 16
	header += strings.Repeat(" ", padding) + "\n"
	out := append([]byte("\x93NUMPY\x01\x00"), byte(len(header)), byte(len(header)>>8))
	out = append(out, []byte(header)...)
	return append(out, make([]byte, w*h*3*4)...)
}
func TestDeliveryStageVerifierStreamsExactNPYAndRefusesCorruption(t *testing.T) {
	plan, parent, child, source, base := deliveryExportPlanFixture(t, func(c *exportpreset.Config) {
		c.Sharpen = &exportpreset.Sharpen{Radius: 1, Strength: .4, Threshold: .02}
	})
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	node, err := CompileDeliveryInitialNode(plan, parent, child, source, base.TargetICCSHA256, registry)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "corrupt_binary", "nonfinite", "wrong_shape", "foreign_pixel"} {
		t.Run(scenario, func(t *testing.T) {
			rgb := deliveryTestRGBNPY(image.Canvas.Width, image.Canvas.Height)
			if scenario == "nonfinite" {
				binary.LittleEndian.PutUint32(rgb[len(rgb)-4:], 0x7fc00000)
			}
			if scenario == "wrong_shape" {
				rgb = deliveryTestRGBNPY(image.Canvas.Width+1, image.Canvas.Height)
			}
			current := image
			current.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(rgb)}
			currentRaw, _ := canonical.Marshal(current)
			reportRaw, _ := canonical.Marshal(DeliveryDerivativeReport{Image: node.Inputs[0], Operation: "sharpen", Applied: true, Width: image.Canvas.Width, Height: image.Canvas.Height, MaxChannelDelta: 0, Warnings: []string{}})
			bodies := deliveryStageObjects{"rgb": rgb, "image": currentRaw, "report": reportRaw}
			result := DeliveryStageResult{TaskID: node.ID, ResultID: uuid.New(), Attempt: 1, Fence: 1}
			for _, entry := range []struct {
				key, role, media string
				id               uuid.UUID
			}{{"rgb", "working_rgb", "application/octet-stream", current.Pixels.ID}, {"image", "working_image", "application/json", uuid.New()}, {"report", "derivative_report", "application/json", uuid.New()}} {
				body := bodies[entry.key]
				result.Outputs = append(result.Outputs, DeliveryStageOutput{Ref: v2input.Ref{ArtifactID: entry.id, SHA256: renderDerivativeBytesSHA(body)}, Role: entry.role, Bucket: "photos", Key: entry.key, Version: "pinned-v1", Bytes: int64(len(body)), MediaType: entry.media})
			}
			if scenario == "corrupt_binary" {
				copy := append([]byte(nil), rgb...)
				copy[len(copy)-1] ^= 1
				bodies["rgb"] = copy
			}
			if scenario == "foreign_pixel" {
				result.Outputs[0].Ref.ArtifactID = uuid.New()
			}
			if scenario == "valid" {
				producerRaw, _ := json.Marshal(result)
				lease := delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID()}
				limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}
				for _, recovery := range []string{"unchanged", "revoked", "changed_fence"} {
					fresh := producerRaw
					if recovery == "revoked" {
						fresh = []byte("null")
					}
					if recovery == "changed_fence" {
						changed := result
						changed.Fence++
						fresh, _ = json.Marshal(changed)
					}
					db := &deliveryStageReadDB{responses: [][]byte{producerRaw, fresh}}
					loaded, loadErr := LoadVerifiedDeliveryStage(context.Background(), db, bodies, "photos", lease, plan, child, source, nil, base.TargetICCSHA256, registry, limits)
					if recovery == "unchanged" {
						if loadErr != nil || loaded == nil || db.calls != 2 {
							t.Fatal("verified loader", loadErr)
						}
						for _, accepted := range []bool{true, false} {
							tx := &deliveryStageRecordDB{accepted: accepted}
							recordErr := loaded.RecordInTransaction(context.Background(), tx, lease, plan, child, node.Operation, "photos")
							if (recordErr == nil) != accepted || tx.calls != 1 || tx.producer == "" {
								t.Fatal("receipt recording", accepted, recordErr)
							}
						}
						barrierTX := &deliveryStageRecordDB{accepted: true}
						if err := loaded.BeginContinuationInTransaction(context.Background(), barrierTX, lease, plan, child, node.Operation, "photos"); err != nil || !strings.Contains(barrierTX.query, "begin_v30_delivery_continuation") {
							t.Fatal("dedicated continuation barrier", err)
						}
						refusedBarrier := &deliveryStageRecordDB{accepted: false}
						if err := loaded.BeginContinuationInTransaction(context.Background(), refusedBarrier, lease, plan, child, node.Operation, "photos"); err == nil {
							t.Fatal("barrier refusal ignored")
						}
						wrongScope := &deliveryStageRecordDB{accepted: true}
						if err := loaded.RecordInTransaction(context.Background(), wrongScope, lease, plan, child, node.Operation, "foreign"); err == nil || wrongScope.calls != 0 {
							t.Fatal("foreign receipt scope accepted")
						}
						owned := loaded.Output()
						owned.ImageJSON[0] = 'x'
						if loaded.Output().ImageJSON[0] != '{' {
							t.Fatal("borrowed output bytes")
						}
						pin := loaded.Producer()
						pin.Outputs[0].Version = "changed"
						if loaded.Producer().Outputs[0].Version != "pinned-v1" {
							t.Fatal("borrowed producer evidence")
						}
					} else if loadErr == nil || loaded != nil {
						t.Fatal("stale producer accepted", recovery)
					}
				}
			}
			verified, err := VerifyDeliveryStageOutput(context.Background(), bodies, "photos", node, source, result)
			if scenario == "valid" {
				if err != nil || !bytes.Equal(verified.ImageJSON, currentRaw) {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid physical output accepted", scenario)
			}
		})
	}
}
