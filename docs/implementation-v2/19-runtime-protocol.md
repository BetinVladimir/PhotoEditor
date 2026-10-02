# Согласование с существующим worker/gateway protocol

## Найденные реальные ограничения baseline

workers/photo_worker/contracts/wire.py: TaskCommand.inputs≤512, TaskResult.outputs≤128; resource_class не содержит signer. runtime/artifacts.py: TaskArtifacts._allowed строится только из command.inputs/params; stage_contract ограничен1MiB. SQL receive_task_event также ограничивает outputs128. Поэтому расширить только operations.json недостаточно: Session Brain с128 face artifacts,5000 correction plans и export bundles иначе не проходят транспорт. Этот документ — обязательная часть V21, consumers/tests изменяются до включения v2.

## Сохраняемый wire envelope

TaskCommand/TaskResult.schema_version остаётся1; старые limits512/128 сохраняются. `params` по-прежнему конкретный operation Params JSON≤1MiB. TaskCommand.inputs содержит один InputClosureManifest и не более127 direct roots (в сумме≤128, чтобы сохранить также legacy activate_bound_task limit). Ссылка manifest хранится в trusted lease.execution_profile.v2_input_manifest, с execution_profile.renderer_version="v2.2", contracts_sha, v2_entry_count и v2_entries_sha. Эти count/digest закрепляются атомарным seal после повторного чтения frozen cursor; worker сравнивает их с хешированным manifest до adapter. Не помещать миллион refs в Rabbit payload. Эту возможность получает только run.kind=v2 + registered v2 operation; legacy задачи продолжают прежний путь.

`BuildInputClosure(task)` получает frozen cursor, отсортированный по UUID then SHA, проверяет exact unique (artifact_id,sha,contract), записывает chunks≤512 entries, максимум1000000 entries/1954chunks, затем InputClosureManifest. entries_sha = SHA256 потокового JCS массива этих entries в заданном порядке. Manifest не содержит сам себя/chunks в entries; они являются transport metadata. Каждый transport-документ заранее публикуется через отдельный fenced `reserve_v2_input_document`/`complete_v2_input_document` scope задачи, без object IO в admission TX. В одной admission/expansion TX `register_v2_task_input_batch` перечитывает frozen cursor и сохраняет v2_task_inputs для каждого entry, проверяет прежний digest; `seal_v2_task_inputs` добавляет manifest/chunks, устанавливает command inputs и профиль. Ошибка откатывает всю TX; graph seal выполняется этим же вызывающим компилятором только после успешной регистрации. Поле contract задаёт registry schema/binary format из15; unique(task,artifact) запрещает разные hashes/типы одного artifact. DB rows immutable, index(task,artifact) и tenant/project FK. Command direct inputs являются subset этой таблицы. Closure roots+chunks создаёт trusted compiler, не worker/user.

`V2TaskArtifacts.create`: прочитать manifest через обычный declared root; проверить schema/task_id/count; начать disk-backed SQLite индекс allowed(artifact_id PRIMARY KEY,sha,contract), сначала command roots/params. Разрешить локальное чтение chunk refs только после получения их из authenticated/hash-verified manifest. Gateway всё равно независимо проверяет v2_task_inputs. Прочитать chunks последовательно, сохранить entries в SQLite, проверить unique/order/count и entries_sha; закрыть transaction индекса, затем исполнять adapter. Ошибка→TASK_INPUTS_INVALID, ничего не stage. Index scratch зарезервировать как max(16MiB,entry_count*512 bytes), RAM на batch≤512 entries. Нет wildcard по project или разрешения arbitrary nested refs.

