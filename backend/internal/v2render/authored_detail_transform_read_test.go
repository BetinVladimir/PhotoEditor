package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestAuthoredDetailTransformReadbackCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (SQLAuthoredDetailTransformReader{}).Read(ctx, auth.Identity{}, AuthoredInstructionSource{}, uuid.Nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := (SQLAuthoredDetailTransformReader{}).Read(context.Background(), auth.Identity{}, AuthoredInstructionSource{}, uuid.Nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestAuthoredDetailTransformReadbackOwnsReturnedWitnessBytes(t *testing.T) {
	proof := CurrentAuthoredDetailTransform{document: AuthoredDetailTransformDocument{Root: []byte(`{"root":1}`), Target: []byte(`{"target":1}`)}, proof: []byte(`{"attempt":1}`)}
	document := proof.Document()
	wire := proof.Proof()
	document.Root[0] = 'x'
	document.Target[0] = 'x'
	wire[0] = 'x'
	if string(proof.Document().Root) != `{"root":1}` || string(proof.Document().Target) != `{"target":1}` || string(proof.Proof()) != `{"attempt":1}` {
		t.Fatal("mutable caller acquired canonical proof storage")
	}
}

func TestAuthoredDetailTransformPhysicalClosureRefusesMissingCorruptAndClose(t *testing.T) {
	v, docs := detailMaterializationFixture(t)
	_ = v
	original := []byte("canonical independent mask bytes")
	ref, obj := authoredSegmentStreamFixture(original)
	pngRef, pngObj := authoredSegmentStreamFixture(original)
	var body map[string]json.RawMessage
	json.Unmarshal(docs[0].Root, &body)
	var mask map[string]json.RawMessage
	json.Unmarshal(body["mask"], &mask)
	mask["artifact"], _ = canonical.Marshal(ref)
	body["mask"], _ = canonical.Marshal(mask)
	root, _ := canonical.Marshal(body)
	locator := authoredDetailTransformLocator{Outputs: []authoredDetailTransformOutput{
		{authoredDetailTransformObject: authoredDetailTransformObject{Ref: ref, Bucket: "owned", Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes}, Role: "transformed_mask", MediaType: "application/octet-stream"},
		{authoredDetailTransformObject: authoredDetailTransformObject{Ref: pngRef, Bucket: "owned", Key: pngObj.Key, Version: pngObj.VersionID, Bytes: pngObj.Bytes}, Role: "transformed_mask_png", MediaType: "image/png"},
	}}
	for _, kind := range []string{"missing", "corrupt", "truncate", "close", "cancel", "foreign-mask", "extra-output"} {
		t.Run(kind, func(t *testing.T) {
			local := locator
			local.Outputs = append([]authoredDetailTransformOutput(nil), locator.Outputs...)
			reader := &authoredSegmentStreamReader{data: bytes.NewReader(original)}
			objects := &authoredSegmentStreamObjects{reader: reader}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "missing":
				local.Outputs = local.Outputs[:1]
			case "corrupt":
				reader.data = bytes.NewReader(bytes.Repeat([]byte("x"), len(original)))
			case "truncate":
				reader.data = bytes.NewReader(original[:len(original)-1])
			case "close":
				reader.closeErr = errors.New("close failure")
			case "cancel":
				reader.cancel = cancel
			case "foreign-mask":
				local.Outputs[0].Ref.ArtifactID = uuid.New()
			case "extra-output":
				local.Outputs = append(local.Outputs, local.Outputs[1])
			}
			err := (SQLAuthoredDetailTransformReader{Objects: objects, Bucket: "owned"}).verifyOutputs(ctx, local, root)
			if err == nil {
				t.Fatal("invalid physical closure accepted")
			}
			if objects.opens > 0 && reader.closeCount != 1 {
				t.Fatal("opened binary stream not closed exactly once")
			}
		})
	}
}

type authoredMaskClosureObjects struct {
	Data    []byte
	Readers []*authoredSegmentStreamReader
	Objects []objectstore.Object
}

func (o *authoredMaskClosureObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	r := &authoredSegmentStreamReader{data: bytes.NewReader(o.Data)}
	o.Readers = append(o.Readers, r)
	o.Objects = append(o.Objects, obj)
	return r, nil
}
func TestAuthoredDetailTransformPhysicalClosureStreamsBothExactVersions(t *testing.T) {
	_, docs := detailMaterializationFixture(t)
	data := bytes.Repeat([]byte("actual-byte-stream"), 10000)
	ref, obj := authoredSegmentStreamFixture(data)
	pngRef, pngObj := authoredSegmentStreamFixture(data)
	var body map[string]json.RawMessage
	json.Unmarshal(docs[0].Root, &body)
	var mask map[string]json.RawMessage
	json.Unmarshal(body["mask"], &mask)
	mask["artifact"], _ = canonical.Marshal(ref)
	body["mask"], _ = canonical.Marshal(mask)
	root, _ := canonical.Marshal(body)
	locator := authoredDetailTransformLocator{Outputs: []authoredDetailTransformOutput{
		{authoredDetailTransformObject: authoredDetailTransformObject{Ref: ref, Bucket: "owned", Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes}, Role: "transformed_mask", MediaType: "application/octet-stream"},
		{authoredDetailTransformObject: authoredDetailTransformObject{Ref: pngRef, Bucket: "owned", Key: pngObj.Key, Version: pngObj.VersionID, Bytes: pngObj.Bytes}, Role: "transformed_mask_png", MediaType: "image/png"}}}
	objects := &authoredMaskClosureObjects{Data: data}
	if err := (SQLAuthoredDetailTransformReader{Objects: objects, Bucket: "owned"}).verifyOutputs(context.Background(), locator, root); err != nil {
		t.Fatal(err)
	}
	if len(objects.Readers) != 2 {
		t.Fatal("did not consume both canonical binary outputs")
	}
	for i, r := range objects.Readers {
		if r.closeCount != 1 || r.maxRead > 65536 || objects.Objects[i].VersionID != obj.VersionID {
			t.Fatal("stream/version ownership changed")
		}
	}
}

func TestAuthoredDetailTransformWitnessContainsExactPhysicalDocuments(t *testing.T) {
	_, docs := detailMaterializationFixture(t)
	p := CurrentAuthoredDetailTransform{document: docs[0], proof: json.RawMessage(`{"attempt":1}`)}
	raw, err := p.Witness()
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || len(body) != 3 {
		t.Fatal("witness ABI changed")
	}
	var root, target string
	json.Unmarshal(body["root_jcs"], &root)
	json.Unmarshal(body["target_jcs"], &target)
	if root != string(docs[0].Root) || target != string(docs[0].Target) || string(body["proof"]) != `{"attempt":1}` {
		t.Fatal("physical content witness changed")
	}
	p.document.Root = []byte("not JSON")
	if _, err := p.Witness(); err == nil {
		t.Fatal("invalid content witness accepted")
	}
}
