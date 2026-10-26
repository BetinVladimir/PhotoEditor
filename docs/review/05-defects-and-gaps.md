# 05. Ошибки, разрывы и потенциальные проблемы

Легенда. **Статус:** `подтверждено` — прочитан код/конфиг, указан путь и строка; `гипотеза` — вывод из косвенных признаков, нужна проверка. **Серьёзность** — для выпуска MVP: Крит / Выс / Сред / Низ.

## A. Дефекты и дефектогенные места (подтверждено чтением кода)

| ID | Серьёзность | Описание | Где | Последствие |
|---|---|---|---|---|
| D-01 | Выс | Первая же ошибка heartbeat задачи помечает lease потерянной и обрывает работу: `except Exception: … ctx._lost.set(); return` — без повтора в пределах оставшегося lease | `workers/photo_worker/runtime/executor.py:180-192` (`Executor.process`, внутренняя `heartbeat`) | Разрыв VPN на 10–15 с убивает многоминутную GPU-задачу; затем задача выполняется заново |
| D-02 | Выс | Первая ошибка heartbeat ёмкости останавливает **весь воркер** (`stop.set()` + `abort()`) | `runtime/capacity.py:65-79` (`CapacityReporter.run`) | Кратковременная недоступность PostgreSQL → остановка процесса и сброс активной задачи; требует внешнего supervisor |
| D-03 | Выс | Фиксация результата (`commit_success`/`commit_failure`) не повторяется при сетевых ошибках; `_query` повторяет только `SerializationFailure`/`DeadlockDetected` (3 попытки по 10–20 мс) | `executor.py:250-262`, `runtime/database.py:365-374` (`TaskDatabase._query`) | Работа GPU завершена, выходы загружены, а commit не дошёл → сообщение не ack, задача переисполняется; осиротевшие выходы |
| D-04 | Сред-Выс | Новое соединение PostgreSQL на **каждый** вызов (TCP+TLS+роль), `connect_timeout=5`, `statement_timeout=5000`; пула нет | `runtime/database.py:54-62` (`_connect`) | На высоколатентном VPN каждый heartbeat платит полный handshake; таймауты 5 с срабатывают ложно |
| D-05 | Сред-Выс | Передача артефактов без докачки: выгрузка — один `PUT` всего файла после полного хеширования (два прохода чтения), новый `Idempotency-Key` на каждую попытку; скачивание — без `Range`, `Transfer-Encoding` запрещён | `runtime/gateway.py:141-168` (`open_stream`), `178-250` (`upload`, ключ — строка 202) | Файлы до 20 ГиБ при обрыве начинаются заново; повтор PUT не распознаётся как тот же |
| D-06 | Выс (для топологии) | Worker в production принимает только `amqps://` (`Transport.__init__`), но в compose/provision нет TLS-слушателя RabbitMQ, а `provision.py` пишет `AMQP_URL=amqp://…` | `runtime/transport.py:31` (`Transport.__init__`); `deploy/compose.yaml` (сервис `rabbitmq` без конфигурации TLS); `deploy/framevela/provision.py:52` | Удалённый воркер не подключится к брокеру «как развёрнуто»; `production.env.example` ссылается на `amqps://`, то есть пример и провижининг расходятся |
| D-07 | Сред | DSN базы формируется с `sslmode=require` (шифрование без проверки сертификата), хотя в провижининге выпускается внутренний CA и сертификат `postgres` | `deploy/framevela/provision.py:51` | Для канала по VPN нужен `verify-full` с CA |
| D-08 | Сред | Один клиентский сертификат `framevela-worker` для всех воркеров; `PHOTO_WORKER_ID` в compose прописаны константами `…0001…0004` | `provision.py` (цикл по `server/worker/postgres`), `deploy/compose.yaml:273,282,291,303` | Нельзя отозвать отдельный GPU-хост; README допускает общий сертификат только «на одном доверенном хосте» |
| D-09 | Выс | Дрейф состояния: на проде применены миграции до 2597, в репозитории нумерация до 02659; scheduler остановлен («нет Render calibration catalog»); `implementation-v2/README.md` называет «миграции до 00195», MVP-план — «до 2625» | `deploy/framevela/README.md`; `backend/db/migrations`; `docs/implementation-v2/README.md` | Невозможно утверждать «что именно развёрнуто»; приёмка на «исторических» результатах недействительна |
| D-10 | Низ-Сред | Жёсткий домен в портале: `useState('https://framevela.com'+path)`; `canonical`/`alternates` на страницах `noindex` | `apps/portal/src/components/LocaleLinks.tsx:10`; `…/s/[share_id]/page.tsx` (`generateMetadata`); `…/[locale]/page.tsx` | Ломает staging/self-host; метаданные share-страницы указывают на прод-домен |
| D-11 | Сред | Деплой photographer ссылается на неотслеживаемый каталог: `"directory": "../../../work/photographer-web-localized-r2"` | `deploy/framevela/photographer/wrangler.jsonc` | Сборку невозможно воспроизвести из репозитория |
| D-12 | Сред | Ограничение частоты есть только для выдачи OTP (`photo.auth_rate_limits`) | `backend/internal/auth/service.go:140-158`; нет аналога в `httpapi`, `portal`, `upload` | Брутфорс/DoS на `portal/sessions`, `download-intent`, `uploads/begin`, регистрации; зависит от внешнего прокси |
| D-13 | Сред | Нет эндпоинта метрик и трассировки; наблюдаемость — только health-проверки | `backend/internal/httpapi`, `deploy/` | Нет алертов по глубине очереди, возрасту outbox, истечению lease, ошибкам commit |
| D-14 | Сред | `make test` не запускает web-тесты и БД-интеграцию | `Makefile:34-35` | «Зелёный» основной таргет не гарантирует работоспособность клиентов и SQL |
| D-15 | Низ-Сред | Жёсткие пути инсталлятора: `/srv/photoeditor`, `/mnt/HC_Volume_<id>` | `deploy/framevela/provision.py:6-8` | Не переносится на другой хост без правки кода |
| D-16 | Низ-Сред | В VCS попали идентификаторы инфраструктуры (IP сервера, идентификаторы Cloudflare, логин SMTP-реле, адрес пересылки почты) | `deploy/framevela/README.md`, `deploy/framevela/*/wrangler.jsonc` | Паролей не найдено, но это упрощает целевые атаки и осложняет открытие репозитория |
| D-17 | Сред | Только тёмная тема, цвета захардкожены; политика-тест не покрывает `screens/`; контраст/размер шрифта не измерены | `docs/ui-audit/REPORT.md` (F-01…F-03), `apps/photographer/src/paper-theme.ts` | Не выполняется обязательное условие №8 MVP |
| D-18 | **Выс** | IP клиента берётся из `r.RemoteAddr`; заголовки `X-Forwarded-For`/`X-Real-IP` нигде не обрабатываются (нет `middleware.RealIP` или доверенных прокси; `server.go:98` подключает только `RequestID` и `Recoverer`). За Caddy (`reverse_proxy api:8080`) `RemoteAddr` — адрес прокси | `backend/internal/httpapi/auth.go:113` (запрос OTP), `backend/internal/httpapi/catalog.go:290,318` (capture-challenge), лимит `{ip: 20/час}` в `backend/internal/auth/service.go:144-146` | Лимит «20 запросов OTP в час на IP» становится **общим для всех пользователей**: исчерпав его, любой (или нагрузка) блокирует вход всем (`429 RATE_LIMITED`); `request_ip_hash` и аудит бесполезны. Подтверждено чтением; проверить на стенде двумя разными клиентами |

