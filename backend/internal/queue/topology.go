package queue

import amqp "github.com/rabbitmq/amqp091-go"

const (
	Commands = "photo.commands.v1"
	Events   = "photo.events.v1"
	Dead     = "photo.dead.v1"
)

type Binding struct{ Queue, Exchange, Key, Class string }

var Bindings = []Binding{
	{"q.worker.cpu", Commands, "cpu.*", "cpu"}, {"q.worker.gpu", Commands, "gpu.*", "gpu"},
	{"q.worker.mail", Commands, "mail.*", "mail"}, {"q.worker.media", Commands, "io.thumbnail", "media"},
	{"q.worker.media", Commands, "io.preview", "media"}, {"q.worker.media", Commands, "io.verify", "media"},
	{"q.worker.archive", Commands, "io.archive", "archive"},
	{"q.worker.collection", Commands, "collection.mutate", "collection"}, {"q.worker.collection", Commands, "media.retry", "collection"}, {"q.orchestrator.events", Events, "task.*", "events"},
	{"q.orchestrator.events", Events, "download_status", "events"},
	{"q.orchestrator.events", Events, "review.created", "events"},
	{"q.orchestrator.events", Events, "delivery.cancelled", "events"},
}

// Declare is a privileged bootstrap operation; runtime publishers do not require
// configure permissions. Deployment sets at-least-once DLX policy separately.
func Declare(ch *amqp.Channel) error {
	for _, name := range []string{Commands, Events, Dead} {
		if err := ch.ExchangeDeclare(name, "topic", true, false, false, false, nil); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, b := range Bindings {
		if !seen[b.Queue] {
			dead := "q.dead." + b.Class
			if _, err := ch.QueueDeclare(dead, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
				return err
			}
			if err := ch.QueueBind(dead, "dead."+b.Class, Dead, false, nil); err != nil {
				return err
			}
			args := amqp.Table{"x-queue-type": "quorum", "x-dead-letter-exchange": Dead, "x-dead-letter-routing-key": "dead." + b.Class, "x-delivery-limit": int32(20), "x-consumer-timeout": int64(14400000)}
			if _, err := ch.QueueDeclare(b.Queue, true, false, false, false, args); err != nil {
				return err
			}
			seen[b.Queue] = true
		}
		if err := ch.QueueBind(b.Queue, b.Key, b.Exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}
