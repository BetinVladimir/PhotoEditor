# video-demo

Runnable DEMO of the video subsystem: upload → probe → montage → render → delivery link → client viewer.

It serves the **real** video API (`internal/video/api`), the real portal share session and an in-process worker
(`internal/video/worker`, real ffmpeg). The only thing replaced is login: there is one built-in dev user.
Because of that it listens on loopback only and refuses any other `-addr`. It connects as a database superuser and
narrows each pool with `SET ROLE` (`photo_api`, `photo_scheduler`, `photo_worker`, `photo_portal`), so every call runs
under its production role. Never deploy it; production serves the same routes from `cmd/photo`
(enabled by `VIDEO_STORE_DIR` + `VIDEO_SCHEDULER_DATABASE_URL`).

```
go run ./cmd/video-demo \
  -dsn 'host=/path/to/socket port=55433 user=photo_test dbname=scratch_v' \
  -store /tmp/video-demo [-encoder h264_nvenc] [-ffmpeg ffmpeg -ffprobe ffprobe]
```

Needs a database with migrations up to 02706. Open http://127.0.0.1:8088. Library: upload a clip, wait for `ready`.
Editor: Add clips, trim, Render. Delivery: Publish & create link; the link opens the client viewer.
State: `<store>/objects` (files), `<store>/pepper` (MAC key of share secrets; keep it or issued links stop working).
The UI is plain HTML/JS embedded from `ui/`; the studio keeps its last sequence/render/links in the browser's localStorage.

## Client page in apps/portal

The real portal page `/{locale}/v/{share_id}#secret` plays a delivery from this server. Start the demo with the portal origin and the portal
against it:

    go run ./cmd/video-demo -dsn '...' -store /tmp/video-demo -portal-origin http://127.0.0.1:3000
    cd ../apps/portal && NEXT_PUBLIC_API_ORIGIN=http://127.0.0.1:8088 npx next dev -H 127.0.0.1 -p 3000

`-portal-origin` only adds CORS for that origin and a placeholder CSRF bootstrap (the demo has no CSRF of its own).

## MinIO / presigned multipart uploads

`-s3 host:port,access,secret[,public-host:port]` stores objects in an S3-compatible bucket (`photo-video`, created and
versioned on start) and makes the library upload large files in 32 MiB parts straight to the store through presigned
URLs (`POST /video/uploads/{u}/multipart`, `/parts`, `/complete`). The server lists the parts the store holds,
assembles the object and refuses it when size or sha256 differ from the declaration. Integration test:
`VIDEO_E2E_MINIO=host:port,access,secret go test -tags videoe2e ./internal/video/api -run Multipart`.
