# Согласия и lineage

Consent имеет scope (tenant,project,purpose,person_id). identity_matching и learning_person требуют person_id; learning_photographer требует person_id=null. Согласие на matching не разрешает обучение. Person creation создаёт revoked grants с epoch=1; project provisioning создаёт revoked photographer grant с epoch=1. ConsentChange всегда CAS существующего grant. Лица не подтверждаются автоматически.

## Запись и зависимости

consent_grants mutable: id,tenant,project,purpose,person_id?,status(granted/revoked),epoch,revision,policy_version,actor_id,updated_at. consent_events immutable хранит каждое изменение. Событие коррекции хранит consent_id+consent_epoch, session_id, scene_group_id?, person_id?, client_event_id, approved_by. learn=false требует consent_id/epoch=null; learn=true требует оба и соответствующий purpose. Scene принадлежит session, asset входит в session snapshot, person association подтверждён.

learning_example_links связывает каждый version с event и consent epoch, а не только digest manifest. workflow_consent_links связывает любой выполняемый workflow с прочитанными grants. model_use_links связывает derived document/recipe с learning_version. artifact refs включают manifest, embeddings, models, evaluations; вложенные refs индексируются для GC.

## RevokeConsent(grant, expected_revision, actor)

Одна TX: lock tenant lifecycle → grant → зависимые runs по UUID → tasks по UUID → profiles по UUID → streams, полный lock order17. Проверить ACL и revision; status=revoked, epoch++, revision++; immutable consent event; все active_version_id, зависящие от старого epoch, установить NULL; зависимым versions присвоить disabled через отдельную mutable version_eligibility запись (не UPDATE immutable learning_versions). Зависимым незавершённым runs cancel_requested=true; outbox инвалидирует cache и начинает cleanup. Ответ успешен только после commit. Лишние cache invalidation доставки безопасны.

Train admission, fit adoption, activation, rollback и каждый render admission сверяют все epochs и granted под тем же lock order. Поэтому revoke до commit запрещает старый результат. Already leased compute может завершить математику, но не adoption. Повтор grant не восстанавливает старые версии: новый epoch требует нового manifest/train/evaluation.

## Данные после отзыва

Training cleanup исключает event из будущих manifest, отзывает capability URLs для training artifacts, удаляет embeddings/model artifacts, когда нет законных независимых refs. Рецепт, который использовал disabled learning version, сохраняется как audit, но не исполняется: 409 PROFILE_CONSENT_REVOKED. Клиент предлагает явный новый draft с базовым стилем, без автоматической замены результата.

Оригинальные фотографии и обычные approved exports не удаляются автоматически: это отдельные пользовательские assets, а не согласие на обучение. Их deletion выполняется обычной командой владельца. Вручную сохранённые параметры correction history остаются, learn=false в projection; исходный immutable event не переписывается. Удаление training bytes не требует удаления всего фото с другими людьми.

Cleanup ledger фиксирует consent_id/artifact_id, state pending/deleting/deleted/blocked_shared, попытки и next_attempt_at; dependency/reason получаются join по immutable links, правила retirement17; retention worker повторяет idempotently. Разделяемый artifact сохраняется до освобождения остальных refs, UI показывает blocked_shared, не обещает физическое удаление. Garbage collector не удаляет pinned history. Ссылки выдаются короткоживущие через текущий authorized gateway, отзыв проверяется перед новым доступом; уже скачанные внешние копии не отзываются.

Tenant purge использует ранее разрешённый forward-only finish_tenant_deletion только после доказанного удаления объектов, включая новые artifacts. photo_fn_owner DELETE — только закрытый allowlist. API/worker не получают DELETE. Revoke не является tenant purge.

## Acceptance

Конкурентные revoke/activate, revoke/adopt, revoke/rollback; повтор grant; два profiles с общей training lineage; shared artifact; старый URL; disabled recipe render; tenant deletion при pending cleanup. Во всех случаях после revoke commit ни один новый accepted output не зависит от отозванного epoch.
