# Реестр трасс: проверенный срез и объём закрытия

Срез исходников: `8d663b1` (2026-10-15). Нормативная область: 30 операций `contracts/operations.json`, 79 HTTP-контрактов `contracts/endpoints.json`; полный построчный реестр кандидатов — [trace-inventory.json](trace-inventory.json). Этот срез выполнен чтением исходников и контрактов. Тесты, миграции, Metal, SQL под реальными ролями и физические модели **не запускались**. Ни один исторический PASS не перенесён.

## Что установлено, а что нет

| Уровень | Текущий факт | Статус |
|---|---|---|
| Контрактный охват | Все 30 операций и 79 endpoint находятся в реестре; у всех операций найден worker-кандидат, у всех endpoint — статическое объявление маршрута либо динамическая таблица. | Инвентарь кода; не закрытая трасса. |
| Сквозная трасса | Для 109 строк ещё не зафиксирована проверка всех переходов request → authority → dispatch → physical result → adoption → review/Apply/download → cancel/retry → cleanup. | `not_verified` для каждой строки; не трактовать как 109 доказанных дефектов. |
| V27 semantic | Клиентские editor/run/semantic screens и кандидат worker существуют. Физическая FAN/BiSeNet/skin → результат по цели → решение фотографа → Apply с реальными model pins не доказана. | P0: закрыть и проверить по установленным моделям. |
| V28–V29 | Контракты и кандидаты implementation для AI операций есть. Нет текущего реестра квалифицированных весов/доноров и доказанной positive physical трассы по каждой операции. | P0 для доступных/квалифицированных возможностей; остальные должны честно показывать `unavailable`. |
| V30 ordinary delivery | UI, API и scheduler/SQL-кандидаты есть; последние кодовые изменения закрывали lease, required bundle и download revocation. Положительный полный путь и гонки revoke/restore не исполнялись. | P0, остаётся сквозной проверкой; дефект не утверждается. |
| V30 interchange export | `backend/cmd/photo/main.go` действительно вызывает `prepareSchedulerInterchangeExport` и `RunSchedulerWithInterchangeExport`; старая запись плана о не подключённой executable factory устарела. При пустом pinned deployment factory возвращает nil. | Расширение после MVP: довести provisioned authority и physical round-trip; не считать отсутствие настройки дефектом MVP. |
| V23 identity | Dispatch остаётся gated до квалификации; код admission/adoption есть. В `apps/photographer/app` нет отдельного `/learning/[id]` или UI identity.compare. | Полный V23 после MVP; не блокирует MVP, если capability закрыта. |
| V31 signing | В коде есть signer/TSA trust и lease gates; provisioned SDK/key/trust и positive end-to-end C2PA на конечном файле не подтверждены. | После MVP; не включать публичное обещание подписи без квалификации. |
| V20/V21/V32 safety | Миграции вплоть до `02167` и защитные сценарии написаны, но не применены/не выполнены в текущем цикле. Отдельно проверить real-role ACL `photo_fn_owner`, tenant purge, restore, readiness. | P0 для MVP-инвариантов; наличие SQL текста не доказательство установки. |

## Реальный разрыв

Подтверждённый **документальный** разрыв — все 109 строк пока имеют только кандидатов исходников, без верифицированных связей и статуса `coding_closed/configured/qualified/accepted`. Подтверждённый **продуктовый** разрыв — нет физической квалификации AI на текущем Metal с потолком 10 ГБ и нет сквозного положительного прогона пути фотографа; V23 learning UI отсутствует, но отнесён к следующей версии. Текущее дерево само по себе не позволяет честно определить число оставшихся кодовых дефектов или процент завершённости: отсутствие запуска проверок отделено от отсутствия реализации.

Для каждой из 30 операций следует заполнить 8 ворот: (1) клиент/request, (2) authorization + frozen Params, (3) durable admission/dispatch, (4) worker и model lock, (5) canonical result, (6) transactional adoption, (7) review/Apply или отказ, (8) cancel/retry/GC/purge. Для 79 endpoint — 6 ворот: route mount, tenant/auth, service/SQL, response/errors, client consumer, idempotency/revocation. Это **714 проверок связей** (240 + 474), верхняя оценка объёма реестра, не 714 независимых фич; общие участки переиспользуются. После проверки каждой строки фиксировать конкретный отсутствующий переход как code gap либо `coding_closed`; физическую квалификацию и тестовый PASS вести отдельно. Этим методом считать новый процент только после заполнения ворот; предыдущая оценка ~68% была экспертной, не измеренной реестром, и не должна использоваться как current completion.

## Очередь до глубокой тестовой фазы

1. **P0, MVP:** проверить 30 operation traces с приоритетом V27–V29, затем 79 route traces по пользовательскому пути; для каждого найденного отсутствующего перехода дописать реализацию и unit-сценарии. Запретить автоматический Apply AI без явного решения фотографа.
2. **P0, MVP:** зафиксировать установленные и неустановленные model/RAW/camera/ICC capabilities на Metal 10 ГБ; код должен fail closed, пока qualification не получена. Физические прогоны и quality corpus остаются следующей тестовой фазой по принятому порядку.
3. **P0, MVP:** замкнуть и затем испытать import → session/editor → review/Apply → export → ordinary delivery/download, tenant purge, restore/readiness и права реальных DB ролей. Миграции/сценарии сейчас считать только authored.
4. **После MVP, но в полной цели V20–V32:** V23 learning/identity UI и activation, V24 hardware tethering, расширенный V30 interchange/delivery, V31 C2PA, горизонтальный V32. Для уже написанных веток не дублировать код; проверять installation/qualification и заполнять трассы.

Критерий завершения *кодирования до тестирования*: для каждой MVP-строки либо заполнены 8/6 ворот с ссылками на код и unit-сценарии, либо документирована отключённая capability и причина; нет открытых code gaps в включённом пользовательском пути. Критерий готовности продукта наступит позднее, после запуска fresh/upgrade миграций, unit/integration/physical QA и исправления выявленных ошибок.
