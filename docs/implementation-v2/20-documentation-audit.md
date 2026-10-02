# Повторное ревью документации: протокол v2.2

Дата: 2026-09-30. Объект проверки — specification/code-generation readiness, не готовность приложения. Изменения ограничены docs/implementation-v2. Код продукта и production migrations не исполнялись и не изменялись.

## Итерации

1. Сквозной review исходных пяти замечаний: отсутствующий training render closure;5000 targets против batch100; ZIP вне output policy; потерянная связь ordinal/preset/export; недоопределённые image algorithms. Исправлены15/16 и schemas/operations.
2. Повторная проверка API→DB: immutable approval, revision0 при первой записи, nullable capture policy при поэтапном rollout, safety review/eligibility, identity.compare до learning, body limits. Исправлены database generator, endpoints,13/17.
3. End-to-end import/delivery: импорт до появления asset_version, parse/ingest/resolve, optional omissions, explicit close-review, child/parent cancellation, graph expansion barriers. Введены ArtifactSnapshot, ParsedInterchange, DeliveryPreparation, typed shards.
4. Сверка с реальными файлами worker/gateway:512 inputs,128 outputs,1MiB stage_contract, отсутствие signer resource_class. Исправлен проект runtime protocol19: task-scoped input closure, root-only result, atomic closure adoption, отдельный signer pool с CPU wire class. Зафиксированы15 source files для проверки drift.
5. Согласование геометрии/качества/эксплуатации: existing DenseWarpTransform, straight alpha, exact protected pixels, сохранение всех registrations, resource reservations, fenced leader, restore/tombstones, выбранный C2PA dependency pin. Исправлены16–19.
6. Финальная очистка прежних формулировок; пересборка машинных контрактов; structural и semantic checks; повторная генерация и byte/hash comparison всех generated indexes/contracts. Результаты в contracts/check-report.json. Повторный запуск этой команды обязателен после любых новых правок.

## Закрытые замечания

Статус ниже означает «решение документировано и согласовано», не «код уже реализован». Automated evidence — semantic-spec.py/validate-spec.py; race/DB/hardware критерии остаются обязательными тестами будущей реализации.

| ID | Разрыв | Решение / evidence |
|---|---|---|
| R09 | evaluate не имел изображений и human-approved renders | RenderCase/FrozenRecipe/TrainingManifest,15; test_training_missing_render_input, test_training_source_approval_mismatch |
| R10 | первый review batch завершал parent | batch CAS + CloseSemanticReview/CloseImportReview,15; test_review_5000_in_50_batches, close_unresolved, reject_only |
| R11 | archive/вложенные files не разрешены policy | role multiplicities, bundle/imported_file, closure,19; test_export_bundle_role, output_reservation, closure tests |
| R12 | delivery потеряла ordinal/preset mapping | FrozenPreset+DeliverySlotShard+assembly input,15; delivery_exact_matrix/missing_pair/required_omission/preset_changed |
| R13 | вычислительные методы оставались названиями | точные формулы/tensor ABI/registration16; protected, relight, negative, lens reference tests; real-photo acceptance07 |
| R14 | approval менял immutable документ | v2_document_approvals, resolved artifact, revision0 first save,17; DB FK/immutability specification |
| R15 | обучение зависело от позднего identity пакета | identity.compare вV23, learning.evaluate GPU; test_identity_dependency_before_learning |
| R16 | capture policy требовал несуществующую qualification при раннем rollout | nullable capture_policy, unavailable только capture; test_schema_nullable_capture_and_donor |
| R17 | identity consent требовал сначала распознать неизвестного человека | trusted confirmed_faces; extraction только разрешённых face IDs,15/16; workflow/consent links11 |
| R18 | import генерировал recipe на ещё не существующий asset | parse→canonical ingest→resolve, new strict contracts,15; test_import_parser_before_resolver |
| R19 | NOT NULL snapshot импорта нельзя было заполнить image item | ArtifactSnapshot + nullable existing snapshot_items.asset_version_id; test_artifact_snapshot_before_ingest |
| R20 | размеры контрактов противоречили реальному wire/runtime | paged input closure, v2_task_inputs, root-only output19; runtime_manifest_sharding/task_scoped_refs tests + source lock |
| R21 | nonlinear geometry и multi-source transforms терялись | DenseWarpTransform reuse, RegisteredTransform[], output roles; schema/closure checks, formulas16 |
| R22 | масштабирование не имело точного persistence/функций | CapacityPolicy/ResourceBudget, existing ledger mapping, DRR/lease/restore18 |
| R23 | C2PA adapter и trust policy оставляли выбор генератору | pinned c2pa-python wheel, adapter methods, trust/revocation/verification15; external SDK qualification gate |
| R24 | дублирующиеся старые описания и невоспроизводимая проверка | обновлены01–14, generated08/function-index, check-docs.py, baseline lock и deterministic regeneration |

Исходные R1–R8 из14 также пересмотрены с учётом новых lifecycle/transport решений. Открытых известных блокирующих **проектных** замечаний после этих проходов не зарегистрировано. Это ограниченный результат review, а не доказательство отсутствия всех возможных ошибок в будущей реализации.

## Явные внешние условия и scope

Внешняя evidence не заменяется текстом: реальные model weights/license clearance, ICC/camera/lens calibration, consented train/calibration/test corpus, C2PA signing credentials/trust anchors, hardware qualification RTX3060 и restore drill. Контракты их приёма, проверки, отказа и статусы awaiting_evidence/unavailable заданы. Создавать вымышленные успешные отчёты запрещено.

Baseline interoperability: XMP subset, PhotoEditor bundle и sidecar+TIFF/mask exchange. Полный proprietary Capture One catalog round-trip, arbitrary PSD/PSB layer round-trip, native tethering всех камер, active-active sync и tenant sharding не считаются реализуемыми текущим baseline; они не должны появляться в маркетинговом обещании без отдельного scope и qualification. Полная v2 roadmap не считается выполненной, если обещанная ML capability остаётся unavailable. Это не скрытые TODO генератора.

Runtime re-review 2026-10-01: `workers/photo_worker/models/lock.py` расширен разрешённым семейством `identity512`, уже определённым в ModelFamilySpec. Проверены строгая схема lock/hash и fail-closed `ModelPool.acquire`; обновлён только соответствующий SHA в source-baseline-lock. Это не является qualification весов или включением `identity.compare` в operation registry.

## Команда повторной проверки

Из корня PhotoEditor:

    PYTHONDONTWRITEBYTECODE=1 .venv-platform/bin/python docs/implementation-v2/check-docs.py

Команда проверяет baseline file hashes, строит DB/contracts/indexes, валидирует схемы/FK/routes/packages/links, запускает сценарные reference tests и повторяет генерацию для проверки воспроизводимости. Не устанавливает зависимости, не запускает БД, не скачивает модели, не генерирует код продукта.

Отчёты: [общий](contracts/check-report.json), [структурный](contracts/validation-report.json), [сценарный](contracts/semantic-validation-report.json), [baseline locks](contracts/source-baseline-lock.json), [entry points](contracts/function-index.json).

Runtime re-review V24 2026-10-01: разрешённые model IDs расширены `eye_open` и `glare_mask`; существующие ID и проверки hash/license/backend сохранены. 15 tests model runtime и 9 capture tests прошли. ONNX интерфейсы проверяют exact named tensors, finite unit outputs и letterbox. Обновлён соответствующий baseline SHA. Это не qualification реальных весов или RTX3060.