`AuthorizeTaskObject`: существующий gateway auth/mTLS/task capability и attempt/fence/lease проверки сохраняются. После них объект разрешён если exact ref в прежнем t.inputs ИЛИ (run.kind=v2 AND exact ref в v2_task_inputs этого task). Таблица не даёт worker SELECT/INSERT, query выполняется existing photo.gateway_input(uuid,integer,bigint,uuid,text,uuid), owner photo_fn_owner, EXECUTE только photo_gateway (подтверждено backend/internal/gateway/service.go и migration00020). Не предоставлять photo_worker прямой DB доступ. Добавить test отрицательного чтения artifact другого task того же project, даже если workflow общий. Revoke consent/tenant/cancel запрещает новое object read тем же existing lifecycle gate.

`V2TaskArtifacts.contract/stage_contract` допускает typed JSON artifact≤8MiB вместо1MiB, только для v2 tasks. Params/control HTTP остаётся1MiB; parsing учитывает фактический RAM budget (не менее4×JSON bytes + parser overhead из qualification). Binary blobs идут streaming. Existing style() budget guard3GiB сохраняется; renderer budget обязан его учитывать. Нельзя просто удалить _allowed или поднять все legacy limits глобально.

## Result root и adoption inventory

Для каждой v2 операции TaskResult.outputs содержит РОВНО root role document ArtifactRef; вложенные files не перечисляются в wire result. operations.json.max_output_count означает максимально разрешённые staged objects через gateway output_uploads, а не длину TaskResult.outputs. Все вложенные outputs stage с текущими task/attempt/fence и per-role policy, ready-for-adoption подтверждается existing upload verification. Input reuse не требует повторной загрузки. Result root upload выполняется последним.

`PrepareV2Adoption(root,task,attempt,fence)` вне TX читает verified root, разворачивает typed refs15 в bounded batches, проверяет exact hashes/roles/MIME, каждая новая nested ref принадлежит verified output_upload того же task/attempt/fence; input ref находится в v2_task_inputs. Формирует canonical inventory и digest. Не использовать root с другого task. Не загружать NPY полноценно для digest — streaming verification.

`CommitV2Adoption(inventory,digest)`: lock order17, повторить canonical_result_id/attempt/fence/lifecycle/grants и metadata checks. Root-only receive_task_event ещё не даёт право FinalizeV2: require v2 adoption marker (existing inbox consumer="v2-adopt", event_id=UUIDv5(task_id,"adopt:"+canonical_result_id)). Existing complete_output уже создаёт artifacts.state=ready и output_uploads.state=ready после verified upload; это storage readiness, а не пользовательская публикация. Adoption НЕ повторяет этот переход и НЕ повторно начисляет storage_bytes. В одной TX проверить readiness ВСЕХ inventory artifacts, зарегистрировать closure refs/owner rows, marker/outbox. До marker user API не выдаёт эти outputs и GC считает их attempt-staged/pending adoption. Состояние task succeeded без этого marker означает pending adoption. Failure validation переводит parent failed, cleanup staging; root не публикуется через user API. Finalize/cache/publish разрешены только после marker. Идемпотентный replay того же inventory no-op, другой digest→RESULT_CONFLICT.

Отдельный процесс `photo-worker v2-adopter` выполняет только подготовку/принятие и финализацию, не выбирает ресурсы и не создаёт второй scheduler. Он читает кандидатов через `v2_pending_adoptions`, держит session advisory lock на task во время подготовки вне транзакции, читает только immutable object versions через `v2_adoption_artifact`, затем вызывает `adopt_v2_plan` в отдельной TX. `v2_pending_finalizations` выбирает готовые run; `finalize_v2_run` повторно проверяет маркеры и освобождает остаток reservation один раз. Семантически невалидный результат записывает `v2.adoption.failed`, прекращает запуск новых задач и после завершения/fence активных задач приводит parent к `failed`; инфраструктурная ошибка повторяется без такой записи. Compose-профиль `v2` выключен до применения V21 migration, наличия всех typed output bindings и квалификации операций.

