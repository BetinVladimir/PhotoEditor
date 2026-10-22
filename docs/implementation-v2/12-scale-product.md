# Продукт, эксплуатация и масштабирование

Целевой продукт — облачная/локальная рабочая среда для фотографа и студии: обработка серии в едином стиле, подтверждаемые персональные коррекции, контроль съёмки, профессиональные маски/слои, согласование и выдача. Это продуктовая гипотеза, не доказательство превосходства над всеми редакторами. Заявляемые возможности ограничены qualification matrix; Photoshop-compatible round-trip, все камеры и восстановление скрытых деталей не обещаются.

## Этап A: RTX3060 12 GiB, RAM32 GiB, Ryzen5600

Один control-plane, PostgreSQL, очередь, object storage, один GPU worker, отдельные CPU и signer workers. GPU concurrency=1; CPU photo concurrency=2, CPU threads/task=2; API max in-flight heavy admissions=8, остальное429. Стартовые бюджеты: GPU model+activation≤9.5GiB; host photo workers≤16GiB aggregate, control-plane≤8GiB, оставшиеся8GiB для OS/cache. Это admission limits, не измеренные benchmark.

Scratch обязательный отдельный volume; reserve до запуска: суммарный worst-case tile/intermediate estimate +20%. Free space <15% или reservation failure →503 STORAGE_CAPACITY, никаких частичных full-image allocations. Tiles512, halo по operation contract; input>200MP reject. ModelPool хранит один GPU model resident, unload/synchronize перед сменой. OOM: fence result, retry один раз с qualified smaller tile; если нет qualified variant →RESOURCE_EXHAUSTED. Silent CPU fallback запрещён.

Queue classes interactive, capture, batch, training; deficit round robin weights4,3,2,1, cost=max(1,estimated_gpu_ms/1000), unknown estimate charged1800. Within class fair tenant rotation, FIFO(created_at,id). Training не preempt внутри kernel; checkpoint tile boundary≤2s где возможно. После10min ожидания elevate на один класс до batch. Не больше2 active queued heavy jobs/tenant по умолчанию; owner может поменять server config. На single GPU p95 capture≤5s — цель при свободном compatible model, очередь и model load учитываются отдельно.

AdmissionBudget(plan) возвращает GPU bytes, host bytes, scratch bytes, output bytes, expected_cost. Резервирование в DB, не process memory. ReleaseBudget(run,terminal_key) idempotent. Scheduler может назначить только worker с exact operation/version/model/calibration/device qualification.

## Этап B: несколько compute-узлов

API остаётся stateless; два экземпляра за load balancer, outbox обеспечивает доставку. Durable state только PostgreSQL/object storage. GPU worker не монтирует local path другого узла: получает artifacts через gateway и declared capabilities. CPU/GPU/signer worker pools независимы. Общая lease/fence модель сохраняется; локальный filesystem — cache/scratch, не источник истины.

Worker heartbeat каждые10s, offline после30s; lease renew каждые10s, timeout60s. Истёкшая lease допускает новый attempt, старый fenced. Autoscale: scale up при oldest eligible wait>30s в течение3min либо reserved utilization>75% 5min; scale down только после15min idle, drain перед stop. Эти правила требуют измеренного task cost, не зависят от числа HTTP запросов.

Affinity учитывает resident model как optimization, но не нарушает tenant fairness. Cache key включает input SHA, params JCS SHA, renderer, model/runtime/backend qualification; cache refs tenant-scoped, не раскрывают чужие artifacts. Нельзя делить фото-кеш между tenants через угадываемый SHA endpoint.

## Этап C: большой объём

Сначала измерить DB lock wait, queue age, object throughput и p95 admission. Partition append-only audit/events/receipts по created_at при росте размеров; mutable task lease таблицы не шардировать до доказанного bottleneck. Reads галереи можно вынести на replica; authorization, CAS, quota, admission, revoke всегда primary. Snapshot/finalizer не читают lagging replica.

Object multipart, bounded concurrency, CDN только для разрешённых derivative delivery; originals private. Backpressure на upload/exports независим от GPU. Масштабирование БД по tenant shards — отдельная будущая миграция с tenant placement directory, home shard для всей транзакции; cross-shard workflow запрещён. Не вводить распределённые транзакции ради одного проекта.

## Локальная студия и residency

Один tenant/project имеет один authoritative region/appliance. Raw assets, face embeddings, training и keys остаются там; облачный control-plane получает health/counters без фото. Agent отправляет в явно выбранный appliance endpoint. Офлайн SQLite journal хранит только очередь upload и локальные edits; серверные consent/approval не подтверждаются офлайн. Reconnect использует idempotency+CAS, не last-write-wins.

Автоматическая двусторонняя синхронизация двух authoritative серверов не входит в v2. Экспорт проекта — explicit frozen bundle с manifest, rights и checksums; импорт создаёт новые IDs и consent revoked. Это предотвращает неразрешённое объединение histories и identity.

## Экономика, наблюдаемость, восстановление

До коммерческого дополнения от 2026-10-18 v2 ограничивалась metering и квотами без платёжного провайдера. Для платного SaaS-запуска теперь принят отдельный [план биллинга со Stripe](billing-prelaunch-plan-2026-10-18.md): каталог планов/цен/лимитов и topup offers, tenant billing profile, скрытый Custom, Checkout и формы кабинета. План спроектирован, но не реализован; открытие продаж требует B0–B4. Это расширение коммерческого объёма, а не утверждение о готовности текущего кода.

Использовать существующий quota ledger: reserve estimated bytes/cost, settle canonical adopted result один раз; cancelled/failed не создают usage за неполученный output, фактический compute учитывать отдельно как infrastructure cost. Внутренние единицы storage byte-days, successful export bytes, measured GPU milliseconds сохраняются; коммерческие standard_photo и оплаченные buckets интегрируются с тем же admission/reservation/settlement. Второй независимый механизм квот не создавать.

Metrics: task wait/runtime by operation/version/class, OOM, admission rejects, stale fence, retries, adoption lag, purge lag, consent cleanup backlog, cache hits, protected-pixel failures. Labels не содержат tenant/person IDs (детализация в authorized audit). Trace request→run→task→attempt→artifact; redaction prompt/email/GPS. Alert: no heartbeat30s, queue age>5min, outbox lag>60s, disk<15%, oldest pending consent cleanup>24h, DB restore test failed.

Backups: daily base + WAL continuous, versioned object manifests, keys отдельным encrypted backup с access audit. Проектные RPO≤15min, RTO≤4h, подтверждаются quarterly restore drill на отдельной среде. Restore сверяет DB artifact refs с объектами, paused scheduler, revocation epochs и deletion tombstones; objects с удалённых tenants не воскресают. Включение writers только после reconcile и smoke test old/new Apply.

Рост допускается по доказательствам: этап A capacity report на реальном железе; B двухузловый kill/retry/fairness test; C load test с рабочими размерами metadata. Конкретные throughput/количество платящих студий до benchmark неизвестны.

### Отдельный биллинг диска —18.10.2026

Уточнение коммерческого плана: во всех тарифах задать включённый диск2/10/30/100/250/500ГБ; Custom — явно. Отдельные recurring storage add-ons и счётчики фото/байтов/исходящего трафика; оригиналы и готовые клиентские сессии хранятся до ручного удаления, прежний30дневный TTL отменён. Диск поддерживает storage-only после отмены AI. Цены, маржа, Stripe items, seed45resource offers и сценарии overquota — в [плане биллинга](billing-prelaunch-plan-2026-10-18.md). Годовой финансовый план обновлён с отдельным Storage MRR. Статус — документация, реализация не начата.
