# Юридические требования и удаление персональных данных

## Правовые основания и границы

Основа — [GDPR, официальный текст EUR-Lex](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng), в частности статьи 5–7, 12–13, 17, 20, 25, 28 и 32. Privacy notice не является универсальным согласием. Для аккаунта/оплаты документируется договорное основание и применимые законные обязанности; optional analytics, marketing и специальная обработка требуют отдельных применимых оснований. Удаление имеет законные исключения для минимальных обязательных records; это не право бессрочно хранить весь профиль.

Этот раздел задаёт инженерные требования и проверяемые gates, а не юридическое заключение о полном соответствии продукта. Юрист/ответственный за privacy должен подтвердить применимые основания, договоры, налоговые retention и трансграничные передачи перед live. Закрытый test contour использует синтетические данные.

## Ссылки на legal.beeloy.org

Пути подтверждены локальным router юридического проекта; публичная доступность, redirect и конкретный текст production-домена в рамках анализа не квалифицированы. Все пользовательские ссылки должны вести именно на этот домен. Подставлять только поддерживаемую locale; если перевода нет — явно обозначенный English fallback, не silent substitution.

| Документ | Целевая ссылка для RU | Локальный источник | Что проверить до включения |
|---|---|---|---|
| B2B Terms | https://legal.beeloy.org/ru/terms | Beeloy/legal/app/page.tsx, T | Включает ли PhotoEditor, AI billing, archive, limits и recurrence |
| Privacy | https://legal.beeloy.org/ru/privacy | app/page.tsx, P | Реальные PhotoEditor данные, субпроцессоры, Stripe и сроки |
| Cookies | https://legal.beeloy.org/ru/cookies | app/page.tsx, C | Описать PhotoEditor, а не только legal centre без analytics |
| DPA | https://legal.beeloy.org/ru/dpa | app/dpa/page.tsx + app/[locale]/dpa/page.tsx | Обработка фотографий, лица, AI providers, TOMs, deletion и backups |
| Consumer Terms | https://legal.beeloy.org/ru/consumer-terms | app/page.tsx, B | Сейчас loyalty-specific; не применять к PhotoEditor без дополнения |

English fallback: те же пути с `/en/`. Дополнение PhotoEditor должно быть опубликовано в legal репозитории на legal.beeloy.org и получить точный version/hash/URL. Путь нового addendum ещё не существует в обследованном router: не добавлять выдуманную production ссылку в UI. До публикации закрыть self-service для неподдержанного B2C scope. Изменение legal репозитория вне объёма этой документационной задачи.

## Юридические пробелы, обязательные к устранению

| Пробел | Требуемый результат |
|---|---|
| Terms ориентированы на ERP/CRM | PhotoEditor addendum: что считается фото/операцией, повторы, packs/expiry, bounded optimistic overrun, без automatic overage, manual archive, отмена AI/storage и refunds |
| Privacy не перечисляет весь PhotoEditor data flow | Inventory фотографий, EXIF/GPS, лиц/embeddings, пользовательских правок, logs, billing, идентификации; роли controller/processor по целям |
| DPA scope ERP/CRM/POS | Явные PhotoEditor instructions, запрет обучения без отдельного основания, subprocessor register, locations, transfer safeguards, deletion assistance |
| Cookie policy описывает legal centre | Реальный scan PhotoEditor web: имя, цель, provider, storage location, срок, necessary/optional; native identifiers отдельно |
| Нет утверждённых точных retention/backup сроков | Версионированная матрица сроков, legal bases, trigger, max backup expiry, restore tombstones и владельцы удаления |
| Сценарий B2C PhotoEditor не покрыт loyalty terms | Отдельная юридическая проверка цифровой услуги, cancellation/withdrawal и mandatory rights до B2C live |
| Внешние providers | Документировать Stripe, hosting/object storage, email, AI и optional analytics; договоры/местоположения/EEA transfers; не предполагать всё EU |
| Matching/biometrics | Отдельная оценка целей: не всякое фото special-category; identification biometrics может требовать Article 9 assessment/DPIA; release gate по реальным функциям |

