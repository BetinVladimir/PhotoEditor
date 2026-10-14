package queue

import (
	"context"
	"errors"
	"testing"
)

func TestRestoreReplayInstallationRefusesUnqualifiedTargets(t *testing.T) {
	for _, mode := range []string{"valid", "plain_broker", "foreign_vhost", "default_vhost", "foreign_database", "unverified_database", "missing_guard", "budget", "guard_refusal"} {
		t.Run(mode, func(t *testing.T) {
			dsn := "postgres://operator:secret@restore.invalid/restore_db?sslmode=verify-full"
			broker := "amqps://operator:secret@broker.invalid/restore-vhost"
			installation := RestoreReplayInstallation{Database: "restore_db", Vhost: "restore-vhost", MaximumEvents: 300, AssertIsolation: func(context.Context) error { return errors.New("not isolated") }}
			switch mode {
			case "plain_broker":
				broker = "amqp://broker.invalid/restore-vhost"
			case "foreign_vhost":
				installation.Vhost = "production"
			case "default_vhost":
				installation.Vhost = "/"
			case "foreign_database":
				installation.Database = "production"
			case "unverified_database":
				dsn = "postgres://restore.invalid/restore_db?sslmode=require"
			case "missing_guard":
				installation.AssertIsolation = nil
			case "budget":
				installation.MaximumEvents = 0
			}
			if mode == "guard_refusal" {
				result, err := RunRestoreReplay(context.Background(), dsn, broker, installation)
				if err == nil || err.Error() != "not isolated" || !result.CompletedAt.IsZero() {
					t.Fatal("resources opened or completion returned before isolation")
				}
				return
			}
			cfg, err := validateRestoreReplayInstallation(dsn, broker, installation)
			if mode == "valid" {
				if err != nil || cfg.MaxConns != 2 || cfg.ConnConfig.RuntimeParams["role"] != "photo_publisher" {
					t.Fatal("installation", err)
				}
			} else if err == nil {
				t.Fatal("unqualified target accepted")
			}
		})
	}
}
