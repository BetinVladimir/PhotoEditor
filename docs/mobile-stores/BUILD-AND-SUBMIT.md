# Сборка и отправка только на тестирование

Утверждённый знак: `docs/brand/APPROVED.json`, камера + воспроизведение + AI. Идентификаторы приложений не выдумываются и не регистрируются автоматически в неизвестном аккаунте.

Конфигурация `apps/photographer/eas.json` содержит только `store-beta`: подписанная store-сборка для физических iOS-устройств и Android AAB. Android submit направлен строго в `internal`, статус `completed` означает доступ выбранным внутренним тестировщикам, а не публичный релиз. iOS submit загружает сборку в App Store Connect/TestFlight; публичная App Review и выпуск не выполняются. Профиля production и автоматической отправки нет.

До сборки подтвердить Apple Team, Google developer, Expo owner/project, существующие или новые Bundle ID и Android package. Передавать значения через FRAMEVELA_IOS_BUNDLE_IDENTIFIER, FRAMEVELA_ANDROID_PACKAGE, FRAMEVELA_EAS_PROJECT_ID, FRAMEVELA_EXPO_OWNER. Эти идентификаторы не секреты. Подпись и API-ключи хранить в сервисах credentials или защищённых файлах вне исходников; не вставлять в документацию.

Скрипт `apps/photographer/scripts/prepare-store-workspace.cjs` создаёт новый минимальный снимок внутри `work/`: приложение, локальный api-client и три каталога переводов. В нём нет backend, БД, моделей, фотографий клиентов, журналов и секретов. Снимок включает SHA256 каждого исходника. Он не отправляет файлы и не создаёт аккаунтов. После любых изменений приложения создавать новый снимок.

Запуск из корня репозитория:

```sh
node apps/photographer/scripts/prepare-store-workspace.cjs "$PWD/work/framevela-mobile-store-beta-001"
```

Внутри снимка инициализировать отдельный локальный Git-корень, чтобы EAS не архивировал основной репозиторий; выполнять команды EAS из `apps/photographer`. После подтверждения организации привязать реальный Expo project. Проверить итоговый EAS archive до первой загрузки. Не использовать EAS_NO_VCS с корнем основного репозитория.

```sh
eas build --profile store-beta --platform ios
eas build --profile store-beta --platform android
```

Отправлять только явный проверенный build ID после успешной сборки и проверки встроенного API, подписей, иконок и основных сценариев на устройствах:

```sh
eas submit --profile store-beta --platform ios --id ACTUAL_IOS_BUILD_ID
eas submit --profile store-beta --platform android --id ACTUAL_ANDROID_BUILD_ID
```

Это образцы команд: ACTUAL_* заменяются реальными ID. Не использовать `--latest` для неизвестного артефакта и не переводить Android в production/open testing. При первой отправке Google может потребовать ручную первоначальную загрузку AAB в Play Console. Группы TestFlight и список внутренних Google-тестировщиков выбираются из подтверждённых пользователем.

Текущий API в профиле — https://api.framevela.com. Этот адрес не доказывает, что worker и все операции доступны. Перед выдачей сборок проверить реальный тестовый workspace, почтовый OTP, доступ к обработке и безопасные фотографии. Не представлять незавершённую приёмку MVP как готовый продукт.

20.10.2026: EAS-сеанс доступен, проект приложения пока не привязан. В App Store Connect обнаружено требование принять обновлённое Apple Developer Agreement владельцем аккаунта; агент его не принимает. Подтверждение аккаунтов/идентификаторов и тестировщиков запрошено. Подписанных IPA/AAB и отправок пока нет. Юридические URL, декларации обработки данных и reviewer access требуют фактических сведений оператора, отмеченных в соседних документах.

Обновление подготовки: зависимости приведены к Expo SDK; expo install --check PASS, TypeScript PASS, 52 focused tests PASS. Устаревший baseUrl удалён, Node/React-типы заданы явно для TypeScript 6. shell-quote обновлён с 1.10.0 до исправленного 1.12.0; это не означает отсутствия остальных npm advisories. Свежий минимальный снимок — work/framevela-mobile-store-beta-002 (001 сохранён как предыдущий). Пользователь подтвердил корпоративные аккаунты и сообщил о принятии Apple Agreement; фактический следующий blocker — developer.apple.com открыт под другой Apple identity и отказывает в регистрации идентификатора. Google create-форма заполнена, декларации оставлены пользователю. Expo предлагает личный @vladimir.betin; создание проекта ждёт подтверждения.


Актуализация 20.10.2026: подтверждены EAS `@beeloy/framevela`, UUID `dde18c43-5867-4de5-b023-f9a1b843d9dc`, CLI anastatiia (Owner). Apple Team BEELOY EOOD U6TTUVHSL6; Bundle ID `com.framevela.photographer` зарегистрирован. Создана карточка App Store Connect `6824850193`, описание EN и keywords сохранены, релиз MANUAL проверен после перезагрузки. Прежние сведения выше о неподтверждённом Expo owner и недоступном Apple Developer исторические. Google карточка пока отсутствует; форма ждёт юридических деклараций компании. Подписанных сборок/отправок нет. Снимок 002 устарел относительно новой привязки и локализации: перед сборкой нужен новый снимок.
