package turbine

import (
	"fmt"
	"testing"
	"time"
)

func TestRabbitMQPool_Connect(t *testing.T) {
	setting, cleanup, err := setupPull()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Connect_Success", func(t *testing.T) {
		rmq := NewRabbitMQPool(*setting, NewMockLogger())
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err, "could not connect to rabbitmqpool")
		}

		for _, client := range rmq.clients {
			if client.conn == nil && client.conn.IsClosed() {
				t.Fatal("client conn not connected")
			}
		}
	})

	t.Run("Connect_Error", func(t *testing.T) {
		setting.Connects[0].Port = "4352"
		rmq := NewRabbitMQPool(*setting, NewMockLogger())
		err = rmq.Connect()
		if err == nil {
			t.Fatal(err, "connect to rabbitmqpool")
		}

		if len(rmq.clients) != 0 {
			t.Fatal("client conn not disconnected")
		}
	})
}

func TestRabbitMQPool_Disconnect(t *testing.T) {
	setting, cleanup, err := setupPull()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Disconnect_Success", func(t *testing.T) {
		rmq := NewRabbitMQPool(*setting, NewMockLogger())
		err = rmq.Connect()
		if err != nil {
			t.Fatal(err)
		}

		rmq.Disconnect()

		if len(rmq.clients) != 0 {
			t.Fatal("client conn not disconnected")
		}
	})
}

func TestRabbitMQPool_SubscribeAndPublish(t *testing.T) {
	setting, cleanup, err := setupPull()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	t.Run("Subscribe_Success", func(t *testing.T) {
		rmq := NewRabbitMQPool(*setting, NewMockLogger())
		err = rmq.Connect()

		err := rmq.Publish("rabbitmq1.t1", "Test Message", "")
		if err != nil {
			t.Fatal("could not publish message", err)
		}

		msg := ""

		err = rmq.Subscribe("rabbitmq1.t1", func(ctx *Context) error {
			fmt.Println(ctx.Message())

			msg = ctx.Message()

			return nil
		}, 10)

		if err != nil {
			t.Fatal("Error subscribing to rabbitmq1.t1", err)
		}

		time.Sleep(5 * time.Second)

		if msg != "Test Message" {
			t.Fatal("error getting message to rabbitmq1.t1 need = ", "Test Message", " get = ", msg)
		}
	})
}
