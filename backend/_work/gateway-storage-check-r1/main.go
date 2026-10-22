package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/url"
	"os"
	"photoeditor/backend/internal/objectstore"
	"time"
)

func main() {
	ctx, c := context.WithTimeout(context.Background(), time.Minute)
	defer c()
	var objects []objectstore.Object
	raw, e := os.ReadFile(os.Getenv("OBJECT_LIST_FILE"))
	if e != nil {
		panic(e)
	}
	if json.Unmarshal(raw, &objects) != nil || len(objects) > 100 {
		panic("invalid scope")
	}
	s, e := objectstore.New(objectstore.ConfigFromEnv(true))
	if e != nil {
		panic("invalid storage configuration")
	}
	cfg := objectstore.ConfigFromEnv(true)
	u, _ := url.Parse(cfg.Endpoint)
	client, e := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: u.Scheme == "https", Region: cfg.Region})
	if e != nil {
		panic("client configuration")
	}
	failed := 0
	for i, o := range objects {
		check := s.Check(ctx, o)
		stat, statErr := client.StatObject(ctx, cfg.Bucket, o.Key, minio.StatObjectOptions{VersionID: o.VersionID})
		if statErr != nil {
			panic("stat unavailable")
		}
		fmt.Printf("{\"index\":%d,\"version_matches\":%t,\"bytes_match\":%t,\"media_matches\":%t,\"sha_metadata_present\":%t,\"sha_metadata_matches\":%t}\n", i, stat.VersionID == o.VersionID, stat.Size == o.Bytes, stat.ContentType == o.ContentType, stat.Metadata.Get("X-Amz-Meta-Sha256") != "", stat.Metadata.Get("X-Amz-Meta-Sha256") == o.SHA256)
		status := "PASS"
		category := ""
		if check != nil {
			status = "FAIL"
			category = fmt.Sprintf("%T", check)
			failed++
		}
		fmt.Printf("{\"index\":%d,\"status\":%q,\"error_type\":%q}\n", i, status, category)
	}
	fmt.Printf("{\"objects\":%d,\"failed\":%d}\n", len(objects), failed)
	_ = io.Discard
	if failed > 0 {
		os.Exit(1)
	}
}
