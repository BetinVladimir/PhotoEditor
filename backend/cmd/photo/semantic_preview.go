package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticreview"
	"time"
)

func buildSemanticPreview(db *pgxpool.Pool, storage *objectstore.Store, bucket, srgbSHA string) (*semanticreview.ExposurePreviewProvider, error) {
	lockSHA, err := colorprofile.LoadLockSHA(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return nil, err
	}
	resolve, err := semanticreview.SQLExposurePreviewResolver(db, storage, bucket)
	if err != nil {
		return nil, err
	}
	sign := func(ctx context.Context, object objectstore.Object, expiry time.Time) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !expiry.After(time.Now()) {
			return "", semanticreview.ErrInvalid
		}
		signed, err := storage.SignRead(ctx, object, time.Minute)
		if err != nil {
			return "", err
		}
		if signed.ExpiresAt.Before(expiry) {
			return "", semanticreview.ErrInvalid
		}
		return signed.URL, nil
	}
	return semanticreview.NewExposurePreviewProvider(db, storage, bucket, resolve, sign, lockSHA, srgbSHA)
}

func buildSemanticRemovalPreview(db *pgxpool.Pool, storage *objectstore.Store, bucket, srgbSHA string) (*semanticreview.RemovalPreviewProvider, error) {
	lockSHA, err := colorprofile.LoadLockSHA(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return nil, err
	}
	resolve, err := semanticreview.SQLRemovalPreviewResolver(db, storage, bucket)
	if err != nil {
		return nil, err
	}
	sign := func(ctx context.Context, object objectstore.Object, expiry time.Time) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !expiry.After(time.Now()) {
			return "", semanticreview.ErrInvalid
		}
		signed, err := storage.SignRead(ctx, object, time.Minute)
		if err != nil {
			return "", err
		}
		if signed.ExpiresAt.Before(expiry) {
			return "", semanticreview.ErrInvalid
		}
		return signed.URL, nil
	}
	return semanticreview.NewRemovalPreviewProvider(db, storage, bucket, resolve, sign, lockSHA, srgbSHA)
}

func buildSemanticBackgroundCleanupPreview(db *pgxpool.Pool, storage *objectstore.Store, bucket, srgbSHA string) (*semanticreview.BackgroundCleanupPreviewProvider, error) {
	lockSHA, err := colorprofile.LoadLockSHA(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return nil, err
	}
	resolve, err := semanticreview.SQLBackgroundCleanupPreviewResolver(db, storage, bucket)
	if err != nil {
		return nil, err
	}
	sign := func(ctx context.Context, object objectstore.Object, expiry time.Time) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !expiry.After(time.Now()) {
			return "", semanticreview.ErrInvalid
		}
		signed, err := storage.SignRead(ctx, object, time.Minute)
		if err != nil {
			return "", err
		}
		if signed.ExpiresAt.Before(expiry) {
			return "", semanticreview.ErrInvalid
		}
		return signed.URL, nil
	}
	return semanticreview.NewBackgroundCleanupPreviewProvider(db, storage, bucket, resolve, sign, lockSHA, srgbSHA)
}

func buildSemanticRetouchPreview(db *pgxpool.Pool, storage *objectstore.Store, bucket, srgbSHA string) (*semanticreview.RetouchPreviewProvider, error) {
	lockSHA, err := colorprofile.LoadLockSHA(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return nil, err
	}
	resolve, err := semanticreview.SQLRetouchPreviewResolver(db, storage, bucket)
	if err != nil {
		return nil, err
	}
	sign := func(ctx context.Context, object objectstore.Object, expiry time.Time) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !expiry.After(time.Now()) {
			return "", semanticreview.ErrInvalid
		}
		signed, err := storage.SignRead(ctx, object, time.Minute)
		if err != nil {
			return "", err
		}
		if signed.ExpiresAt.Before(expiry) {
			return "", semanticreview.ErrInvalid
		}
		return signed.URL, nil
	}
	return semanticreview.NewRetouchPreviewProvider(db, storage, bucket, resolve, sign, lockSHA, srgbSHA)
}
