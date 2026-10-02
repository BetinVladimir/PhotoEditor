package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"time"
)

type Command struct {
	SchemaVersion int       `json:"schema_version"`
	EventID       uuid.UUID `json:"event_id"`
	Operation     string    `json:"operation"`
	PayloadID     uuid.UUID `json:"payload_id"`
	Attempt       int       `json:"attempt"`
	CorrelationID uuid.UUID `json:"correlation_id"`
}

var ErrPoison = errors.New("invalid mail command")

func DecodeCommand(raw []byte) (Command, error) {
	var c Command
	if len(raw) > 65536 {
		return c, ErrPoison
	}
	b, err := canonical.Bytes(raw)
	if err != nil {
		return c, ErrPoison
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || c.SchemaVersion != 1 || c.Operation != "mail.send" || c.EventID == uuid.Nil || c.PayloadID == uuid.Nil || c.CorrelationID == uuid.Nil || c.Attempt < 1 || c.Attempt > 4 {
		return c, ErrPoison
	}
	return c, nil
}

type Worker struct {
	DB     *pgxpool.Pool
	ID     uuid.UUID
	Keys   auth.KeyRing
	Sender Sender
}

func (w *Worker) Process(ctx context.Context, raw []byte) error {
	c, err := DecodeCommand(raw)
	if err != nil {
		return err
	}
	var outcome string
	var fence *int64
	var recipientCipher, payloadCipher []byte
	var kid, id *string
	var expires *time.Time
	err = w.DB.QueryRow(ctx, "SELECT outcome,fence,recipient_cipher,payload_cipher,key_id,message_id,expires_at FROM photo.claim_mail($1,$2,$3)", c.PayloadID, c.Attempt, w.ID).Scan(&outcome, &fence, &recipientCipher, &payloadCipher, &kid, &id, &expires)
	if err != nil {
		return err
	}
	switch outcome {
	case "missing", "terminal", "busy", "stale":
		return nil
	case "claimed":
	default:
		return errors.New("unknown mail claim outcome")
	}
	if fence == nil || kid == nil || id == nil || expires == nil {
		return errors.New("invalid mail claim")
	}
	sent, retryable := false, false
	recipient, e := w.Keys.Open(*kid, recipientCipher, []byte(c.PayloadID.String()+":recipient"))
	if e == nil {
		var plain []byte
		plain, e = w.Keys.Open(*kid, payloadCipher, []byte(c.PayloadID.String()+":payload"))
		if e == nil {
			var p struct {
				Kind   string `json:"kind"`
				Code   string `json:"code,omitempty"`
				Locale string `json:"locale"`
				Tenant string `json:"tenant,omitempty"`
				Token  string `json:"token,omitempty"`
			}
			d := json.NewDecoder(bytes.NewReader(plain))
			d.DisallowUnknownFields()
			e = d.Decode(&p)
			if e == nil && (p.Kind == "otp" || p.Kind == "invite") {
				var message Message
				if p.Kind == "otp" {
					message, e = OTPMessage(string(recipient), *id, p.Locale, p.Code)
				} else {
					message, e = InviteMessage(string(recipient), *id, p.Locale, p.Tenant, p.Token)
				}
				if e == nil {
					sendCtx, cancel := context.WithDeadline(ctx, *expires)
					e = w.Sender.Send(sendCtx, message)
					cancel()
					sent = e == nil
					retryable = !sent
				}
			}
		}
	}
	var saved bool
	if err = w.DB.QueryRow(ctx, "SELECT photo.finish_mail($1,$2,$3,$4,$5)", c.PayloadID, *fence, w.ID, sent, retryable).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return errors.New("mail lease lost")
	}
	return nil
}
