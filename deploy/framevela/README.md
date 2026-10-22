# Framevela deployment

Состояние развёртывания: 20 октября 2026, время сервера UTC 19 октября.

## Публичные адреса

- https://framevela.com — Cloudflare Worker `framevela-portal`, новая корневая страница о проекте.
- https://api.framevela.com — Caddy → Go API, HTTPS, `/health/ready` отвечает 200.
- https://storage.framevela.com — Caddy → закрытый MinIO с внутренним TLS. Объекты доступны только по разрешённым подписанным ссылкам.
- https://framevela-portal.beeloy-web.workers.dev — дополнительный адрес портала.

Worker version: `c7006f66-d0dc-4841-acef-97a5f0be6d80`. Route `framevela.com/*`, account `434d7cf69d6175676c4fdcf9167a5033`, zone `655180ec4477ee9712841a4b0f61a58e`.

## Сервер

`root@178.104.19.245`, `/srv/photoeditor`, Docker Compose. PostgreSQL bind mount `/mnt/HC_Volume_107202006/postgres`; запуск запрещён без смонтированного диска. Только Caddy публикует порты 80/443. PostgreSQL, RabbitMQ, MinIO и внутренние сервисы не публикуют порты хоста. Все рабочие подключения к PostgreSQL используют отдельные ограниченные роли, TLS; bootstrap-владелец не используется в runtime.

Исходники образа: `/srv/photoeditor/release`. Закреплённые digest образов: `/srv/photoeditor/release/image-pins.json`, серверный `.env`. MinIO собран из закреплённого upstream Go module вместо недоступного registry-образа, исходная лицензия включена.

Работают: API, object-gateway, publisher, dead-letter-worker, collection-worker, maintenance, mail-worker, PostgreSQL, RabbitMQ, MinIO, Caddy. Проверка готовности всех перечисленных приложений пройдена после повторного штатного `bootstrap-queue`.

**Обработка фотографий целиком пока не введена в эксплуатацию.** Scheduler остановлен и вынесен в профиль `qualified-render`: в исходниках нет квалифицированного измеренного Render calibration catalog. Нельзя подменять его тестовыми измерениями. Нужен реальный canonical JSON в `/srv/photoeditor/config/render-calibration.json`, SHA256 в серверном `.env` как `PHOTO_RENDER_CALIBRATION_SHA256`; после квалификации запускать `docker compose -f compose.json --profile qualified-render up -d scheduler`. CPU/GPU вычислительные исполнители и модели ещё не установлены. У сервера 2 vCPU/4 ГБ RAM, GPU нет; RTX установка остаётся отложенной. Регистрация новых пользователей отключена (`REGISTRATION_ENABLED=false`), tenant bootstrap ещё не выполнен.

Миграции применены из embedded bundle текущего Go executable до версии 2597. Незавершённые рабочие migration overlays 026xx в это развёртывание не включены. Это не приёмка всей системы по плану implementation-v2. Автоматические внешние резервные копии и проверка восстановления ещё не настроены.

## Почта

Исходящая: Brevo `smtp-relay.brevo.com:587`, STARTTLS, логин `a4d164001@smtp-brevo.com`, From `noreply@mail.framevela.com`. Пароль только `/srv/photoeditor/secrets/mail/smtp-password`, UID 10001, режим 600; значение не копировать в репозиторий и журналы. SMTP STARTTLS + authentication PASS; доставка в почтовый ящик этим тестом не подтверждается. Доступ сервиса к внешней сети нужен для SMTP.

Входящая: Cloudflare Email Routing включён для framevela.com и mail.framevela.com. Catch-all выключен. 2026-10-20 пользователь уточнил адрес назначения: `beeloy.org@gmail.com` (Verified). Создано и визуально проверено активное правило `noreply@mail.framevela.com` → `beeloy.org@gmail.com` (Active). Доставка реального входящего письма этим подтверждением не доказана. Ошибочно указанный ранее `brrloy.org@gmail.com` не используется правилом. SPF/DKIM Cloudflare Routing не подтверждают домен отправителя в Brevo; записи Brevo брать только из реального аккаунта провайдера.

## Управление

Все команды на сервере из `/srv/photoeditor`, обязательно `-f compose.json` (Compose не обнаруживает JSON по умолчанию).

```sh
docker compose -f compose.json ps
docker compose -f compose.json --profile mail up -d --wait api object-gateway publisher dead-letter-worker collection-worker maintenance mail-worker
docker compose -f compose.json --profile bootstrap run --rm --no-deps bootstrap-queue
```

`provision.py` предназначен для первой установки; секреты сохраняются, существующие не перезаписываются. `roles.sql` — одноразовое создание ролей, повторный запуск не идемпотентен. На текущем сервере mail role создана отдельным приватным SQL. Не повторять инициализацию базы, не удалять volumes. Миграции запускать после резервного копирования и проверки целевой версии; не считать этот файл разрешением на будущий destructive upgrade.

## Повторная сборка портала

Production dependencies исходного Next приложения сохранены; Cloudflare сборка изолирована и воспроизводится с lockfile `portal/package-lock.json`. `portal/prepare.py` копирует актуальный `apps/portal/src` и `public`, конфигурацию и публичный API origin в новый каталог внутри репозитория.

```sh
python3 deploy/framevela/portal/prepare.py work/portal-next-release
cd work/portal-next-release
npm ci
npm run build:vinext
npx wrangler deploy --config dist/server/wrangler.json
```

Для Wrangler использовать уже авторизованный локальный профиль; OAuth/token не помещать в исходники. После каждого deploy проверить обычный HTTPS, главную страницу, локализованные страницы, ошибки недействительной ссылки и заголовки безопасности. Ответы приложения `no-store`; статические ресурсы обслуживает Workers Assets. Локальный DNS в момент развёртывания возвращал старый parking IP, поэтому публичный HTTPS дополнительно проверен с сервера без отключения проверки сертификатов.

Evidence: `work/portal-cloudflare-20261020/`, серверные `/srv/photoeditor/release/*-check.json`, `services-recheck.log`, `migrate.log`. Эти проверки не заменяют полный end-to-end тест реальной клиентской галереи.

Дополнительная проверка: стандартный HTTPS-клиент получил 200 для `/`, `/ru`, `/en`, `/bg` и `/health/ready` API, а анонимный доступ к bucket — 403 XML от MinIO. Python urllib получил 403 на edge; причина фильтрации отдельно не установлена, настройка защиты не ослаблялась.
