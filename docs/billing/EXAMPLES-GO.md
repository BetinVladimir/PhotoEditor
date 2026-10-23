# EXAMPLES-GO — образцы Go-кода биллинга

Сгенерировано `tools/gen-examples.py` из живых файлов. Вручную не править. Копировать форму, не содержимое.

## 1. Обёртка вокруг SQL-функции: Prepare с холодным таймаутом, Bind с hash запроса, MapError

Правила формы: Prepare берёт блокировки биллинга раньше run/quota locks; холодное ожидание ограничено 200 мс и даёт `BILLING_UNAVAILABLE` 503; Bind принимает `canonical.Hash(request)` как digest; все SQL-ошибки идут через `MapError`.

### Источник: `backend/internal/billing/workflow.go (PrepareApply)`

```go
func PrepareApply(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, who auth.Identity, route string) (bool, error) {
	// Take billing locks after the API receipt, before collection/run/quota locks.
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_apply($1,$2,$3,$4)`, tenantID, who.UserID, who.SessionID, route).Scan(&enabled)
	if err != nil && ctx.Err() == nil && errors.Is(cold.Err(), context.DeadlineExceeded) {
		return false, domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return enabled, MapError(err)
}
```

### Источник: `backend/internal/billing/workflow.go (BindApply)`

```go
func BindApply(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, who auth.Identity, runID uuid.UUID, key string, route string, request any) error {
	hash, err := canonical.Hash(request)
	if err != nil {
		return err
	}
	digest, err := hex.DecodeString(hash)
	if err != nil {
		return err
	}
	var requestID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT photo.billing_bind_routed_apply($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, who.UserID, who.SessionID, runID, key, digest, instanceID, route).Scan(&requestID)
	return MapError(err)
}
```

### Источник: `backend/internal/billing/workflow.go (MapError)`

```go
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	if pg.Code == "55P03" {
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	switch pg.Message {
	case "BILLING_QUOTA_EXCEEDED", "BILLING_OVERRUN_BLOCKED", "BILLING_BETA_BUDGET_EXCEEDED":
		return domain.NewError("BILLING_QUOTA_EXCEEDED", 409)
	case "PHOTO_INITIAL_IN_PROGRESS":
		return domain.NewError("PHOTO_INITIAL_IN_PROGRESS", 409)
	case "BILLING_IDEMPOTENCY_CONFLICT":
		return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "BILLING_AUTHORITY_INVALID":
		return domain.NewError("FORBIDDEN", 403)
	case "BILLING_OPERATION_POLICY_REQUIRED", "BILLING_UNAVAILABLE",
		"BILLING_PERIOD_INVALID", "BILLING_ACCOUNTING_DISABLED", "BILLING_POLICY_INVALID", "BILLING_PERIOD_POLICY_PINNED":
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return err
}
```

## 2. HTTP-handler с Idempotency-Key, интерфейсом зависимости и защитой от поздних ответов

Форма: интерфейс сервиса → проверка контекста → route id → `idempotency.ValidKey` → decodeJSON → Validate → вызов сервиса → проверка контекста → writeError/writeJSON.

### Источник: `backend/internal/httpapi/delivery_download_intent.go`

```go
)

type deliveryDownloadIntentCreator interface {
	Create(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, delivery.DownloadIntentRequest) (delivery.DownloadIntent, error)
}

func serveDeliveryDownloadIntent(w http.ResponseWriter, r *http.Request, who auth.Identity, tid uuid.UUID, creator deliveryDownloadIntentCreator) {
	if r.Context().Err() != nil {
		return
	}
	packageID, err := routeID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if packageID == uuid.Nil || !idempotency.ValidKey(r.Header.Get("Idempotency-Key")) {
		writeError(w, r, domain.NewError("VALIDATION_FAILED", 422))
		return
	}
	var request delivery.DownloadIntentRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, r, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, r, err)
		return
	}
	if creator == nil {
		writeError(w, r, domain.NewError("DELIVERY_UNAVAILABLE", 503))
		return
	}
	intent, err := creator.Create(r.Context(), who, tid, packageID, r.Header.Get("Idempotency-Key"), request)
	// A late response must not disclose a bearer token to a cancelled request.
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := time.Now()
	token, tokenErr := base64.RawURLEncoding.Strict().DecodeString(intent.Token)
```

Регистрация маршрута: см. `backend/internal/httpapi/catalog.go` (блок `r.Post("/api/v1/tenants/{t}/legacy-imports/reconcile"` — `s.tenantRequest`, `decodeJSON`, `writeJSON`).
