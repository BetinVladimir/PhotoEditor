# 11. Сборка, модели, ресурсы и эксплуатация

## Зависимости и модели

Выбор библиотек соответствует ТЗ2/21–25. P01 фиксирует точные совместимые версии и SHA на целевых платформах; проект не назначает непроверенные patch numbers. Python3.11 обязательный target, PyTorch2.x+совместимый torchvision; Go toolchain/modules; Expo SDK+React Native; Next.js+Node supported runtime; npm lock. Каждый Docker image по digest, MinIO из выбранного source commit и воспроизводимого build; upstream maintenance status проверяется при P01, не считать старую формулировку о репозитории доказательством текущего состояния.

`ModelLockEntry={id,architecture,weight_id,repository,revision,files[{path,sha256,bytes}],preprocessing_id,packages_lock_hash,code_license,weights_license,notices,redistribution_policy,backends{cpu,cuda,mps},doctor_report_hash}`. Значение backend unsupported/untested/passed с reason/report, release required paths passed. Без SHA/лицензии веса installer не устанавливает. `install_models(lock,selected_ids,offline_bundle?)` скачивает только из allowlisted exact revision, SHA до atomic local rename. Runtime network downloads forbidden/local_files_only. Крупные пакеты ставятся по запросу installer, соответствующий код всегда в поставке.

| ID | Точная архитектура/вариант | Preprocess |
|---|---|---|
| lraspp_mobilenet_v3_large | torchvision LRASPP_MobileNet_V3_Large_Weights.COCO_WITH_VOC_LABELS_V1 | person_guided_v1 long768, ImageNet mean/std |
| sfd | face-alignment sfd detector | long1024, pinned adapter |
| fan2d | face-alignment TWO_D flip_input=false float32 | crop256, explicit CPU compile off |
| keypointrcnn_resnet50_fpn | KeypointRCNN_ResNet50_FPN_Weights.COCO_V1 | min640 max1024 |
| dinov2_vits14 | DINOv2 ViT-S/14 | full frame padded518,384D L2 |
| segformer_b0_ade | nvidia/segformer-b0-finetuned-ade-512-512 | official processor512,id2label frozen |
| bisenet_face19 | zllrunning/face-parsing.PyTorch CelebAMask-HQ checkpoint | crop512; skin/eyes/mouth/hair; no tooth class |
| u2net | rembg u2net.onnx | official320; ONNX CPU Mac |
| mobilesam | TinyViT mobile_sam.pt | long1024 predictor padding |
| depth_anything_v2_small | depth_anything_v2_vits.pth | infer_image input518 |
| scunet_color_real_psnr | scunet_color_real_psnr.pth | sRGB float, tile512/256 halo64 |
| realesrgan_x2 / realesrgan_x4 | RealESRGAN_x2plus.pth / RealESRGAN_x4plus.pth RRDBNet | tile256 halo32; operation model alias resolved by scale before dispatch |
| big_lama | big-lama | ROI<=1024; FFT CPU fallback |
| sd15_inpaint | stable-diffusion-v1-5/stable-diffusion-inpainting |512² DDIM,batch1; lock UNet,VAE,CLIP,tokenizer,scheduler |

CPU reference path обязателен для image algorithms. MPS SFD/FAN/NMS/ROIAlign/FFT не объявляются рабочими по имени устройства — doctor каждой операции; CPU fallback bounded. ONNX CPU/GPU packages разделены, не устанавливать конфликтующие варианты одновременно. ModelPool держит одну тяжёлую модель, evicts before next; libvips cache≤512MiB.

`doctor(profile)->DoctorReport`: verify packages/model/profile hashes, run synthetic inference for each installed op on CPU and selected backend, finite/shape checks, compare tolerances, memory/disk/connectivity/SQL role checks, stop unavailable capabilities. `benchmark(dataset,profile)->BenchmarkReport`: cold/warm,per-stage p50/p95,RSS/VRAM/MPS,scratch,backend/fallback; build time отдельно. `validate-config` purely schema/hash; `migrate` explicit style migration; пользовательские analyze/apply Typer обращаются Go API, не локальный скрытый pipeline.

## Compose profiles

Сервисы: caddy, api, scheduler, publisher, event-consumer, maintenance, mail-worker, postgres, rabbitmq, minio, expo-web, portal, worker-cpu, worker-gpu (cuda optional), worker-media, worker-archive, worker-collection; mailpit dev only. Migrate/bootstrap one-shot jobs должны завершиться до API ready. Docker nonroot, read-only rootfs, tmp limited volumes, healthchecks/restart unless-stopped, graceful stop timeout>=lease+cleanup; не считать depends_on гарантом долговременной доступности.

`mac-cpu`: Docker VM≤24GiB; infra≤5GiB, heavy≤14,media/archive≤2,reserve≥3; host≈8 free. `mac-mps`: compose infra/media/archive≤7GiB +native worker≤14 =>app≤21; gateway loopback credentials, no Docker MPS promise. `cuda-12gb`: GPU concurrency1 VRAM≤9GiB; host16GiB total app≤10 (infra4+heavyhost4+media/archive2),host32 total≤20. Archive concurrency1,media1 default up to2 within total budget. Не назначать каждому контейнеру полный machine budget.

OOM profiles: sample65536→32768→16384;color1024→512→256;restoration512→256→128;ROI1024→768→512. Max2 reductions; after exhausted explicit CPU fallback only if doctor+RAM estimate fit, otherwise failed/review. Смена backend отдельная recorded attempt, общий automatic attempt cap6. GPU scheduling slot не освобождать до fenced task terminal/lease expiry; child process kill при hard timeout.

