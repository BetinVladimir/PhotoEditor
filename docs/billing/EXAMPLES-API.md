# EXAMPLES-API — образцы endpoint и DTO

Сгенерировано `tools/gen-examples.py`. Вручную не править.

Правила (09 §1, §7): новые идентичности добавлять в ОБА файла endpoints.json (`docs/implementation/contracts/` и `docs/implementation-v2/contracts/`); DTO — в `docs/implementation-v2/contracts/schemas.json`, `$defs`; `additionalProperties=false`, явный `required`, запрос не принимает server-only суммы, actor и tenant; tenant — параметр пути `{t}`.

### Источник: `docs/implementation-v2/contracts/endpoints.json` (формат v2)

```json
{
  "method": "POST",
  "path": "/api/v1/tenants/{t}/download-jobs/{id}/download-intent",
  "request_schema": "OwnerDownloadIntentRequest",
  "response_schema": "OwnerDownloadIntent",
  "handler": "Download.CreateIntent",
  "package": "V1",
  "role": "viewer",
  "success_status": 201,
  "idempotency": true
}
```

### Источник: `docs/implementation/contracts/endpoints.json` (формат v1: другие имена полей)

```json
{
  "method": "POST",
  "path": "/api/v1/tenants/{t}/capture-uploads",
  "authorization": "device",
  "request": "CaptureDeviceUploadBegin",
  "response": "CaptureDeviceUploadSession",
  "status": 201,
  "handler": "Upload.BeginDevice",
  "idempotency": true,
  "revision_required": false
}
```

### Источник: `docs/implementation-v2/contracts/schemas.json`, `$defs`

```json
{
  "OwnerDownloadIntentRequest": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "volume": {
        "type": "integer",
        "minimum": 1,
        "maximum": 5000
      }
    },
    "required": [
      "volume"
    ]
  },
  "OwnerDownloadIntent": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "intent_token": {
        "type": "string",
        "pattern": "^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$"
      },
      "expires_at": {
        "type": "string",
        "format": "date-time"
      }
    },
    "required": [
      "intent_token",
      "expires_at"
    ]
  }
}
```
