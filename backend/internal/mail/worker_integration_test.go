package mail

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"testing"
)

type recordingSender struct {
	messages []Message
	fail     bool
}

func (s *recordingSender) Send(_ context.Context, m Message) error {
	s.messages = append(s.messages, m)
	if s.fail {
		return errors.New("unknown SMTP outcome")
	}
	return nil
}
func TestIntegrationMailRetryAndSecretErasure(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx := context.Background()
	pool := func(role string) *pgxpool.Pool {
		cfg, e := pgxpool.ParseConfig(dsn)
		if e != nil {
			t.Fatal(e)
		}
		if role != "" {
			cfg.ConnConfig.RuntimeParams["role"] = role
		}
		p, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	admin := pool("")
	key := make([]byte, 32)
	rand.Read(key)
	ring := auth.KeyRing{Active: "test", Keys: map[string][]byte{"test": key}}
	a := auth.Service{DB: pool("photo_auth"), Pepper: key, MailKeys: ring, Registration: true}
	challenge, err := a.RequestOTP(ctx, uuid.NewString()+"@example.test", "ru", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	var payload, event uuid.UUID
	err = admin.QueryRow(ctx, `SELECT m.id,o.event_id FROM photo.mail_payloads m JOIN photo.auth_mail_outbox o ON o.payload_id=m.id WHERE m.challenge_id=$1`, challenge.ID).Scan(&payload, &event)
	if err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{fail: true}
	worker := Worker{DB: pool("photo_mail"), ID: uuid.New(), Keys: ring, Sender: sender}
	command := Command{SchemaVersion: 1, EventID: event, Operation: "mail.send", PayloadID: payload, Attempt: 1, CorrelationID: event}
	raw, _ := json.Marshal(command)
	if err = worker.Process(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatal("no mail attempt")
	}
	if err = worker.Process(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatal("duplicate resent before scheduled retry")
	}
	var status string
	if err = admin.QueryRow(ctx, "SELECT status FROM photo.mail_payloads WHERE id=$1", payload).Scan(&status); err != nil || status != "retry" {
		t.Fatal(status, err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.mail_payloads SET next_attempt_at=now()-interval '1 second' WHERE id=$1", payload); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.DB.Exec(ctx, "SELECT photo.reconcile_mail(100)"); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	command.Attempt = 2
	command.EventID = uuid.New()
	raw, _ = json.Marshal(command)
	if err = worker.Process(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || sender.messages[0].ID != sender.messages[1].ID || sender.messages[0].Body != sender.messages[1].Body {
		t.Fatal("retry changed OTP or message ID")
	}
	var bytes int
	if err = admin.QueryRow(ctx, "SELECT status,octet_length(recipient_cipher)+octet_length(payload_cipher) FROM photo.mail_payloads WHERE id=$1", payload).Scan(&status, &bytes); err != nil || status != "sent" || bytes != 0 {
		t.Fatal("secret retained", status, bytes, err)
	}
	if err = worker.Process(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 {
		t.Fatal("terminal duplicate resent")
	}
	if _, err = worker.DB.Exec(ctx, "SELECT * FROM photo.mail_payloads"); err == nil {
		t.Fatal("mail role has broad table access")
	}
}
