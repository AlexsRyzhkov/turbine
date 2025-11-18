package turbine

import (
	"strings"

	"github.com/rabbitmq/amqp091-go"
)

func isNotFound(err error) bool {
	if err == nil {
		return false
	}

	if amqpErr, ok := err.(*amqp091.Error); ok {
		return amqpErr.Code == 404
	}

	return strings.Contains(err.Error(), "NOT_FOUND")
}

func isTypeMismatch(err error) bool {
	if err == nil {
		return false
	}

	if amqpErr, ok := err.(*amqp091.Error); ok {
		return amqpErr.Code == 406
	}

	return strings.Contains(err.Error(), "inequivalent arg 'type'")
}
