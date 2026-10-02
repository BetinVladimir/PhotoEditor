# База и миграции: нормативная версия 2.2

[database.json](contracts/database.json) перечисляет 40 таблиц, все колонки, nullability, defaults, unique/check и FK. [schema-reference.sql](contracts/schema-reference.sql) — генерируемая схема таблиц, FK, индексов и RLS, а не готовая production migration. Кодогенератор обязан дополнить её функциями/триггерами ниже и разделить по полю migration. Нельзя запускать этот reference SQL на production.

## Версионирование и документы

v2_document_streams — mutable указатели current/approved; v2_documents — immutable версии с UNIQUE(tenant,stream,version_no); v2_document_refs — immutable ссылки с UNIQUE(tenant,document,purpose,ordinal). Замена draft создаёт новый document_id и новые refs, поэтому не конфликтует со ссылками предыдущей версии. v2_document_links прежнего проекта исключена. History удерживает все artifacts до явно разрешённого retirement.

JSON в SQL ограничен 1 MiB, changed_paths имеет тип array; policy имеет тип object и SessionPolicy schema, capture_policy — nullable CapturePolicy. Остальные большие документы хранятся artifacts, до 8 MiB на JSON. Body HTTP до 1 MiB; если полный план больше — получаем artifact по авторизованному download intent. Ни произвольные storage URL, ни object keys клиенту не выдаются.

Каждая новая таблица имеет tenant_id/project_id, composite internal FK с обоими scopes. Существующие FK используют tenant/id; guard_v2_project_links дополнительно проверяет project через parent asset/collection/run. Global user FK не содержит tenant. Нет ON DELETE CASCADE. Nullable circular FK current_document_id, approved_document_id, active_version_id связываются после создания таблиц; при tenant purge сначала обнуляются в разрешённом deletion state.

## Обязательные функции и триггеры

SECURITY DEFINER owner photo_fn_owner; SET search_path=pg_catalog,photo; REVOKE ALL FROM PUBLIC. Вход principal вычисляется доверенным authenticated API; произвольный actor из JSON запрещён. SQL функции проверяют tenant binding, membership/role и lifecycle, даже когда API уже проверил.

- guard_v2_project_links(): BEFORE INSERT/UPDATE. Проверяет все external FK по chain ownership; asset→collection/project, run→snapshot/project. Export preset tenant-scoped и намеренно может использоваться в нескольких проектах; artifact требует authorized project binding. Для workflow проверяет run.kind=v2 и точное совпадение snapshot. Для correction — same stream/source/project и approved document evidence. FK face_id — не отдельная таблица: ID должен присутствовать в подписанном faces artifact соответствующей версии; validator запрещает другой face.
- guard_v2_immutable(): BEFORE UPDATE на всех mutable=false, всегда reject. DELETE доступен только lifecycle function после её собственных проверок; API/worker не имеют привилегии. Immutable history не UPDATE даже для отзыва согласий.
- bump_v2_revision(): BEFORE UPDATE на mutable; NEW.revision=OLD.revision+1, updated_at=statement_timestamp(); вызывающая функция сначала проверяет expected. Не допускается присваивание клиентом.
- replace_v2_draft(tenant uuid, asset uuid, expected bigint, document uuid, artifact uuid, sha text, actor uuid) returns uuid: lock stream, CAS, версия+1, validate recipe/layers, insert document+все nested refs; update current. Старая версия остаётся.
- approve_v2_draft(tenant uuid, asset uuid, expected bigint, document uuid, actor uuid) returns jsonb: lock stream, документ=current, correct source; immutable evidence в v2_document_approvals и audit; set approved_document_id, bump revision; вернуть DraftApprovalResult. Уже сохранённый документ не обновляется.
- adopt_v2_plan(workflow uuid, task uuid, attempt int, fence bigint, result uuid) returns boolean: полный алгоритм 09; canon result включает artifact/digest. false только stale; validation failure — domain error, не silent success.
- activate_learning_version(profile uuid, expected bigint, version uuid, actor uuid, reason text) returns bigint: locks grants→profile; evaluation eligible, all epochs, immutable lineage; set active, activation event, outbox. Rollback проходит те же проверки.
- change_v2_consent(consent uuid, expected bigint, action text, actor uuid, policy text) returns bigint: 11; grant новый epoch, revoke disables all dependent models.
- mutate_v2_resource(tenant uuid, project uuid, command text, expected bigint, body jsonb, actor uuid) returns jsonb: command allowlist ровно из17, switch с отдельной typed validator, никаких dynamic table names/SQL из body. Каждая branch имеет самостоятельный negative-role test.
- retire_v2_document(document uuid) returns boolean: maintenance only; TTL90days, не current/approved, не referenced by correction/model/workflow/publication; снять refs и удалить history только в этой функции, с audit. Строгая immutability означает отсутствие UPDATE, не вечное хранение после разрешённого retirement.
- cleanup_v2_consent(consent uuid, limit int) returns int: photo_maintenance, limit1..100; ledger алгоритм11. Storage delete вне TX, receipt/finalize внутри TX.

