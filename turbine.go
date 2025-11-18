package turbine

import (
	"context"
	"fmt"
	"sync"
)

type RabbitMQPool struct {
	setting *RabbitSetting

	clients map[string]*RabbitMQ

	pathToClient map[string]*RabbitMQ

	consumers  map[string]RabbitConsumer
	publishers map[string]RabbitPublish

	wgWorker        sync.WaitGroup
	cancelWorkerFn  context.CancelFunc
	ctxCancelWorker context.Context

	middlewares []MiddlewareFunc

	logger ILogger
}

func NewRabbitMQPool(setting RabbitSetting, logger ILogger) *RabbitMQPool {
	consumers := map[string]RabbitConsumer{}
	publishers := map[string]RabbitPublish{}

	for _, connect := range setting.Connects {
		for _, consumer := range connect.Consumers {
			consumers[fmt.Sprintf("%s.%s", connect.Alias, consumer.Alias)] = consumer
		}

		for _, publish := range connect.Publishers {
			publishers[fmt.Sprintf("%s.%s", connect.Alias, publish.Alias)] = publish
		}
	}

	return &RabbitMQPool{
		setting: &setting,
		clients: make(map[string]*RabbitMQ),
		logger:  logger,

		consumers:    consumers,
		publishers:   publishers,
		pathToClient: make(map[string]*RabbitMQ),
	}
}

func (r *RabbitMQPool) Connect() {
	for _, connect := range r.setting.Connects {
		r.clients[connect.Alias] = NewRabbitMQ(fmt.Sprintf(
			"amqp://%s:%s@%s:5672/%s",
			connect.Login,
			connect.Pass,
			connect.Host,
			connect.Vhost,
		), r.logger)

		err := r.clients[connect.Alias].Connect()
		if err != nil {
			r.Disconnect()
		}

		for _, consumer := range connect.Consumers {
			r.pathToClient[fmt.Sprintf("consumer.%s.%s", connect.Alias, consumer.Alias)] = r.clients[connect.Alias]
		}

		for _, publish := range connect.Publishers {
			r.pathToClient[fmt.Sprintf("publisher.%s.%s", connect.Alias, publish.Alias)] = r.clients[connect.Alias]
		}

		ctx, cancel := context.WithCancel(context.Background())

		r.ctxCancelWorker = ctx
		r.cancelWorkerFn = cancel
	}
}

func (r *RabbitMQPool) Disconnect() {
	r.cancelWorkerFn()
	r.wgWorker.Wait()

	wg := sync.WaitGroup{}

	for _, client := range r.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Disconnect()
		}()
	}

	wg.Wait()

	r.clients = make(map[string]*RabbitMQ)
}

type Handler func(ctx *Context) error

func (r *RabbitMQPool) Subscribe(path string, handler Handler, numWorkers int) error {
	client, ok := r.pathToClient["consumer."+path]
	if !ok {
		return fmt.Errorf("[RabbitMQPool:%s] Client not found!", path)
	}

	consumer := r.consumers[path]

	messages, err := client.Subscribe(consumer.Queue, consumer.PrefetchCount)

	if err != nil {
		return fmt.Errorf("[RabbitMQPool:%s] Error subscribing to messages!", path)
	}

	for i := 0; i < numWorkers; i++ {
		r.wgWorker.Add(1)

		go func() {
			defer r.wgWorker.Done()

			for message := range messages {
				select {
				case <-r.ctxCancelWorker.Done():
					return
				default:
					ctx := &Context{
						context.Background(),
						string(message.Body),
					}

					handlerWithMiddleware := handler

					for _, middleware := range r.middlewares {
						handlerWithMiddleware = middleware(handlerWithMiddleware)
					}

					err := handlerWithMiddleware(ctx)
					if err != nil {
						message.Nack(false, true)
					} else {
						message.Ack(false)
					}
				}
			}
		}()
	}

	return nil
}

func (r *RabbitMQPool) Publish(path string, data string) error {
	client, ok := r.pathToClient["publisher."+path]
	if !ok {
		return fmt.Errorf("[RabbitMQPool:%s] Client not found!", path)
	}

	publisher := r.publishers[path]

	return client.SafePublish(publisher.Exchange, publisher.RoutingKey, data)
}

type MiddlewareFunc func(next Handler) Handler

func (r *RabbitMQPool) Use(middleware MiddlewareFunc) {
	r.middlewares = append(r.middlewares, middleware)
}