Репозиторий содержит исполнимый `deploy/compose.yaml` и отдельные multi-stage Dockerfiles API, универсального Python worker, Expo web и Next standalone. Образы и все base images обязательны как внешние build arguments с immutable digest; default credentials отсутствуют. One-shot migrate/storage/queue bootstrap являются health dependencies API, worker трафик идёт к отдельному mTLS object gateway, CUDA и SMTP включаются только профилями. `backend/cmd/photo-healthcheck` выполняет bounded loopback readiness probe и поддерживает проверяемый mTLS без вывода тела/секретов. `tests/platform/test_deploy_topology.py`, `make deploy-check` и `docker compose config` проходят. Сборка конкретных production images, выпуск сертификатов, native MPS worker и live restore/benchmark всё ещё требуют deployment-specific evidence.

## Caddy и ENV

APP_DOMAIN→Expo static+`/api/*` Go; PORTAL_DOMAIN→Next standalone3000+`/api/*` Go; STORAGE_DOMAIN→MinIO9000 без изменения Host/path/query. Public only80/443; DB/Rabbit/MinIO consoles private. Internal `/internal/*` API public Caddy не маршрутизирует. Static Expo SPA fallback не перехватывает API404. Portal CSP/no-store/no-referrer headers; CORS exact APP_ORIGIN/PORTAL_ORIGIN, expose ETag multipart, разрешённые PUT/GET methods. Presign формируется с public endpoint, worker доступ с internal endpoint.

Profiles `public_tls` ACME with persisted /data,/config; `local_tls` tls internal and explicit trust install; `local_http` только loopback dev, cookie flags отдельные. TLS profile выбирает startup script готовым Caddyfile, не вымышленной runtime директивой. Next standalone includes public/.next/static nonroot Node; fonts bundled, no external requests.

ENV schema mandatory groups: domains/origins/TLS; DATABASE_URL по роли +DB_POOL_MAX; AMQP_URL/prefetch/ack timeout; MinIO endpoints/bootstrap and runtime secrets; OTP_PEPPER/PAYLOAD_KEY/SMTP TLS/JWT/refreshttl; models/worker resource budgets; registration/quotas/retention; portal/share/archive/upload limits. Значения по ТЗ43, расширить `MAX_STYLE_BYTES=100000000`, `API_JSON_MAX_BYTES=1048576`, `OTP_RATE_*`, `WORKER_MTLS_*`, `PROFILE_LOCK_PATH`. Public Expo/Next vars только API path, не credentials. `Config.LoadEnv()->Config`: required/format/ranges/origin cross-validation fail fast; никаких default passwords. Secret flags используют `_FILE` mounted secrets, ENV mutually exclusive. SMTP только sysadmin.

## Observability, recovery и backup

slog JSON logs request/run/task/attempt,redaction; metrics queue age/depth,outbox lag,lease failures,duplicate commits,retries,memory/disk,quota,upload failures,archive bytes,thumbnail latency,SMTP failures. Labels operation/resource/status, не tenant UUID в публичном metrics endpoint. `/health/live` process, `/health/ready` dependencies для роли; liveness не зависит от краткого MinIO отказа. Admin dashboard показывает operational errors, не фото.

`Backup.Create`: maintenance stop new mutators/dispatch, await active commits/fence outstanding, take Postgres consistent dump+MinIO exact referenced versions manifest while mutations paused, persist locks/config/Caddy state and encrypted key backup; затем resume. Model weights можно восстановить offline bundle с hashes, не неизвестной сетью. `Restore`: empty isolated instance→objects/versions→DB→secret keys/locks→migrate compatible→reconcile running tasks to retries→verify sample SHA/RLS/shares/review/snapshot→only then route traffic. Тест restore обязателен; план RPO24h ежедневный backup, RTO4h инженерная цель до измерения.

GC hourly, upload expiry sweep5min, lease reaper5s,outbox reconciler10s; cron внутри maintenance role loop с leader advisory lock, не внешняя manual cleanup команда. Backup retention7 daily+4 weekly encrypted copies, параметры system settings. One-node Compose не HA.

Release: platform locks macOS-arm64-MPS/Linux-arm64-CPU/Linux-amd64-CPU/CUDA,SBOM,code+weights licenses,profile hashes,model install offline path,capability doctor report,tests+benchmarks,Expo local build iOS Xcode/Android SDK без обязательного EAS. Secret scanner и dependency audit дают отчёт с documented resolution; ни license, ни compatibility не считать закрытыми без evidence.

## Реализованный listener object gateway

`PHOTO_ROLE=object-gateway` запускает Go gateway.Run с mTLS RequireAndVerifyClientCert/TLS≥1.2, отдельными Ed25519 keys и role photo_gateway. Конфигурация перечислена в README. Чтение credentials через bounded regular file descriptor с O_NOFOLLOW/O_NONBLOCK; secret mode0600; unknown/duplicate JSON keys отклоняются. Startup проверяет обязательные SQL функции20–21, DB connection и включённое versioning bucket без создания bucket. Health также mTLS, handler max headers16KiB/read headers5s; streams имеют индивидуальные15s IO deadlines и task deadline. Shutdown15s отменяет request contexts. Startup code скомпилирован и unit-tested; настоящий запуск со всеми зависимостями пока не подтверждён из-за недоступной dev-БД/Docker.
