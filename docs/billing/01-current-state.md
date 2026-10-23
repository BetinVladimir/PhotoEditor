# Текущее состояние и точки интеграции

## Проверенный срез

- Анализ выполнен 21.10.2026 по рабочей копии, содержащей многочисленные существующие изменения.
- Эти изменения не редактируются.
- Проверка исходников и документации не равна запуску приложения или квалификации всей системы.
- Отсутствие коммерческого контура установлено в обследованных SQL/Go/TS/TSX файлах по Stripe, billing_plans и billing_credit; это не аудит каждого файла репозитория.

| Источник относительно корня PhotoEditor | Наблюдение | Следствие |
|---|---|---|
| docs/implementation-v2/billing-prelaunch-plan-2026-10-18.md | Семь планов, 45 resource offers, Stripe, Custom, переносы; статус «спроектировано, не реализовано» | Сохранить числа, развернуть технические контракты |
| docs/implementation-v2/12-scale-product.md | Коммерческое дополнение уже заменяет раннюю границу MVP без платежей; существующий quota ledger должен переиспользоваться | Не строить второй storage admission |
| docs/marketing-mrr-financial-plan-bg-2026-10-18.md | Финансовая модель использует те же цены, storage и 10× egress | Не выдавать расчётную маржу за измеренную себестоимость |
| backend/db/migrations/00002_tenancy.sql | tenants.quotas и retention в JSON; tenant_usage содержит storage_bytes, reserved_bytes, source_files, reserved_files, active_runs, active_gpu_tasks; RLS | Коммерческие ресурсы отделить от технических source_files и GPU concurrency |
| backend/db/migrations/00004_ingest_sharing.sql | quota_reservations имеет holder, bytes, files, state и expires_at | Повторно использовать действующие резервы физических байтов |
| backend/internal/tenant/service.go | defaultQuotas: 100×1024³ bytes, 100000 source files, GPU tasks=1; defaultRetention: processing 7 дней, exports 30 дней, temporary 24 часа | Это не Trial и не принятый manual retention. Нужны новые defaults и совместимость backfill |
| apps/photographer/screens/settings/quotas.tsx | React Native Paper; чтение tenantSettings + capabilities; лимит storage выводится в GiB; коммерческих buckets/платежей нет | Заменить содержимое полноценной сводкой, не ограничиться карточками покупки |
| apps/photographer/app/settings/quotas.tsx | Обёртка существующего localized routing | Сохранить локализованную навигацию и legacy redirects |
| apps/photographer/screens/settings/profile.tsx | Paper UI, email/locale/theme/timezone; осмотренный экран не содержит удаления профиля | Добавить отдельный подтверждаемый lifecycle удаления |
| backend/internal/auth и apps/photographer/src/auth.ts | Существующий auth и очистка локальной сессии | Logout не считать GDPR erasure; использовать auth для свежей проверки личности |
| backend/internal/tenant/service.go, Delete | Есть tenant deletion | Не смешивать user deletion и удаление общей студии |
| backend/db/migrations/00154_tenant_deletion_cleanup.sql | Claim/checkpoint/finish с cleanup token и lease | Подключить новые таблицы к актуальной версии purge, не копировать старую функцию целиком |
| docs/implementation-v2/11-consent-lifecycle.md | Consent scope/epoch, revoke и зависимые artifacts/models | Согласие на условия не заменяет consent для matching/learning |
| backend/internal/gateway/download.go | Worker-authorized чтение с mTLS/task identity и bounded Range | Внутренний worker download не является автоматически платным пользовательским egress; отдельно инвентаризировать public delivery paths |
| backend/db/README.md | Ограниченный валидатор migration bundle и forward-only workflow | Новые миграции идут после актуального head и через действующий процесс, без выдуманного номера |

## Юридический репозиторий

Корень: `/Users/freelancer/Documents/Beeloy/legal`. В `app/page.tsx` документы имеют ключи terms, consumer-terms, privacy, cookies; router `app/[locale]/[document]/page.tsx` подтверждает форму `/{locale}/{document}`. DPA находится в `app/dpa/page.tsx` и `app/[locale]/dpa/page.tsx`.

