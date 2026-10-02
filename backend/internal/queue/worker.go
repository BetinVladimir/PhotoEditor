package queue

import (
	"errors"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func WorkerQueue(worker uuid.UUID, class string) (string, error) {
	if worker == uuid.Nil {
		return "", errors.New("invalid worker identity")
	}
	switch class {
	case "cpu", "gpu", "media", "archive", "collection":
	default:
		return "", errors.New("invalid worker class")
	}
	return "q.worker." + worker.String() + "." + class, nil
}

// DeclareWorker is privileged provisioning; workers only consume their own queue.
func DeclareWorker(ch *amqp.Channel, worker uuid.UUID, class string) error {
	name, err := WorkerQueue(worker, class)
	if err != nil {
		return err
	}
	args := amqp.Table{"x-queue-type": "quorum", "x-dead-letter-exchange": Dead, "x-dead-letter-routing-key": "dead." + class, "x-delivery-limit": int32(20), "x-consumer-timeout": int64(14400000)}
	if _, err = ch.QueueDeclare(name, true, false, false, false, args); err != nil {
		return err
	}
	for _, binding := range Bindings {
		if binding.Class == class && binding.Exchange == Commands {
			if err = ch.QueueBind(name, "worker."+worker.String()+"."+binding.Key, Commands, false, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func BootstrapWorker(url string, worker uuid.UUID, class string) error {
	if _, err := WorkerQueue(worker, class); err != nil {
		return err
	}
	connection, err := dial(url)
	if err != nil {
		return errors.New("broker connection unavailable")
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err = Declare(channel); err != nil {
		return err
	}
	return DeclareWorker(channel, worker, class)
}