EXECUTE: photo_api получает draft/approval, activate/consent, mutate; photo_scheduler — adoption/finalization; lifecycle использует фактическую существующую maintenance role, проверенную на baseline. photo_worker не получает эти функции и DML новых таблиц. Reference grants owner DELETE перечислены по таблицам; запрет DEFAULT PRIVILEGES. Реализация не должна создавать новое общее право удаления API.

## Миграционный порядок

Фактический head репозитория — `00232`; `00196` не использовалась. Уже созданные миграции менять нельзя. Поле `migration` в `work-packages.json` и `contracts/database.json` — стабильный логический слот исходной спецификации; созданные физические номера перечислены здесь и в `physical_migrations` пакета. Начиная с `00233`, выделять номера по фактическому head для ещё не созданного пакета; никогда не перезаписывать применённый файл.

| Миграция | Пакет | Содержание |
|---|---|---|
| 00197 | V21 | workflows, versioned documents, qualifications, draft backend; runs/snapshots kind v2; finalizer/adoption |
| 00198 | V22 | sessions, persons, observations, consents, workflow lineage; session operations |
| 00199 | V22 | materialization ScenePlan, correction sources and locks |
| 00200 | V22 | atomic Analysis Apply decision and parent completion |
| 00201 | V22 | authorized InputSnapshot source for Analysis Apply |
| 00202 | V22 | authorized sealed replay of Analysis Apply |
| 00203 | V22/Apply | tenant-scoped API admission of Apply build row |
| 00204 | V23/V22 | девять V23 metadata tables, immutable guards, GC/purge refs; V22 decision added to exact purge allowlist; V23 writers and operation registry still disabled |
| 00205 | V23 | атомарная запись RecordCorrection с проверкой approval/source/session/consent и replay |
| 00206 | V23 | создание learning profile через project/person-scoped SECURITY DEFINER admission |
| 00207 | V23 | доверенный scoped список training candidates с consent и immutable artifact readiness |
| 00208 | V23 | run-scoped TrainingManifest transport с pinned SHA, quota и owner scope |
| 00209 | V23 | детерминированные ID TrainingManifest и ArtifactSnapshot документов для предварительного snapshot digest |
| 00210 | V23 | V2.Admit draft learning run с pending ArtifactSnapshot, quota и idempotency до публикации объектов |
| 00211 | V23 | freeze manifest после готовности двух документов, повторная проверка candidates/consent, immutable snapshot и seal guard |
| 00212 | V23/V22 | запрет изменения root artifact_id и created_at у sealed V2 snapshot |
| 00213 | V23 | JCS bytes recovery в V2.Admit idempotency record и scoped pending/replayed probe |
| 00214 | V23 | learning.fit CPU registry/output policy, v2_enabled=false до полного admission |
| 00215 | V23 | scoped fit Params document после freeze и идемпотентная blocked task stage |
| 00216 | V23 | атомарное открытие evaluation barrier после canonical adoption learning.fit |
| 00217 | V23 | scoped canonical CandidateProfile input и fenced claim evaluation barrier |
| 00218 | V23 | renew/release fenced evaluation lease с проверкой cancellation и canonical candidate |
| 00219 | V23 | fenced learning.evaluate Params backend document после canonical fit adoption |
| 00220 | V23 | learning.evaluate GPU registry/output policy, dispatch disabled до готового worker |
| 00221 | V23 | fenced blocked evaluate task stage с exact candidate/params и edge от fit |
| 00222 | V23 | атомарное закрытие learning.evaluate barrier после точного sealed input closure; dispatch остаётся disabled до готовности worker |
| 00223 | V23 | identity.compare GPU registry и три output policy; dispatch disabled до квалифицированного worker |
| 00224 | V23 | canonical fit/evaluate adoption создаёт immutable learning version, eligibility и consent lineage без активации |
| 00225 | V23 | CAS human safety review и явная activate/rollback с live consent epochs |
| 00226 | V23 | отзыв learning consent отключает зависимые версии и снимает active pointer |
| 00227 | V23 | постановка derived learning artifacts в consent cleanup ledger при отзыве |
| 00228 | V23 | tenant audit для safety review и активации профиля |
| 00229 | V23 | публикация draft с active learning dependency и immutable model_use_link |
| 00230 | V23 | approval повторно проверяет learning consent и eligibility после публикации |
| 00231 | V23 | approval также сверяет active version профиля после публикации |
| 00232 | V23 | approval атомарно закрепляет model и source descriptor refs в document closure |
| 00245 | V23 | run/event-scoped FrozenRecipe publication с детерминированными ID, SHA из recovery manifest и квотой |
| 00246 | V23 | freeze проверяет готовые FrozenRecipe отдельно от исходных EditingRecipeV3, сохраняя оба source-document ref |
| 00247 | V23 | новый SHA строгого RenderCase контракта при сохранении допуска прежнего SHA для уже запущенных V2 tasks |
| 00248 | V23 | SHA RenderCase с DescriptorShard provenance; seal выпускает новый SHA, ранее запечатанные V2 tasks сохраняют допустимые SHA |
| 00249 | V23 | scoped SECURITY DEFINER чтение base/approved EditingRecipeV3 для training admission: project/asset/document stream, approval, ready object и текущий consent; только photo_scheduler получает EXECUTE |
| 00250 | V23 | scheduler EXECUTE для locked candidate inventory и scoped SECURITY DEFINER чтение project/mode/person learning profile в той же транзакции |
| 00251 | V23 | scoped mask provenance для training: MaskRef в immutable closure correction event, binary child в canonical output успешного producer task того же проекта, live consent и role allowlist |
| 00252 | V23 | scoped перечисление ready JSON refs из immutable closure correction event для детерминированного выбора единственной protected mask; только photo_scheduler получает EXECUTE |
| 00253 | V23 | DBA-pinned `training_calibration_sha` в qualification policy и scheduler-only scoped чтение точного Calibration artifact при активной trusted qualification; forward-only |
| 00254 | V23 | scheduler-only scoped выбор active baseline модели и её training artifact SHA; отсутствие active version отличается от повреждённой/disabled версии; forward-only |
| 00255 | V23 | run/profile-scoped deterministic zero-profile publication с SHA из recovery manifest, квотой и запретом при active version; только photo_scheduler получает EXECUTE; forward-only |
| 00256 | V23 | freeze-time trigger сверяет baseline ref в recovery с ready zero-profile либо с текущей eligible active version; смена active version до freeze даёт конфликт; forward-only |
| 00257 | V23 | scheduler-only вывод model/dataset lock из единственной активной session.describe qualification по pinned Calibration SHA; forward-only |
| 00258 | V23 | scoped чтение exact ready FrozenRecipe refs из pending recovery manifest для повторного admission; только photo_scheduler получает EXECUTE; forward-only |
| 00259 | V23 | атомарное сохранение канонических FrozenRecipe bytes в pending idempotency recovery с проверкой SHA против manifest; лимит 32 MiB, после seal recovery заменяется JobTicket; forward-only |
| 00260 | V23 | разрешённый JSON output `mask_protected_ref` для mask.rasterize только при ненулевой явной защите; существующие outputs сохраняются; forward-only |
| ≥00233 | V23 | identity.compare worker, qualification, consent revoke/cleanup integration до включения writers |
| после V23 | V24 | devices/challenges/receipts/alerts/shot lists, capture policy |
| после V24 | V25 | optical.lens registry/policy и qualification, RAW version binding |
| после V25 | V26 | layer/region-preview registry integration, UI capabilities |
| после V26 | V27 | semantic tables, transfer/assembly policies |
| после V27 | V28 | portrait/reflection/relight policies; identity.compare уже в V23 |
| после V28 | V29 | optical multi-input policies и lineage |
| после V29 | V30 | delivery/interchange tables и assembly/parser policies |
| после V30 | V31 | provenance tables, isolated signer policy |
| после V31 | V32 | reconciliation assertions, capacity/restore deployment gates |

