package turbine

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rabbitmq/amqp091-go"
)

type MockLogger struct {
	l       *log.Logger
	listMsg []string
}

func NewMockLogger() *MockLogger {
	return &MockLogger{
		l:       log.New(os.Stdout, "", 0),
		listMsg: []string{},
	}
}

func (l *MockLogger) Debugf(format string, v ...any) {
	l.output("DEBUG", format, v...)
}

func (l *MockLogger) Infof(format string, v ...any) {
	l.output("INFO", format, v...)
}

func (l *MockLogger) Warnf(format string, v ...any) {
	l.output("WARN", format, v...)
}

func (l *MockLogger) Errorf(format string, v ...any) {
	l.output("ERROR", format, v...)
}

func (l *MockLogger) output(level, format string, v ...any) {
	timestamp := time.Now().Format(time.RFC3339)
	msg := fmt.Sprintf(format, v...)
	l.l.Printf("[%s] %s: %s", timestamp, level, msg)
	l.listMsg = append(l.listMsg, msg)
}

func setup() (string, func(), error) {
	fmt.Println("setup rabbitmq docker container")
	pool, err := dockertest.NewPool("")
	if err != nil {
		return "", nil, fmt.Errorf("could not connect to docker: %w", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "rabbitmq",
		Tag:        "3-management",
		PortBindings: map[docker.Port][]docker.PortBinding{
			"5672/tcp": {{HostIP: "0.0.0.0", HostPort: "5673"}},
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("could not start rabbitmq container: %w", err)
	}

	resource.Expire(120)

	url := fmt.Sprintf(
		"amqp://guest:guest@localhost:%s/",
		resource.GetPort("5672/tcp"),
	)

	if err := pool.Retry(func() error {
		conn, err := amqp091.Dial(url)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}); err != nil {
		pool.Purge(resource)
		return "", nil, fmt.Errorf("could not connect to rabbitmq: %w", err)
	}

	cleanup := func() {
		_ = pool.Purge(resource)
	}

	return url, cleanup, nil
}

func setupPull() (*RabbitSetting, func(), error) {
	fmt.Println("setup rabbitmq docker container")
	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, nil, fmt.Errorf("could not connect to docker: %w", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "rabbitmq",
		Tag:        "3-management",
		PortBindings: map[docker.Port][]docker.PortBinding{
			"5672/tcp": {{HostIP: "0.0.0.0", HostPort: "5672"}},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("could not start rabbitmq container: %w", err)
	}

	resource.Expire(120)

	url := fmt.Sprintf(
		"amqp://guest:guest@localhost:%s/",
		resource.GetPort("5672/tcp"),
	)

	if err := pool.Retry(func() error {
		conn, err := amqp091.Dial(url)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}); err != nil {
		pool.Purge(resource)
		return nil, nil, fmt.Errorf("could not connect to rabbitmq: %w", err)
	}

	cleanup := func() {
		_ = pool.Purge(resource)
	}

	conn, _ := amqp091.Dial(url)

	ch, _ := conn.Channel()

	createQueue(ch, "test1")
	createQueue(ch, "test2")
	createQueue(ch, "test3")

	return &RabbitSetting{
		Connects: []RabbitConnect{
			{
				Alias: "rabbitmq1",
				Host:  "localhost",
				Login: "guest",
				Pass:  "guest",
				Port:  resource.GetPort("5672/tcp"),
				Vhost: "/",

				Consumers: []RabbitConsumer{
					{
						Alias:         "t1",
						PrefetchCount: 10,
						Queue:         "test1",
					},
				},

				Publishers: []RabbitPublish{
					{
						Alias:      "t1",
						Exchange:   "",
						RoutingKey: "test1",
						ReplyTo:    "",
					},
					{
						Alias:      "t2",
						Exchange:   "",
						RoutingKey: "test2",
						ReplyTo:    "",
					},
				},
			},

			{
				Alias: "rabbitmq2",
				Host:  "localhost",
				Login: "guest",
				Pass:  "guest",
				Vhost: "/",

				Consumers: []RabbitConsumer{
					{
						Alias:         "t1",
						PrefetchCount: 10,
						Queue:         "test1",
					},
					{
						Alias:         "t2",
						PrefetchCount: 10,
						Queue:         "test2",
					},
					{
						Alias:         "t3",
						PrefetchCount: 10,
						Queue:         "test3",
					},
				},

				Publishers: []RabbitPublish{
					{
						Alias:      "t3",
						Exchange:   "",
						RoutingKey: "test3",
						ReplyTo:    "",
					},
				},
			},
		},
	}, cleanup, nil
}

func createExchange(ch *amqp091.Channel, name, _type string) error {
	return ch.ExchangeDeclare(
		name,
		_type,
		true,
		false,
		false,
		false,
		nil,
	)
}

func createQueue(ch *amqp091.Channel, name string) error {
	_, err := ch.QueueDeclare(
		name,
		true,
		false,
		false,
		false,
		nil,
	)

	return err
}

func bindQueueToExchange(ch *amqp091.Channel, qName, routingKey, exchangeName string) error {
	return ch.QueueBind(
		qName,
		routingKey,
		exchangeName,
		false,
		nil,
	)
}

func publishMsg(ch *amqp091.Channel, body string, exchange, routingKey string) error {
	return ch.Publish(
		exchange,
		routingKey,
		false,
		false,
		amqp091.Publishing{
			ContentType: "text/plain",
			Body:        []byte(body),
		},
	)
}
