# 12. Верификация и критерии завершения

## Уровни и команды будущего monorepo

`make contracts-check`: regenerate Pydantic JSON Schemas/TS SDK/sqlc и no diff; validate registry и legacy hash. `make test-unit`: legacy/Python/Go unit и contract suites. `make test-integration`: live PostgreSQL/MinIO/Rabbit integration suite с real role credentials. `make test-fault`: lease/dead-letter/retry fault scenarios. `make test-security`: gateway/capability/tenant local checks, live RLS/admission/gateway/download checks и security-sensitive Go packages. `make test-web`: portal tests/build и photographer tests/typecheck/web export. `make test-native`, `make benchmark PROFILE=...`, `make test-restore`, `make test-visual`, `make test-hardware` сначала выполняют доступные repo-local проверки, затем fail-closed проверяют внешний evidence через `scripts/verification-gate.py`. Без evidence они сохраняют `work/evidence/gates/<gate>.json` со статусом `blocked_external` и завершаются кодом 2; unavailable gate никогда не считается PASS.

## Обязательные сценарии

| ID | Триггер | Проверяемый результат |
|---|---|---|
| DB01 | пустая DB→00011; повтор migrate | stable schema, seed no duplicates, правильные grants |
| DB02 | две tenants, чужие FK/UUID, без SET LOCAL | no read/write, SQL constraint reject, pool context не течёт |
| DB03 | duplicate version/hash mutations | immutable triggers; переименование style не меняет versions |
| AUTH01 | OTP wrong5/expired/replay/concurrent verify | consumed once; generic errors; no secret logs |
| AUTH02 | refresh twice/logout/role revoke | reused family revoked, следующий access request denied |
| SHARE01 | every combination view/download/upload | upload-only no catalog; blind download-only no metadata; mutation denied |
| SHARE02 | revoke после presign/parts | API deny immediately, GET<=60s, PUT<=600s staging only, no complete |
| UP01 | два concurrent quota reserves на границе | один success, counters never over limit/negative |
| UP02 | complete network failure до/после S3 complete | reconcile one asset, no lost quota, digest verified |
| UP03 | forged MIME,huge dimensions,CMYK,SVG,bomb,ICC missing | bounded failure, no pipeline acceptance |
| IMM01 | apply дважды same key и с новым key | same run/collection first; distinct new result second |
| IMM02 | source membership mutation during apply | snapshot stable; source hashes untouched; output no inherited shares |
| IMM03 | selected/all_change_selected/HDR | correct item counts; unchanged versions reused membership; bracket one output provenance all sources |
| STYLE01 | one/multiple pairs,unconfirmed,all invalid | fixed unvalidated; no invalid ready style; explicit confirmations |
| STYLE02 | v1 apply then save v2/archive style | run keeps exact v1 hash; reload/export/import identity canonical |
| REVIEW01 | duplicate/stale accept,reject generation | no double gate advancement; exact candidate only; final accepted/base branch |
| BULK01 | external revision change between chunks | completed chunks retained, remaining conflict, per-item report |
| DL01 | snapshot then add/remove items | ZIP original ordered snapshot, intent rechecks current permission/publication |
| DL02 | ZIP>4GiB,>5000 files,>10GiB,one oversized | ZIP64, split volumes, no omissions/duplicates; oversized explicit separate |
| DL03 | missing input/disk full/archive expiry | failed no partial ready, reservations cleanup, new job after expiry |
| UI01 | draft changes while preview running | stale response discarded; SaveEdits new collection |
| UI02 | 5000 photos,5 guest sessions,AI+ZIP | bounded DOM/preload, no original downloads, metadata p95<=500ms warm LAN |
| UI03 | RU/BG/EN themes web/native offline | no raw keys, flows recover without lost server work |
| GC01 | live snapshot/artifact lease while deletion | never deletes protected refs; mark/sweep attach race prevented |
| RESTORE01 | backup restore isolated instance | checksum/auth/RLS/snapshot/review preserved, orphan recovery safe |

## Fault matrix

Inject process kill после object upload до SQL commit; после SQL commit до Rabbit ack; publisher после confirm до sent_at; event duplicate; lease expiry during inference; disconnect DB/Rabbit/MinIO; malformed message/unknown operation; heartbeat stalled; cancel concurrent commit; stale result после retry; download crash during ZIP close/upload/commit. Для каждого: допускается повтор вычисления, максимум один canonical result per task и один output per source ordinal; outbox не теряется; failed/cancelled не публикуется. Проверять real DB/queue, не одни mocks. Старый worker не может «победить» новый fence. Rabbit ACK timeout не является lease authority.

## Численные gates

N01 synthetic ColorProgram: ≥95% пар medianΔE<=1,p95<=3,SSIM>=.99. N02 реальные reconstruction≥90%: median<=2,p95<=6,SSIM>=.98. N03 independent test≥80%: median<=3,p95<=8,SSIM>=.97; median dataset improvement vs identity≥20%, photographer accept/minor global fix≥80%. Набор≥60пар/6съёмок, split sessions≈60/20/20, immutable закрытый test.