Каждая миграция атомарно: tables/constraints → functions/triggers → explicit grants → GC/purge refs → registry policies → assertions. Ни одного пакета с writers enabled до GC integration. Каждый пакет самостоятельно deployable после предыдущего; V32 не блокирует тестирование registry предыдущих пакетов.

Backfill: новые feature rows пусты; существующие users/assets/runs не переводятся в v2. Consent grants lazily provisioned под lock project при первом feature enable; draft stream+base checkpoint+current document создаются при первом PUT с expected_revision="0" и проверкой отсутствия stream, GET не пишет. Для отсутствующего draft GET=404; UI берёт base recipe из существующего editor. Initial stream revision1, после установки current первый save возвращает revision2; следующие записи CAS текущей revision, алгоритм17. Конкурентный first PUT: unique stream + CAS гарантирует один успех, другой409.

Forward-only. Откат релиза — flags off, drain/fence tasks, совместимый предыдущий бинарник; DROP колонок не является rollback. Purge порядок: leaf links → eligibility/activations/children → versions/documents → workflows/sessions/persons; grants после их links/events. Circular pointers обнуляются до удаления. Существующий finish_tenant_deletion расширяется закрытым allowlist и проверяет object-cleanup proof.

Acceptance: fresh install baseline→215 и populated upgrade; exact schema snapshot; каждый FK negative tenant/project; privilege matrix; immutable update denied; CAS races; purge после storage receipt; restore и mixed old/new runs. Reference SQL ещё не является доказательством успешного PostgreSQL upgrade.

Transport input closure и новая v2_task_inputs описаны19; графовые barriers используют существующий graph_expansions. Полная матрица EXECUTE и immutable approval —17.