Никакой checkbox Terms не создаёт согласия изображённых на загруженных фото людей. Tenant отвечает за основания customer content; Beeloy выполняет processor duties и отдельные controller обязанности для аккаунта/биллинга. Существующие consent_grants scope/epoch продолжают контролировать identity_matching и learning, onboarding не ставит их в granted.

## Онбординг

Сначала показать ссылки и версии необходимых документов. Пользователь явно отмечает принятие Terms; B2B representative подтверждает полномочия и применимость DPA; отдельно отмечает ознакомление с Privacy. Если для определённой реально optional цели нужно consent, показать отдельный выключенный checkbox с purpose и отзывом. Нельзя сделать обязательным «согласие на любую обработку данных» вместо договорного основания.

На сервер отправляются document_set_hash, events(document_version_id,action), client_event_id; locale и capture channel уже связаны с показанным requirement set/auth context. Server восстанавливает individual text hashes из version IDs. Server проверяет current required version и меняет account_onboarding_states на complete вместе с legal receipt одной транзакцией; users.status остаётся active для доступа к онбордингу. Новая версия между показом и submit → 409 LEGAL_REACCEPTANCE_REQUIRED и повторное ознакомление. Архив принятого текста доступен пользователю; ссылка на изменяемую страницу без сохранённой версии недостаточна.

Существующие аккаунты получают pending legal status при переходе, видят объяснение и документы. Не приписывать старым пользователям согласие задним числом. При необходимости reacceptance новые paid actions закрыты; доступ к privacy controls, deletion, export и обязательным правам сохраняется согласно policy. Выход из onboarding не создаёт подписку.

## Cookies и optional SDK

Necessary auth/security storage объясняется без фиктивного consent gate. Optional SDK не инициализируются до явного выбора. Reject optional столь же доступен, как accept; closing banner не считается accept. Consent сохраняет purpose/version/time и применяется при следующем визите; истечение/изменение purposes требует нового выбора. Отзыв прекращает будущую загрузку SDK и очищает доступное optional storage. Если optional cookies отсутствуют, не показывать ложный analytics consent — достаточно информирования и действующей cookie policy.

Paper UI используется также для consent banner/dialog. На native вместо browser cookie механики учитываются SecureStore/device identifiers и platform privacy permissions; web consent не выдаёт системное разрешение на устройстве. Существующие refresh/CSRF tokens имеют отдельный security lifecycle и очищаются при deletion.

## Удаление профиля и студии

Профиль — пользователь, студия — tenant; удаление одного не должно стирать чужую общую студию. Функция доступна независимо от квот/задолженности. Для запроса — свежая аутентификация; proposed max age=5 минут. До подтверждения показать последствия, возможность export, shared ownership и legally retained records.

| Ситуация | Поведение |
|---|---|
| Member нескольких студий | Отозвать все memberships и user credentials; сохранённые законно общие studio assets остаются с обезличенной attribution. Запрос на собственные изображения как data subject обрабатывается отдельным workflow с controller |
| Owner с eligible active участником для передачи | Явная подтверждённая transfer ownership и отдельный billing contact для каждой студии, затем удалить профиль |
| Sole owner | До erasure commit дать выбор transfer существующему active участнику через текущий TransferOwner либо явное закрытие принадлежащей студии и её subscription/content; повторно проверить tenant revision/owner на commit. Нет silent auto-delete общего tenant и нет требования бессрочно сохранять PII |
| Ownership unresolved | Принять privacy case и прекратить optional обработку; в ownership_action_required не запускать irreversible erasure и не требовать действий через уже отозванную сессию. Пользователь завершает preflight через свежую auth; если он требует немедленного закрытия, coordinator обрабатывает scoped close instruction. Privacy case имеет deadline и escalation, не теряется из-за отсутствия решения |
| Paid order в обработке | Пометить account deleting до новых grants; сверить уже совершённую оплату; не выдавать новый доступ удалённому аккаунту; завершить documented refund/cancellation saga |
| Непогашенная задолженность/спор | Минимальные records с основанием из retention policy; прочая PII удаляется, право запроса не блокируется |

## Saga удаления

