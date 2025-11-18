package turbine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rabbitmq/amqp091-go"
)

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

func TestRabbitMQ_Connect(t *testing.T) {
	url, cleanup, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Connect_Success", func(t *testing.T) {
		rmq := NewRabbitMQ(url, NewMockLogger())
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err, "could not connect to rabbitmq")
		}
	})

	t.Run("Connect_CreatesAMQPConnection", func(t *testing.T) {
		rmq := NewRabbitMQ(url, NewMockLogger())
		_ = rmq.Connect()
		if rmq.conn == nil || rmq.conn.IsClosed() {
			t.Fatal(err, "could not create amqp091.Connection")
		}
	})

	t.Run("Connect_CreatesPublishChannel", func(t *testing.T) {
		rmq := NewRabbitMQ(url, NewMockLogger())
		_ = rmq.Connect()
		if rmq.publishCh == nil || rmq.publishCh.IsClosed() {
			t.Fatal(err, "could not create publishChannel")
		}
	})

	t.Run("Connect_CreatesAdminChannel", func(t *testing.T) {
		rmq := NewRabbitMQ(url, NewMockLogger())
		_ = rmq.Connect()
		if rmq.adminCh == nil || rmq.adminCh.IsClosed() {
			t.Fatal(err, "could not create adminChannel")
		}
	})

	unExistingUrl := "amqp://guest1:guest@localhost:5672/"
	t.Run("ErrorConnect", func(t *testing.T) {
		rmq := NewRabbitMQ(unExistingUrl, NewMockLogger())
		err = rmq.Connect()
		if err == nil {
			t.Fatal("connect to rabbitmq succeeded is err")
		}

		if !strings.Contains(err.Error(), "Failed connect") {
			t.Fatal("connection error is expected to contain 'Failed connect'")
		}

		if rmq.conn != nil {
			t.Fatal("create rabbitmq connection")
		}

		if rmq.adminCh != nil {
			t.Fatal("create admin channel")
		}

		if rmq.publishCh != nil {
			t.Fatal("create publish channel")
		}
	})
}

func TestRabbitMQ_Reconnect(t *testing.T) {
	url, cleanup, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Reconnect_RecreateConnection", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()

		oldConnection := rmq.conn
		oldPublishCh := rmq.publishCh
		oldAdminCh := rmq.adminCh
		rmq.conn.Close()

		// Чтобы точно переподключился добавляем 1 секунду
		<-time.After(rmq.waitTime + 1*time.Second)

		if !strings.Contains(logger.listMsg[1], "connection lost - trying to reconnect...") ||
			!strings.Contains(logger.listMsg[2], "Successfully connected") {
			t.Fatal("invalid logging")
		}

		rmq.conn.Close()
		<-time.After(rmq.waitTime + 1*time.Second)

		if oldConnection == rmq.conn || rmq.conn.IsClosed() {
			t.Fatal("could not reconnect to rabbitmq")
		}

		if oldPublishCh == rmq.publishCh || rmq.publishCh.IsClosed() {
			t.Fatal("could not recreate publish channel")
		}

		if oldAdminCh == rmq.adminCh || rmq.adminCh.IsClosed() {
			t.Fatal("could not recreate admin channel")
		}
	})

	t.Run("Reconnect_RecreatePublishCh", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()

		oldConnection := rmq.conn
		oldPublishCh := rmq.publishCh
		oldAdminCh := rmq.adminCh

		rmq.publishCh.Close()
		<-time.After(rmq.waitTime + 1*time.Second)

		if !strings.Contains(logger.listMsg[1], "connection lost - trying to reconnect publish...") ||
			!strings.Contains(logger.listMsg[2], "reconnect publish channel success") {
			t.Fatal("invalid logging")
		}

		if oldConnection != rmq.conn || rmq.conn.IsClosed() {
			t.Fatal("reconnect to rabbitmq or close connection")
		}

		if oldPublishCh == rmq.publishCh || rmq.publishCh.IsClosed() {
			t.Fatal("could not recreate or close publish channel")
		}

		if oldAdminCh != rmq.adminCh || rmq.adminCh.IsClosed() {
			t.Fatal("recreate or close admin channel")
		}
	})

	t.Run("Reconnect_RecreateAdminCh", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()

		oldConnection := rmq.conn
		oldPublishCh := rmq.publishCh
		oldAdminCh := rmq.adminCh

		rmq.adminCh.Close()
		<-time.After(rmq.waitTime + 1*time.Second)

		if !strings.Contains(logger.listMsg[1], "connection lost - trying to reconnect admin...") ||
			!strings.Contains(logger.listMsg[2], "reconnect admin channel success") {
			t.Fatal("invalid logging")
		}

		if oldConnection != rmq.conn || rmq.conn.IsClosed() {
			t.Fatal("reconnect to rabbitmq or close connection")
		}

		if oldPublishCh != rmq.publishCh || rmq.publishCh.IsClosed() {
			t.Fatal("recreate or close publish channel")
		}

		if oldAdminCh == rmq.adminCh || rmq.adminCh.IsClosed() {
			t.Fatal("could not recreate or close admin channel")
		}
	})
}

