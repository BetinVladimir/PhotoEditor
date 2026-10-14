package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

// Cross-language formulas must agree against a database with current migrations.
func TestMultiResourcesSQLMatchesGo(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	for _, mode := range []string{"planar", "cylindrical", "focus2", "focus30"} {
		t.Run(mode, func(t *testing.T) {
			p, originals := multiOriginals(t)
			operation := "optical.panorama"
			var params []byte
			if mode == "planar" || mode == "cylindrical" {
				p.Projection = mode
				params, _, err = p.Compile()
			} else {
				operation = "optical.focus_stack"
				if mode == "focus30" {
					for len(originals) < 30 {
						originals = append(originals, append([]byte(nil), originals[0]...))
						binding := p.Frames[0]
						binding.AssetVersionID = uuid.New()
						p.Frames = append(p.Frames, binding)
					}
				}
				focus := FocusStackParams{SchemaVersion: 1, Frames: append([]v2input.SessionVersionBinding(nil), p.Frames...), ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: p.Calibration, MotionPolicy: "review"}
				params, _, err = focus.Compile()
			}
			if err != nil {
				t.Fatal(err)
			}
			expected, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			sources := make([]string, len(originals))
			for i, raw := range originals {
				sources[i] = string(raw)
			}
			sourceJSON, err := canonical.Marshal(sources)
			if err != nil {
				t.Fatal(err)
			}
			var raw []byte
			if err := conn.QueryRow(ctx, "SELECT photo.v29_multi_resource_estimate($1,$2::jsonb,$3::jsonb)", operation, string(params), string(sourceJSON)).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var actual MultiResourceEstimate
			if json.Unmarshal(raw, &actual) != nil || actual != expected {
				t.Fatalf("SQL=%s Go=%+v", raw, expected)
			}
		})
	}
}
