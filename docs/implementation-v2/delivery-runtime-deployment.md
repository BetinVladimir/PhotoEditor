# V30: подключение delivery runtime

Статус: код написан, проверки не запускались; миграции01216–01222 не применялись в этом этапе. Это описание подключения, не production acceptance.

Стандартный WorkerConfig.from_env принимает PHOTO_DELIVERY_ENABLED=true|false. По умолчанию false. true разрешён только при PHOTO_WORKER_CLASS=cpu. Неверные значения приводят к WorkerConfigurationError. Остальные ограничения, credentials и capacity policy существующего CPU-worker сохраняются.

При true build_worker создаёт DeliveryInstallation с SQLDeliveryAdmission на том же TaskDatabase, который используется executor. Не допускается одновременно передавать custom delivery_installation: конфликт завершает создание worker ошибкой. При false сохранена возможность явно передать installation для embedded deployment/tests. Проверка SQL admission не подменяется permissive callback.

Установленные обработчики: v2.delivery.prepare и v2.delivery.assemble, версия1, CPU, renderer v2.2. До чтения входов и перед staging результата повторяется lease/source admission. SQL source gate assembly сейчас возвращает false; настройка не обходит это ограничение. Readiness подготовки требует immutable proof и всей сохранённой авторизации/closure; сама установка обработчика не создаёт workflow/tasks и не включает registry.

Порядок будущего ввода: завершить orchestration и assembly authority; применить полный последовательный набор forward-only миграций; выполнить unit/SQL/security/GC/lease/recovery тесты; квалифицировать operation registry и output policy; лишь затем включить setting на CPU-worker и разрешить admission задач. Не включать незаквалифицированный registry для обхода проверки готовности.

Проверки, написанные в test_delivery_installation.py: закрытый default, строгие значения env, запрет чужих pools, сохранение настройки в WorkerConfig, привязка SQL admission к тому же database, отказ source gate, конфликт override, отсутствие нужного SQL-интерфейса. Тесты NOT RUN. Полный process startup, реальные SQL роли, dispatch/lease/adoption, restart, публикация и физическая проверка артефактов остаются открытыми.