## B. Разрывы (чего не хватает до заявленной цели)

| ID | Разрыв | Комментарий |
|---|---|---|
| G-01 | Нет control-plane API для worker | Всё взаимодействие с состоянием задач — через SQL-функции (≈100) из `TaskDatabase`; для VPN нужен HTTP/gRPC-слой (см. [MVP/01](MVP/01-worker-and-vpn.md)) |
| G-02 | Нет описания и конфигов VPN/кластера | Ни схемы адресов/маршрутов, ни политик доступа, ни управления сертификатами GPU-хостов |
| G-03 | Нет e2e и CI | См. [04](04-testing-and-e2e.md) |
| G-04 | Нет пакета развёртывания worker на Apple Silicon | В `deploy/README.md`: «MPS remains a native-host worker»; нет launchd-юнита, установщика, doctor-сценария для Mac |
| G-05 | Биллинг B2–B4 | Нет Stripe-интеграции (в коде не найдено), webhook, ledger-интеграции, UI кабинета, продаж; этап 1Б.7 не принят |
| G-06 | Видеоредактор: кода нет | Только 24 документа в `docs/video`; следить, чтобы UI/портал не обещали видео |
| G-07 | HA/DR | В плане заявлены RPO ≤ 15 мин / RTO ≤ 4 ч «как критерии реального drill»; drill не проведён; единственный экземпляр БД и брокера |
| G-08 | Матрица поддерживаемых камер/форматов/пресетов | MVP-план: «Матрицу нельзя придумывать до измерения»; измерений нет → UI не может честно обещать поддержку |
| G-09 | Квалификация GPU-операций | Большинство операций «awaiting_evidence»; отсутствие квалификации = `unavailable` (корректное поведение, но функциональность MVP зависит от измерений) |
| G-10 | Allowlist функций MVP в клиенте | Маршруты отложенных функций присутствуют в `apps/photographer/app/[locale]/` |
| G-11 | Нативные приложения | Принятие iOS/Android вынесено за MVP; тесты — на react-native-web |