- Terms описывают B2B ERP/CRM, подключаемые модули, планы и продление.
- Consumer Terms относятся к loyalty-приложению, поэтому их нельзя механически назначить условиями B2C PhotoEditor.
- Privacy содержит controller/processor roles и критерии retention.
- Cookies прямо описывает legal centre без optional analytics, что не описывает cookies самого PhotoEditor.
- DPA содержит ERP/CRM/POS scope и требует расширения под обработку фотографий.
- README юридического проекта перечисляет незакрытые юридические проверки.

## Обязательная инвентаризация перед изменением кода

- Зафиксировать текущий migration head и определения реально установленных функций: старые numbered SQL — история, последующие миграции могут переопределять их.
- Найти все owner/upload/export/preview/public share/CDN пути и все SQL admission/finalization entrypoints.
- Составить таблицу «operation registry ID → вход API → billing class → canonical result → событие списания».
- Без этой таблицы закрыть BILL-01 нельзя.

Проверить source of truth генерации контрактов, миграций, api-client и DTO по существующим scripts/packages; generated файлы не править вручную. Статус authored/not run в implementation-v2 не переносить в completed. E2E должны проверять установленную текущую цепочку миграций и реальный Paper экран.

## Разрешённые изменения при будущей реализации

- Новые billing/privacy структуры, contracts, backend services, maintenance/reconciliation jobs, Paper экраны, migrations и тесты.
- Финансовая логика серверная.
- Настройки квот tenant PATCH должны перестать позволять обходить купленные entitlement: обычный владелец не может увеличить коммерческий лимит через старую форму/API.
- Технические override доступны только закрытому audited operator process.

## Уточнения повторного аудита 21 октября 2026

- Текущий source head обследованного каталога: `02597_cleanup_diagnostic_names.sql`; это filename в рабочей копии, не подтверждение installed DB head.
- Пакет текущих endpoints содержит 323 method/path identities.
- Executable v2 operation source содержит 39 declarations, не весь исторический registry.
- Эти числа — срез, перед реализацией переснять.

- `backend/internal/httpapi/catalog.go` использует `/api/v1/tenants/{t}`, `Idempotency-Key`, `If-Match: "<revision>"`; error envelope в server.go — code/message_key/args/request_id.
- Для web mutation проходят origin+CSRF;
- Stripe webhook имеет отдельную signature boundary.
- `domain.Role` содержит owner/admin/editor/viewer, tenant имеет единственный owner_user_id, TransferOwner уже реализован.
- Добавлять billing-admin для первого выпуска не требуется.

- `auth.Service.verifyOTP` вызывает auth_bootstrap_user и сразу создаёт сессию;
- AuthenticateAccess требует users.status='active', sessions не revoked и auth_epoch совпадает.
- Поэтому pending onboarding следует хранить отдельно от users.status, а свежую auth нельзя выводить из времени refresh session.
- Profile erase должен увеличивать auth_epoch и отзывать OTP recovery receipts, иначе старый bootstrap/replay способен вернуть доступ.

- `portal.Service.RedeemDownloadIntent` возвращает DownloadLink после Objects.SignDownload и повторной проверки current authority.
- Это signed object URL, не измеритель переданных bytes.
- Owner download intent создаёт `backend/internal/download/service.go`; redemption проходит через `httpapi/download_redemption.go`.
- Новый controlled delivery обязан сохранить формат DownloadLink и его object version/SHA/filename proofs, а не подменить предметную security проверку одной billing quota.

- `idempotency.Use` имеет scope tenant/principal_type/principal_id/route/key, key длины 1..128 ASCII33..126, canonical.Hash(request), receipt lock до domain locks и ValidateReceiptAuthority перед replay.
- `database.InTransaction` может повторять DB-only callback до трёх раз при 40001/40P01.
- Stripe/storage HTTP внутри callback недопустим.

`photo.outbox` уже обслуживает tenant task events с event_id/destination/routing_key. Новая billing_outbox — отдельная таблица для финансов/privacy side effects и не замена task publisher. Atomic bridge создаёт оба нужных события в одной TX; два consumer не должны оба выполнять одно fulfillment.

- Точки генерации: baseline source `docs/implementation/contracts/endpoints.json`; executable source `docs/implementation-v2/contracts/endpoints.json` и `schemas.json`; упаковка `scripts/endpoint_packaging.py` и `generate-contracts.py`.
- `generate-db.py` читает baseline tables.json и генерирует исторические миграции 1–9, поэтому добавлять новые billing tables в baseline с перегенерацией старых DDL запрещено.
- Для нового billing нужен additive catalog и forward-only migration stage, описанный в 09.
