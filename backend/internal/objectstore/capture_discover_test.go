package objectstore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestCaptureDiscoveryExactKey(t *testing.T) {
	tenant, upload := uuid.New(), uuid.New()
	key, err := StagingKey(tenant, upload)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		rows []minio.ObjectMultipartInfo
		want string
		bad  bool
	}{
		{name: "unique", rows: []minio.ObjectMultipartInfo{{Key: key + "/neighbor", UploadID: "other"}, {Key: key, UploadID: "capture-id"}}, want: "capture-id"},
		{name: "empty", rows: []minio.ObjectMultipartInfo{{Key: key + "/neighbor", UploadID: "other"}}},
		{name: "ambiguous", rows: []minio.ObjectMultipartInfo{{Key: key, UploadID: "one"}, {Key: key, UploadID: "two"}}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discoverExactMultipart(context.Background(), key, func(context.Context) <-chan minio.ObjectMultipartInfo {
				rows := make(chan minio.ObjectMultipartInfo, len(tc.rows))
				for _, row := range tc.rows {
					rows <- row
				}
				close(rows)
				return rows
			})
			if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
				t.Fatalf("discovery: id=%q error=%v", got, err)
			}
		})
	}
}