func TestRabbitMQ_Subscribe(t *testing.T) {
	url, cleanup, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Subscribe_UnExistingQueue", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		_, err := rmq.Subscribe("test", 10)
		if err == nil || !strings.Contains(err.Error(), "queue does not exist") {
			t.Fatal("queue is exist")
		}
	})

	t.Run("Subscribe_ExistingQueue", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err = createQueue(rmq.adminCh, "test")
		if err != nil {
			t.Fatal(err)
		}

		delivery, err := rmq.Subscribe("test", 10)
		if err != nil {
			t.Fatal("queue isn't exist")
		}

		err = publishMsg(rmq.adminCh, "Test1", "", "test")
		if err != nil {
			t.Fatal(err)
		}

		msg := <-delivery
		fmt.Println("Message: ", string(msg.Body))
		if !strings.Contains(string(msg.Body), "Test1") {
			t.Fatal("invalid message get")
		}
	})

	t.Run("Subscribe_GetMessageAfterReconnect", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err = createQueue(rmq.adminCh, "test")
		if err != nil {
			t.Fatal(err)
		}

		delivery, err := rmq.Subscribe("test", 10)
		if err != nil {
			t.Fatal("queue isn't exist")
		}

		err = publishMsg(rmq.adminCh, "Test1", "", "test")
		if err != nil {
			t.Fatal(err)
		}

		rmq.conn.Close()

		msg := <-delivery
		fmt.Println("Message: ", string(msg.Body))
		if !strings.Contains(string(msg.Body), "Test1") {
			t.Fatal("invalid message get")
		}
	})
}

func TestRabbitMQ_Publish(t *testing.T) {
	url, cleanup, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Subscribe", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err = createQueue(rmq.adminCh, "test")
		if err != nil {
			t.Fatal(err)
		}

		err = rmq.SafePublish("", "test", "Test message")
		if err != nil {
			t.Fatal(err, "message could be published")
		}

		delivery, _ := rmq.Subscribe("test", 10)

		msg := <-delivery
		fmt.Println("Message: ", string(msg.Body))
		if string(msg.Body) != "Test message" {
			t.Fatal("invalid message published")
		}
	})

	t.Run("Subscribe_UnExistingExchange", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err = createQueue(rmq.adminCh, "test")
		if err != nil {
			t.Fatal(err)
		}

		err := rmq.SafePublish("Exchange", "test", "Test message")
		if err == nil || !strings.Contains(err.Error(), "Exchange is not exist") {
		}
	})

	t.Run("Subscribe_UnExistingQueue", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err := rmq.SafePublish("", "test", "Test message")
		if err != nil {
			t.Fatal("message should be published")
		}
	})

	t.Run("Subscribe_ReconnectPublishing", func(t *testing.T) {
		logger := NewMockLogger()
		rmq := NewRabbitMQ(url, logger)
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		err = createQueue(rmq.adminCh, "test")
		if err != nil {
			t.Fatal(err)
		}

		rmq.publishCh.Close()

		err = rmq.SafePublish("", "test", "Test message")
		if err != nil {
			t.Fatal(err, "message could be published")
		}

		delivery, _ := rmq.Subscribe("test", 10)

		msg := <-delivery
		fmt.Println("Message: ", string(msg.Body))
		if string(msg.Body) != "Test message" {
			t.Fatal("invalid message published")
		}
	})
}
