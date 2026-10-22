# Semantic preview до решения фотографа

Статус 2026-10-15: транспортный контракт, HTTP mount, клиентский валидатор и блокировка UI написаны; production producer, SQL publication/authority и окончательный Apply gate ещё не подключены. D-01 остаётся открытым. Unit-сценарии написаны, не запускались. Нормативный endpoint registry расширен с 79 до 80; `packages/contracts/v2-endpoints.json` синхронизирован с полным актуальным исходником документации (его прежняя копия отставала и до этого изменения).

## Уже написанный интерфейс

`GET /api/v1/tenants/{t}/semantic-intents/{id}/targets/{target}/preview` требует editor authority и четыре query fence: `expected_revision`, `target_revision`, `plan_sha`, `candidate_sha`. Повторные/неизвестные query параметры запрещены. Без установленного `SemanticReviewPreviewProvider` сервер возвращает 503; нельзя подставлять applied child или синтетические картинки, чтобы сделать кнопку активной.

Ответ `SemanticReviewPreview` содержит точный tenant/intent/target/version/workflow/revision/plan/candidate binding, ссылки `render` и `evidence`, `quality_status=pass|review`, ограничения и `protected_pixels_unchanged=true`. Четыре отдельные авторизованные media-ссылки: `before`, `after`, `affected`, `protected`. Все имеют одинаковый canvas, размер 1…4096, pinned artifact/SHA, MIME и expiry не более пяти минут. Overlays — PNG. Клиент отвергает foreign/stale binding, подменённые/повторные refs, несогласованные размеры, небезопасные URL и истёкшие ссылки.

UI разрешает approve после успешного отображения всех четырёх media для текущего binding. Ошибка изображения, expiry, замена клиента/кандидата, background или refresh authority сбрасывают разрешение и ещё не отправленное approve. Просмотренные receipts сохраняются при виртуализации списка до истечения срока; одновременно удерживается не более 100 receipts, выбранные approve не вытесняются. Общий таймер и AppState gate действуют также для целей вне viewport. Reject остаётся доступным. Перед новым Apply клиент снова читает parent authority; изменившийся fence требует нового preview. Уже отправленная попытка с неизвестным ответом повторяется с теми же body/key согласно существующему idempotency replay.

## Следующий обязательный кодовый этап

1. Ввести durable pre-Apply render intent/receipt с уникальностью `(tenant,intent,target,target_revision,plan_sha,candidate_sha,renderer/model locks)`. Из исходного TransferCandidate физически прочитать mask/transform, EditIntent, WorkingImage, protection и qualification; проверить их producer/canonical result/fence, snapshot/version и consent scope до и после IO. Сам TransferCandidate является предложением переноса маски и **не является** rendered image.
2. Admission preview не должен создавать approval или менять target в `queued/applied`. Его отдельные задачи/артефакты используют существующие quota/lease/cancel/replay/GC механизмы. Выполнить реальное преобразование/генерацию на qualified backend; сохранить exact rendered image и измеренный quality/protection report. Не переиспользовать `ApprovedSemanticEdit` с выдуманной approval.
3. Publish receipt только после canonical worker result и проверки полного render/evidence/overlay closure. Provider выдаёт media grants для exact object versions и повторяет текущий editor/target/plan gate после object IO. Readiness сообщает доступность только при установленных producer и authority. Фоновый результат после cancel/revoke не открывает preview.
4. Расширить admission Apply серверным receipt gate в той же транзакции, что decision batch/target locks. Новый approve требует exact pre-Apply receipt и актуальные plan/target/consent/version fences; reject не требует preview. Сохранить первоначальное replay уже завершённых решений до нового gate. Apply должен принять **показанные** rendered bytes, а не повторно запустить случайную генерацию с другим результатом. Если точное adoption существующего render невозможно, отказать и запросить новый preview.
5. Forward-only миграции должны включать ACL/RLS, роли/output registry, purge allowlist и GC/restore. Написать fresh/upgrade/denied-role и сценарии stale result, foreign target, object version race, cancel, expired link, uncertain commit/replay, protected-pixel mismatch. Выполнение — в согласованной глубокой тестовой фазе.

Полное закрытие D-01 наступает после подключения producer → canonical preview receipt → provider → UI display → fenced adoption показанного результата. Наличие этого транспорта или disabled-кнопки самостоятельно не закрывает пункт.

## Проверенная граница worker: exposure pipeline

Статический разбор текущего кода подтвердил следующие зависимости:

- `workers/photo_worker/operations/semantic/exposure_pipeline.py`: `execute_encoded_local_exposure` и `execute_measured_local_exposure` разбирают `ApprovedSemanticEdit` и вызывают `verify_admission` до и после IO/compute. Подключать их к preview с permissive verifier нельзя.
- `workers/photo_worker/operations/semantic/execute_exposure.py`: `execute_local_exposure` требует verifier committed human batch/child/task/source authority. Сам DTO `ApprovedSemanticEdit` не содержит approval, но отсутствие поля approval не отменяет обязательную проверку внешнего SQL admission.
- `workers/photo_worker/operations/semantic/local_exposure.py`: `apply_local_exposure` — независимое численное ядро без admission и публикации. Оно принимает уже проверенные working-linear RGB/mask/protection, проверяет overlap, бюджет памяти и cancellation. Именно этот уровень пригоден для общего использования двумя раздельно авторизованными контурами.
- `workers/photo_worker/operations/semantic/exposure_quality.py`: `measure_exposure_quality` пока также связан с ApprovedSemanticEdit. Перед использованием в preview следует выделить общую проверку численного результата с параметрами EV/strength/source, сохранив независимый replay и битовую проверку protected pixels.

Следующий этап реализации: выделить общий численный quality evaluator и самостоятельный preview input model/binder; затем подключить отдельный SQL preview authority loader. Существующий Apply executor и его human-admission verifier остаются обязательными для Apply. Отдельный preview executor не может создавать child version или публиковать Apply receipt. До установки loader/producer/provider endpoint остаётся unavailable. Это конкретный незакрытый участок P0.1, а не поддержанный backend.
