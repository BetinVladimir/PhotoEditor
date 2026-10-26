---
id: O-9b
title: Apple Silicon worker package (launchd + installer + env template)
area: ops
size: S
depends: []
edit:
  - deploy/mac-worker/com.photoeditor.worker.plist (new)
  - deploy/mac-worker/install.sh (new)
  - deploy/mac-worker/worker.env.example (new)
  - deploy/mac-worker/README.md (new)
context:
  - deploy/env/rtx-worker.env.example
  - configs/capacity-metal-verification.json:1-20
verify:
  - python3 -c "import plistlib; plistlib.load(open('deploy/mac-worker/com.photoeditor.worker.plist','rb'))"
  - bash -n deploy/mac-worker/install.sh
---
## Spec
- `worker.env.example`: variables read by `WorkerConfig.from_env`: `APP_ENV=production`, `PHOTO_WORKER_ID=REPLACE-UUID`, `PHOTO_WORKER_CLASS=gpu`, `PHOTO_WORKER_BACKEND=mps`, `DATABASE_URL=postgresql://photo_worker_mac:REPLACE@db.photo.internal/photoeditor?sslmode=verify-full&sslrootcert=/usr/local/photo/tls/ca.crt`, `AMQP_URL=amqps://photo_worker_mac:REPLACE@queue.photo.internal:5671/photo`, `WORKER_AMQP_CA_FILE=/usr/local/photo/tls/ca.crt`, `WORKER_AMQP_CERT_FILE=/usr/local/photo/tls/worker.crt`, `WORKER_AMQP_KEY_FILE=/usr/local/photo/tls/worker.key`, `GATEWAY_ORIGIN=https://gateway.photo.internal:8443`, `GATEWAY_CA_FILE`, `WORKER_CERT_FILE`, `WORKER_KEY_FILE` (same files), `PHOTO_SCRATCH_ROOT=/usr/local/photo/scratch`, `PHOTO_MODEL_ROOT`, `PHOTO_MODEL_LOCK`, `PHOTO_CAPACITY_POLICY=/usr/local/photo/capacity.json`, `PHOTO_ICC_ROOT`, `WORKER_MAX_DOWNLOAD_BYTES=21474836480`, `WORKER_MAX_UPLOAD_BYTES=21474836480`, `WORKER_MAX_SCRATCH_BYTES=REPLACE`. Header comment: replace every REPLACE; keep real values out of version control.
- `com.photoeditor.worker.plist`: label `com.photoeditor.worker`, `ProgramArguments` = `["/usr/local/photo/venv/bin/photo-worker","run"]`, `EnvironmentVariables` is NOT used (secrets live in the env file); instead `ProgramArguments` runs through `/bin/zsh -lc 'set -a; . /usr/local/photo/worker.env; set +a; exec /usr/local/photo/venv/bin/photo-worker run'`; `KeepAlive` true, `RunAtLoad` true, `ThrottleInterval` 10, `StandardOutPath`/`StandardErrorPath` under `/usr/local/photo/log/`, `ProcessType` `Interactive` (keeps full GPU priority).
- `install.sh` (bash, `set -euo pipefail`, refuses to run as non-root unless `DRY_RUN=1`): creates `/usr/local/photo/{scratch,log,tls,models}` (mode 700 for `tls`), creates the venv with `uv venv --python 3.11 /usr/local/photo/venv`, installs the worker (`uv pip install --python /usr/local/photo/venv/bin/python "${WORKER_SRC:?path to workers dir}[ml,media]"`), copies the plist to `/Library/LaunchDaemons/` with root:wheel 644, `launchctl bootstrap system ...`, and prints next steps (copy certificates, fill env file, `photo-worker validate-config`). Every mutating command goes through a `run()` helper that only echoes when `DRY_RUN=1`.
- `README.md`: <= 40 lines: prerequisites (Apple Silicon, macOS, WireGuard client, chrony alternative: macOS system time sync), install, upgrade, logs, uninstall (`launchctl bootout`), pointer to `docs/ops/network-topology.md`.