## C. Потенциальные проблемы (риски; часть — гипотезы)

| ID | Риск | Статус | Как проверить |
|---|---|---|---|
| R-01 | Нагрузка на команду: нечитаемый код + 2,6 тыс. миграций + церемонии квалификации → высокий bus factor, дорогой онбординг | подтверждено косвенно | Время «чистая машина → зелёные тесты → первый PR» |
| R-02 | Время установки с нуля: цепочка из 2 626 миграций, 27 МБ SQL в R6-кандидате, лимит чтения migration source уже поднят до 64 MiB | гипотеза | Замер `migrate` на чистой БД; baseline-снимок |
| R-03 | Разрастание `SECURITY DEFINER`-функций (владелец `photo_fn_owner`): сложный аудит, риск эскалации привилегий при ошибке в одной из сотен функций | гипотеза | Автоматический линт ACL/`search_path` для всех функций (`scripts/check-sql-function-syntax.py` — расширить) |
| R-04 | Scheduler — единственный лидер (advisory lock); при потере соединения dispatch останавливается (по дизайну) | подтверждено документом `18-capacity-operations.md` | Нагрузочный тест failover + алерт |
| R-05 | AsyncStorage на web = `localStorage` (≈5–10 МБ): 55 журналов попыток и журнал загрузки тысяч файлов могут упереться в квоту | гипотеза | Загрузка 2–5 тыс. файлов в браузере, мониторинг `QuotaExceededError` |
| R-06 | Клиентский SHA-256 всего файла перед загрузкой (`webSource`, `nativeSource`): на больших RAW-сериях — долгие паузы и память | гипотеза | Профилировать импорт 500 RAW по 50–80 МБ |
| R-07 | Два пути доставки портала: контейнер `deploy/images/portal` (compose) и Cloudflare Worker (`deploy/framevela/portal/portal-worker.mjs`) | подтверждено | Выбрать один для MVP |
| R-08 | Регистрация воркеров вручную через SQL (`deploy/provision-*-worker.sql`, `activate-*.sql`) | подтверждено | Автоматизировать (`doctor` + seed) |
| R-09 | `x-consumer-timeout` 4 ч в очередях, `x-delivery-limit` 20 | подтверждено `queue/topology.go:42` | Сверить с максимальным deadline задач GPU и стратегией retry |
| R-10 | Смешение «режимов правды»: статусы в сотнях документов, evidence в `work/` (≈4 200 записей, не в VCS) | подтверждено | Переехать на машинно-читаемый реестр в репозитории/хранилище артефактов |

## D. Что проверено и не вызвало замечаний

* `tsc --noEmit` для `apps/portal` (≈2,7 с) и `apps/photographer` (≈12 с) — **0 ошибок**.
* Cookie аутентификации: `HttpOnly`, `SameSite=Lax`, `Secure` в production (`backend/internal/httpapi/auth.go:25`).
* Портал: секрет шаринга во фрагменте URL, `Referrer-Policy: no-referrer`, SSR без данных коллекции.
* Compose: `read_only`, `cap_drop: ["ALL"]`, `no-new-privileges`, без дефолтных паролей и без mutable-тегов.
* В просмотренных файлах секретов (паролей, ключей) не обнаружено; провижининг генерирует их локально и не печатает.
