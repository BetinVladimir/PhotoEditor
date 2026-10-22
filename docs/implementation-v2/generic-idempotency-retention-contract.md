# MVP: retention подтверждённого generic acceptance

Нормативный контракт для `backend/internal/idempotency.Use`. Принят для MVP 2026-10-17. Применяется к completed generic receipts, не ко всем таблицам с похожими именами.

## Подтверждённый результат

После атомарного сохранения успешного результата сохраняются scope `(tenant, principal_type, principal_id, route, key)`, request fingerprint, parent binding, canonical response status/body и resource identity до подтверждённого tenant purge. `expires_at` этой completed записи не разрешает удаление, повторную admission или повторный вызов mutation callback.

Точный повтор возвращает прежний canonical acceptance только после текущей tenant/session/membership/project/resource authorization. Истечение срока самой работы или отсутствие возможности скачать результат не превращает повтор в новую работу. Другой fingerprint либо другой immutable parent возвращает conflict. Отзыв текущего доступа запрещает выдачу прежнего результата; receipt сохраняется и не заменяется отказом.

`Use` сохраняет текущую реализацию: строка блокируется по exact scope, проверяется fingerprint, действующая authority и сохранённый успешный результат. Completed TTL filter не добавляется. Все будущие periodic cleanup обязаны исключать completed generic receipts. Tenant purge использует существующий явный allowlist; этот контракт не вводит новых DELETE прав.

## Отдельные сроки

Pending RenderFreeze/V2.Admit и другие специализированные private preparation имеют собственные deadlines и правила recovery. Одноразовые download intents, OTP challenges и encrypted OTP recovery receipts сохраняют сроки/consumption semantics своих exact contracts. Данный контракт не продлевает их жизнь, не разрешает выдавать истёкший bearer и не продлевает право исполнения задачи.

`expires_at` generic completed receipt — историческое поле планируемого срока, а не обещание удаления или permission на повторную mutation. Для pending нельзя выводить политику только из generic completed правила: требуется route-specific contract.

## Следующие версии

Безопасный tombstone GC относится к следующей версии. До отдельного проекта и миграции его нет. Он должен сохранять durable identity/fingerprint/parent и предотвращать повторную admission после удаления response payload; просто DELETE по expires_at недопустим. Текущий MVP не обещает автоматической очистки completed generic receipts раньше tenant purge.

## Сценарии следующего этапа

Авторский integration scenario `backend/internal/project/receipt_retention_integration_test.go`: expired completed exact key/body → byte-identical replay без callback; changed body → conflict; restricted project ACL revoke → отказ без response/callback и без удаления receipt. Это authored scenarios, не evidence PASS. Также обязательны upgrade, конкурентное ожидание receipt, tenant purge и ограничение размеров/объёма хранилища при эксплуатационной проверке.
