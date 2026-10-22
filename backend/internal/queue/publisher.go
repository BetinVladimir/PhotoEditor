package queue

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"sync"
	"time"
)

type Envelope struct {
	ID, EventID             uuid.UUID
	Destination, RoutingKey string
	Payload                 json.RawMessage
	Lease                   time.Time
	Kind                    string
}
type ConfirmedPublisher interface {
	Publish(context.Context, Envelope) error
}
type Publisher struct {
	channel *amqp.Channel
	returns chan amqp.Return
	mu      sync.Mutex
}

func NewPublisher(conn *amqp.Connection) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err = ch.Confirm(false); err != nil {
		ch.Close()
		return nil, err
	}
	return &Publisher{channel: ch, returns: ch.NotifyReturn(make(chan amqp.Return, 1))}, nil
}
func (p *Publisher) Close() error { return p.channel.Close() }
func (p *Publisher) Publish(ctx context.Context, e Envelope) error {
    if err:=ctx.Err();err!=nil{return err}
	if len(e.Payload) > 65536 || !json.Valid(e.Payload) || e.EventID == uuid.Nil || e.Destination != Commands && e.Destination != Events {
		return errors.New("invalid queue envelope")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
    if err:=ctx.Err();err!=nil{return err}
	confirm, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, e.Destination, e.RoutingKey, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: e.EventID.String(), Timestamp: time.Now().UTC(), Body: e.Payload})
	if err != nil {
		return errors.New("broker publish failed")
	}
	if confirm == nil {
		return errors.New("publisher confirms disabled")
	}
	ack, err := confirm.WaitContext(ctx)
	if err != nil || !ack {
		_ = p.channel.Close()
		return errors.New("broker confirmation unavailable")
	}
	// Rabbit sends mandatory basic.return before basic.ack; the client dispatches
	// returns before delivering the confirmation. A returned message is not sent.
	select {
	case returned, ok := <-p.returns:
		if !ok {
			return errors.New("broker channel closed")
		}
		if returned.MessageId != e.EventID.String() {
			_ = p.channel.Close()
			return errors.New("broker return correlation mismatch")
		}
		return errors.New("broker message unroutable")
	default:
	}
	return nil
}

type Outbox struct {
	DB        *pgxpool.Pool
	Publisher ConfirmedPublisher
}

func (o *Outbox) Dispatch(ctx context.Context, kind string, limit int) (int, error) {
	rows, err := o.DB.Query(ctx, "SELECT id,event_id,destination,routing_key,payload,lease FROM photo.claim_outbox_batch($1,$2,60)", kind, limit)
	if err != nil {
		return 0, err
	}
	batch := []Envelope{}
	for rows.Next() {
		e := Envelope{Kind: kind}
		if err = rows.Scan(&e.ID, &e.EventID, &e.Destination, &e.RoutingKey, &e.Payload, &e.Lease); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	sent := 0
	for _, e := range batch {
		// Keep each publish within the DB lease. On any unknown outcome rows stay
		// unsent and are safely republished with the same event ID after expiration.
        if err=publishWithinLease(ctx,o.Publisher,e);err!=nil{return sent,err}
		var marked bool
		if err = o.DB.QueryRow(ctx, "SELECT photo.mark_outbox_sent($1,$2,$3)", kind, e.ID, e.Lease).Scan(&marked); err != nil {
			return sent, err
		}
		if !marked {
			return sent, errors.New("outbox publish lease lost")
		}
		sent++
	}
	return sent, nil
}

// A late nil result is an unknown delivery outcome, not permission to mark the
// row sent. Leave it recoverable under its stable event ID.
func publishWithinLease(ctx context.Context,publisher ConfirmedPublisher,e Envelope)error{
 if err:=ctx.Err();err!=nil{return err}
 deadline:=e.Lease.Add(-time.Second)
 if local:=time.Now().Add(10*time.Second);local.Before(deadline){deadline=local}
 publishCtx,cancel:=context.WithDeadline(ctx,deadline);defer cancel()
 if err:=publishCtx.Err();err!=nil{return err}
 if err:=publisher.Publish(publishCtx,e);err!=nil{return err}
 if err:=publishCtx.Err();err!=nil{return err}
 return ctx.Err()
}