Input reuse role semantics: root всегда новый document, остальные required logical payloads могут быть ранее declared inputs. Staged objects, не reachable из root, отклоняются и уходят staging cleanup; max_count считается только uploads текущего attempt, required по reachable types. Политики различают logical roles не только по schema: ImageResult.changed_mask и generated_mask обе MaskRef, но generated=null допустим только для non-generative operation/mode16. Capability signer проверяет dedicated pool, несмотря на resource_class=cpu.

## Signer и rollout

provenance.sign имеет resource_class=cpu для совместимости wire; execution_pool=signer в registry/lease profile. Dedicated CPU worker advertises allowlist только provenance.sign, key mount только там; общий CPU worker не advertises signer и не получает эту задачу. Registry qualification включает pool requirement, scheduler проверяет его до lease. Не добавлять несуществующее resource_class='signer' в старый enum.

Migration00196: v2_task_inputs, FK/index/RLS/grants, gateway read authorization extension, v2 adoption marker consumer, GC references и purge allowlist. GRANT EXECUTE gateway_input только photo_gateway; direct SELECT v2_task_inputs ему не выдавать. V21 сначала развернуть readers/runtime/gateway/scheduler, потом feature flags; старый worker не advertises renderer v2.2, поэтому не получает новых tasks. Downgrade выключает v2 admissions и drain/fence, legacy consumer не читает v2 payload.

Worker registration хранит `v2_renderer_versions` как JSON array с пустым fail-closed default. Trusted qualification/registration записывает `"v2.2"` только после проверки runtime/schema locks; worker SQL role не может обновлять регистрацию напрямую. `reserve_dispatch` выбирает только worker с этой версией для run.kind=v2, а `claim_task` повторяет проверку под lock. Отсутствие версии или mismatch workflow/task renderer возвращает unauthorized при claim; output gateway также не должен переходить на legacy quota path для некорректно помеченного v2 run. Legacy tasks игнорируют это поле.

Обязательные тесты генерации: legacy512/128 limits unchanged; v2 root-only5001 outputs; nested forged artifact; missing verified upload; stale fence после подготовки inventory; crash между receive и adoption; cancel в этом промежутке; manifest/chunk altered SHA; chunk duplicates; 1000001 entry reject; bounded SQLite index;8MiB accepted/8MiB+1 rejected только v2; generic CPU cannot lease signer. Одни schema witnesses эти проверки не заменяют.

### V24 scheduler activation

The scheduler starts the fenced capture loop when `PHOTO_CAPTURE_ENABLED=true`.
Before startup it checks callable V24 SQL entry points and requires both
`capture.prepare:1` and `capture.assess:1` to be enabled for the chosen backend.
Registry enablement follows model/hardware qualification; the setting does not
qualify models or bypass the task admission gate. Provide the exact qualified
worker lock digest in `PHOTO_CAPTURE_MODEL_LOCK_SHA`.

`PHOTO_CAPTURE_BACKEND` is `cuda` by default, or explicitly `cpu`.
Resource ceilings are decimal byte values: `PHOTO_CAPTURE_RAM_BYTES` defaults
to 17179869184, `PHOTO_CAPTURE_VRAM_BYTES` to 10737418240,
`PHOTO_CAPTURE_SCRATCH_BYTES` to 68719476736 and
`PHOTO_CAPTURE_RESERVED_BYTES` to 1073741824. CPU permits VRAM=0. These are
admission ceilings; each frozen task profile computes its own requirements.
The defaults leave headroom on the requested RTX3060 12GB / 32GB host, but
must still pass the hardware benchmark and concurrent workload qualification.
Scratch must be backed by actual available storage. No benchmark is implied by
these defaults. Missing lock, invalid budget or unavailable SQL/operation
capability rejects enabled startup. Default activation is false until gates pass.

The capture loop runs independently of dispatch/recovery. Readiness includes its
state while enabled; metrics expose `photo_scheduler_capture_transitions_total`
and `photo_scheduler_capture_failures_total`. A transition counts successful
queue processing, including waiting/release, rather than a completed QA report.
Shutdown cancels and joins the loop; per-job renewal protects long object IO.
