package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/billingprovider"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/database"
)

func readPrivateStripeFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Stripe webhook secret file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("Stripe webhook secret file must be private and regular")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) == 0 || len(data) > 4096 {
		return nil, errors.New("Stripe webhook secret file invalid")
	}
	return data, nil
}

func installStripeWebhookIngress(ctx context.Context, cfg config.StripeIngressConfig) (billing.WebhookIngressService, func(), error) {
	noop := func() {}
	if !cfg.Enabled {
		return nil, noop, nil
	}
	signing, err := readPrivateStripeFile(cfg.WebhookSecretRef)
	if err != nil {
		return nil, noop, err
	}
	secret := strings.TrimSpace(string(signing))
	clear(signing)
	if !regexp.MustCompile(`^whsec_[A-Za-z0-9]+$`).MatchString(secret) {
		return nil, noop, errors.New("Stripe webhook signing secret invalid")
	}
	encodedKey, err := readPrivateStripeFile(cfg.PayloadKeyRef)
	if err != nil {
		return nil, noop, err
	}
	payloadKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encodedKey)))
	clear(encodedKey)
	if err != nil || len(payloadKey) != 32 {
		return nil, noop, errors.New("Stripe webhook payload key must contain base64 AES-256 key")
	}
	defer clear(payloadKey)
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, noop, errors.New("Stripe webhook database configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	poolConfig.MaxConns = 4
	database.GuardApplicationRole(poolConfig)
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, noop, errors.New("Stripe webhook database unavailable")
	}
	fail := func(err error) (billing.WebhookIngressService, func(), error) { pool.Close(); return nil, noop, err }
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if database.RequireApplicationRole(startup, pool, "photo_maintenance") != nil {
		return fail(errors.New("Stripe webhook database role installation invalid"))
	}
	var ready bool
	if pool.QueryRow(startup, `SELECT photo.billing_stripe_ingress_ready($1,$2,$3)`, cfg.ProviderAccountID, cfg.AccountID, cfg.APIVersion).Scan(&ready) != nil || !ready {
		return fail(errors.New("Stripe Sandbox inbox schema or account installation invalid"))
	}
	parser, err := billingprovider.NewStripeEventParser(cfg.AccountID, cfg.APIVersion)
	if err != nil {
		return fail(errors.New("Stripe webhook parser configuration invalid"))
	}
	recorder, err := billingprovider.NewPostgresWebhookRecorder(pool, cfg.ProviderAccountID, cfg.AccountID, cfg.APIVersion, auth.KeyRing{Active: cfg.PayloadKeyID, Keys: map[string][]byte{cfg.PayloadKeyID: payloadKey}})
	if err != nil {
		return fail(err)
	}
	ingress := billingprovider.NewWebhookIngress(parser, recorder, secret)
	return billing.StripeWebhookReceiver{Ingress: ingress, ProviderAccountID: cfg.ProviderAccountID, ExternalAccountID: cfg.AccountID}, pool.Close, nil
}