1. Записать deletion request и scoped status token. Выполнить authenticated preflight ownership и свежую проверку личности до irreversible erasure commit. При commit erasure_requested закрыть новые login/refresh, revoke sessions/API keys/device tokens/push tokens и приостановить memberships. Последующие шаги выполняет erasure coordinator, не отозванная user session. Idempotent retry возвращает тот же request.
2. Исполнить уже подтверждённый до erasure commit ownership/close plan для каждого tenant; заново проверить snapshot epochs. Transferred работающие студии не закрывать. Сразу закрыть создание нового Checkout и выдачу нового entitlement этому deleting account. Existing runs отменяются по текущим строгим lifecycle правилам; старый worker не может adopted output после запрета.
3. Cancel будущие Stripe charges/items в соответствии с выбранным закрытием AI/storage. Сохранить минимальные invoice/payment records, удалить/редактировать customer PII и ненужные metadata в пределах поддерживаемого Stripe lifecycle. Исторические финансовые документы могут иметь отдельное законное хранение; не обещать уничтожение у Stripe всех инвойсов. Если external API недоступен — retry и статус external_pending.
4. Удалить/обезличить account email, name, avatar, locale/timezone там, где привязка более не нужна; удалить OTP recovery, sessions, invitation email, device identifiers, user-owned drafts, saved selections и billing contacts без retention basis. Ограничить audit attribution непрямым subject ref; очистить free text и snapshot PII, а не только users.email.
5. Для закрываемого tenant использовать текущий deletion_jobs и актуальный finish_tenant_deletion. Удалять originals/results/previews/ZIP, все object versions и незавершённые multipart uploads; связанные models/embeddings/EXIF и artifacts по consent/lineage. Shared objects не удалять без освобождения законных refs; показать retained reason. Финансовые retained записи вынести из customer runtime scope до tenant purge.
6. Очистить cache, search/index, CDN invalidations, analytics identity, logs по policy, webhook raw payload, outbox payload и локальные app caches/SecureStore. Immutable business ledger хранит минимальные business refs, не PII snapshots. Лишние PII в immutable audit требуют privacy-safe envelope/redaction либо per-subject encryption key destruction с проверкой отсутствия plaintext copies.
7. Создать erasure tombstones для backup/restore. Backup copies изолированы от обычной обработки и истекают по утверждённому конечному сроку. Restore сначала применяет deletion tombstones, затем разрешает serving. Тест restore должен доказать, что login/email/photo/download не воскресли.
8. Зафиксировать completion evidence по каждой системе. Статус completed только при выполненных active-system шагах и раскрытом backup lifecycle; retained legal exceptions → partially_retained с категорией, основанием и сроком. Пользователь видит проверяемый статус, а не неподтверждённое «всё удалено».

Операционная цель для активных систем — до 7 дней, для первичного ответа по запросу — в пределах одного месяца согласно GDPR Article 12 с применимыми исключениями/продлением. Это proposed internal SLA, не утверждённый срок хранения backup. До live нужны конкретные approved maximum backup days и statutory retention по категории/юрисдикции. Draft retention policy не должна препятствовать немедленному удалению необязательной PII.

## Матрица PII и доказательств

| Категория | Где искать | Действие и доказательство |
|---|---|---|
| Account identifiers | auth users/sessions/OTP/recovery/devices/invitations | Нет login/refresh/lookup по старому email; credentials revoked |
| Payment contact | billing_contacts, Stripe customer, Checkout metadata | Delete/redact, retained invoices строго отдельно; внешний outcome receipt |
| Photo content | objects/versions/multipart, thumbnails, ZIP, models/embeddings, EXIF/GPS | Object inventory пуст для purge scope; shared refs перечислены |
| User activity | runs/corrections/audit/free text, caches, client storage | Attribution redacted; no email/name/IP in remaining payload без основания |
| Webhook/telemetry | inbox payload, outbox, logs, analytics, error tracker | TTL + redaction/deletion; pseudonymous labels также учтены |
| Backups | DB/object backups/snapshots/restore jobs | Finite expiry + tombstones; restore drill no resurrection |
| Financial/legal retention | invoices/payments/accepted contracts | Минимальные records, purpose restriction, approved retain_until, eventual erasure job |

Полный data inventory — артефакт приёмки, а не предположение, что перечисленных таблиц достаточно. Простой поиск email не доказывает отсутствие всех PII; нужны seeded canary values по категориям и проверки policy/keys/external systems.
