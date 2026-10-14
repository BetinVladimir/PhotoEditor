# V30: установка проверки импортируемых стилей

## Runtime-конфигурация API

`PHOTO_V30_IMPORT_STYLE_CONFIG` — абсолютный путь к JSON-файлу размером 2..1048576 байт. `PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256` — lowercase SHA256 **точных байтов файла**, включая пробелы и переносы. Обе переменные задаются совместно. Без обеих factory отключена и approve, требующий квалификации стиля, возвращает dependency unavailable; reject-only review не требует factory. Ошибка заданной конфигурации останавливает startup API.

Файл содержит только следующие поля:

| Поле | Значение |
| --- | --- |
| `schema_version` | `interchange-review-style-config/v1` |
| `python_path` | Абсолютный путь к исполняемому Python установленного worker runtime |
| `worker_root` | Абсолютный каталог, содержащий `photo_worker` |
| `scratch_root` | Существующий выделенный абсолютный каталог; constructor отвергает symlink самого каталога |
| `scratch_bytes` | Общий process budget native style scratch, 23..137438953472 байт |
| `timeout_seconds` | Deadline отдельного native style probe, 1..600 секунд |
| `runtime_sha256` | SHA256 независимо квалифицированного runtime, не SHA конфигурации |
| `models` | Объект installed model policy: ключ модели → значение, ожидаемое StyleV2 validator; не более 1024 записей, ключ/значение 1..240 байт |

Не подставлять фиктивные runtime/model pins. Пустой `models` является пустой политикой допустимых моделей, а не разрешением любой модели. Установленный work ICC берётся из `PHOTO_ICC_ROOT` через проверенный registry `work_rec2020_gamma24_v1`; импорт не меняет обработку существующих стилей.

Startup проверяет конфигурацию, пути и создаёт одну scratch budget instance на factory. Он не запускает Python, не подтверждает физическую native qualification, не применяет миграции и не выдаёт database privileges. Каталог scratch должен быть выделен API и ограничен filesystem quota; process budget не заменяет ограничение диска между процессами. Для будущих ресурсов в том же каталоге нужно переиспользовать общую budget instance, а не создавать независимые счётчики.

## Approval

Factory создаёт отдельный qualifier для текущего workflow, entry и target binding. Master загружается через project/version-scoped каталог; SQL повторяет текущую review authority. Проверяются hash/version/master canvas, ICC/encoding и installed renderer/raw ABI, затем worker probe выполняет полный StyleV2 parser/model policy. Исторические quality/provenance claims не дают approval: импортированные quality остаются needs_review/unvalidated до текущего пользовательского решения.

Регистрация стиля и фона выполняется атомарно с DecideImport; отдельный Style.Accept не требуется. Миграция `01385_v30_interchange_style_version_allocation.sql` сохраняет content-derived style ID и deterministic version UUID. Под блокировкой строки styles новый import получает max(version_no)+1; точный replay использует прежний номер. Existing config/content immutable; исчерпание int32 range явно отклоняется.

## Статус доказательств

Конфигурационные unit-тесты написаны, включая положительное создание factory без исполнения sentinel Python и отказы по hash, schema, unknown/trailing JSON, paths, budget, deadline, runtime/models/ICC. Они не запускались. Миграции не применялись. Полный положительный SQL approval lifecycle, параллельная регистрация версий, native parsing реальным worker и дальнейший render остаются задачами отложенного глубокого тестирования. Этот документ не является доказательством готовности V30.

## Связь регистрации и рецепта

Миграция 01386 требует internal style_registration для каждого approve. Receipt включает точные байты загруженного authored recipe; Go проверяет typed EditingRecipeV3, а SQL независимо сравнивает SHA этих байтов с locked current target recipe, base_style с регистрируемым стилем и source с target binding. Без receipt approve отклоняется; reject-only не меняется. Обновление API и установка миграций должны быть согласованы: прежний four-field resolved approval новый commit не принимает. Этот receipt не является полем публичного DecideImport request.