N04 CPU/MPS/CUDA same style p95ΔE<=.2,max<=1 vs CPU; byte identical encodings не требуются. N05 identity/tiled max absolute work RGB<=2/65535 без ICC roundtrip; ICC separate reference patches, alpha0/1,orientation1..8,32/64 float reference trilinear boundaries,nonlinear LUT corner tests. N06 no hidden color normalization fixed single-pair.

N07 beauty≥30pairs,geometry≥30pairs split people/sessions; synthetic warp median landmark error<=.5% ROI diagonal; protected/outside influence<=2/65535; detJ>=.5; strength0 exact bypass. ≥90% auto-accepted portraits visually no wax/seams/double/warped background; skipped denominator публикуется. Defects outside accepted mask untouched.

N08 extended≥500frames/10sessions,100similar groups,50portraits,30complex masks,20HDR,30generation tasks. Cull top3 contains photographer pick≥90% groups,false reject<=2%. Series median brightness deviation reduction≥20%,clipped increase<=.5pp. Person/sky/object mIoU>=.9 supported dataset; crop protected landmarks success≥95%. Denoise median SSIM not worse clean reference; masks/hair/textures human QA. All generation final candidates recorded accepted and outside mask+feather unchanged before later exports.

P01 performance: 100×45MP base and 100×24/45MP extended на обеих машинах; no OOM/leaks/source change/external AI. Cold/warm p50/p95 per stage, peak RSS/VRAM/MPS/scratch. Apply initial target<=60s/45MP and analyze<=10min/pair — engineering objectives, final SLA по measured report. Каждый heavy module CPU/CUDA/MPS fallback измеряется, unavailable backend не скрывать. GPU budget9GiB, общие budgets11. 4x preflight учитывает pixel cap, output streaming test на допустимом source size.

## Test fixtures и evidence

`fixtures/manifest.json`: dataset_id,license/consent,source_sha,session_id,person_group,split,camera/profile/native_resolution,is_synthetic,expected_supported_operations. Tests не используют production personal photos. Старые6JPEG only legacy smoke, не ground truth. Новый model не имеет доступа к test labels при настройке thresholds. Reference reviewer decisions immutable actor/date/reasons.

Каждый gate сохраняет command,commit,environment locks,exit code,artifact hashes,metrics,thresholds,failed cases,backend и skipped counts. `quality=null` при отсутствии измерения, никогда zero. Недоступный RTX/эталоны →blocked_external gate, не PASS. Документационный аудит выполнил только22 legacy tests; все целевые gates ожидают реализации.


### Воспроизводимая synthetic квалификация fitting backend

`make check-fitting-backends` выполняет scripts/check-fitting-backends.py в repo-local Python environment. Fixture seed2026:512 training RGB,256 независимых holdout RGB, известные global gains и local person/background EV/saturation, person probabilities .9/.1. Используются FitOptions defaults400/600/200,3 initializations; early stopping действует по основному контракту. Для каждого доступного CPU/MPS/CUDA требуется уменьшение median и p95 DeltaE00 более чем вдвое против identity. Для GPU дополнительно p95 различия с CPU<=.5 DeltaE00. Deadline900s/backend; отказ и недоступность сохраняются раздельно. Exit0 означает прохождение доступных backend; unavailable не означает passed.

Отчёт work/evidence/fitting-backends.json включает версии Torch, тип хоста, seed/размеры fixture, хеши source algorithms, фактические options, reconstruction metrics, history, initialization и CPU comparison. Команда не изменяет worker registrations, qualified_operations или doctor_report_sha256 и не устанавливает веса моделей.

Фактический прогон текущего хоста: CPU8.57s, median0.10548/p950.24892; MPS27.26s, median0.10557/p950.24885; MPS-vs-CPU p950.00048291. Local fit сохранён на обоих backend, initialization1. CUDA unavailable. Это512/256 synthetic samples, а не full65536 production benchmark, фото-корпус или квалификация ML моделей. MPS driver allocation после завершения сохранён как after-value, не peak. Сквозной broker/SQL/object-storage запуск остаётся отдельной проверкой.


## Формат внешнего evidence

Файл передаётся переменной соответствующей цели: `NATIVE_EVIDENCE`, `BENCHMARK_EVIDENCE`, `RESTORE_EVIDENCE`, `VISUAL_EVIDENCE` или `HARDWARE_EVIDENCE`. JSON обязан содержать `gate`, `status: "PASS"`, текущий `source_commit`, фактические `command`, `environment`, `exit_code: 0`, непустые `artifacts`, `metrics`, `thresholds`, а также `failed_cases` и `skipped_count`. Для benchmark поле `profile` обязано совпадать с `PROFILE`. Валидатор отклоняет stale commit, чужой gate, пустые метрики/пороги и любой неуспешный статус.
