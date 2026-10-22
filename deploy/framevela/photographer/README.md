# Photographer web на Cloudflare

Worker `framevela-photographer`, account `434d7cf69d6175676c4fdcf9167a5033`, custom domain `app.framevela.com`. Конфигурация обслуживает Expo SPA и сохраняет локализованные пути `/ru/login`, `/en/login`, `/bg/register`. Портал использует отдельный Worker.

Текущий проверенный экспорт: `work/photographer-web-localized-r2`, API origin `https://api.framevela.com`. Не заменять его loopback-preview сборкой. Перед повторным развёртыванием собрать новый export из актуального приложения, проверить исходники, API origin и обновить `assets.directory` в `wrangler.jsonc` на новый каталог. Предыдущий артефакт сохранить для сравнения и отката.

Из `apps/photographer` сборка Expo выполняется с `EXPO_PUBLIC_API_ORIGIN=https://api.framevela.com` и `expo export --platform web --output-dir` с новым каталогом внутри `work`. Использовать установленную версию Expo из lockfile. Текущий deployment проверен Wrangler 4.151.2 из изолированного portal toolchain; секреты и токены в config не входят.

Из корня репозитория выполнить последовательно, используя Wrangler этой версии:

```sh
wrangler deploy --config deploy/framevela/photographer/wrangler.jsonc --dry-run
wrangler deploy --config deploy/framevela/photographer/wrangler.jsonc
```

Первый запуск не доказывает доступность домена. После второго проверить обычный HTTPS, соответствие HTML/JS локальным хешам и отображение формы входа; не отправлять реальные клиентские данные для smoke-проверки.

Backend должен разрешать точные origins: `APP_ORIGIN=https://app.framevela.com`, `PORTAL_ORIGIN=https://framevela.com`. `CAPTURE_ORIGIN` указывает на приложение. MinIO server CORS содержит эти два origin без wildcard. Узкий скрипт `../update-frontend-origins.py` сначала проверяет существующие значения и inventory сервисов, сохраняет исходный Compose и меняет только эти поля; по умолчанию работает без записи. После применения обновить затронутые запущенные сервисы и проверить CORS. Скрипт не запускает миграции, не меняет секреты, не создаёт роли и не удаляет данные.

Развёртывание UI не означает готовность обработки фото: scheduler, worker/models, RTX и полная приёмка имеют отдельные незакрытые критерии в `docs/implementation-v2`.
