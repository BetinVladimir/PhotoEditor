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
