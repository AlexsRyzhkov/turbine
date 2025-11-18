package turbine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type OutgoingMessage struct {
	Exchange   string
	RoutingKey string
	ReplyTo    string
	Body       []byte
}

type Consumer struct {
	PrefetchCount int
	Queue         string
}

type RabbitMQ struct {
	url    string
	logger ILogger

	conn      *amqp091.Connection
	publishCh *amqp091.Channel
	adminCh   *amqp091.Channel

	mu sync.Mutex

	cancelWorkerFn context.CancelFunc
	wgWorker       sync.WaitGroup
	ctxWorker      context.Context

	confirms  chan amqp091.Confirmation
	pending   chan OutgoingMessage
	wgPending sync.WaitGroup

	consumers map[string]Consumer

	waitTime time.Duration

	closeConn      chan *amqp091.Error
	closePublishCh chan *amqp091.Error
	closeAdminCh   chan *amqp091.Error
}

func NewRabbitMQ(url string, logger ILogger) *RabbitMQ {
	return &RabbitMQ{
		url:       url,
		logger:    logger,
		pending:   make(chan OutgoingMessage),
		consumers: make(map[string]Consumer, 10),
		waitTime:  2 * time.Second,
	}
}

func (r *RabbitMQ) Connect() error {
	err := r.initConnection()
	if err != nil {
		return err
	}

	r.runWorker()

	return nil
}

func (r *RabbitMQ) initConnection() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, err := amqp091.Dial(r.url)
	if err != nil {
		err = fmt.Errorf("[RabbitMQ:%s] [err:%s] Failed connect", r.url, err)
		r.logger.Errorf(err.Error())
		return err
	}

	publishCh, err := conn.Channel()
	if err != nil {
		conn.Close()
		err = fmt.Errorf("[RabbitMQ:%s] [err:%s] Failed connect and create publishCh", r.url, err)
		r.logger.Errorf(err.Error())
		return err
	}

	adminCh, err := conn.Channel()
	if err != nil {
		conn.Close()
		err = fmt.Errorf("[RabbitMQ:%s] [err:%s] Failed connect  and create adminCh", r.url, err)
		r.logger.Errorf(err.Error())
		return err
	}

	r.closeConn = conn.NotifyClose(make(chan *amqp091.Error))
	r.closePublishCh = publishCh.NotifyClose(make(chan *amqp091.Error))
	r.closeAdminCh = adminCh.NotifyClose(make(chan *amqp091.Error))

	if err := publishCh.Confirm(false); err != nil {
		conn.Close()
		err = fmt.Errorf("[RabbitMQ:%s] [err:%s] Failed connect", r.url, err)
		r.logger.Errorf(err.Error())
		return err
	}

	r.conn = conn
	r.publishCh = publishCh
	r.adminCh = adminCh
	r.confirms = publishCh.NotifyPublish(make(chan amqp091.Confirmation, 1))

	ctx, cancel := context.WithCancel(context.Background())
	r.cancelWorkerFn = cancel
	r.ctxWorker = ctx

	r.logger.Infof("[RabbitMQ:%s] Successfully connected", r.url)

	return nil
}

func (r *RabbitMQ) initPublishChannel() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	publishCh, err := r.conn.Channel()
	if err != nil {
		return err
	}

	r.publishCh = publishCh
	r.closePublishCh = publishCh.NotifyClose(make(chan *amqp091.Error))

	return nil
}

func (r *RabbitMQ) initAdminChannel() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	adminCh, err := r.conn.Channel()
	if err != nil {
		return err
	}

	r.adminCh = adminCh
	r.closeAdminCh = adminCh.NotifyClose(make(chan *amqp091.Error))

	return nil
}

func (r *RabbitMQ) runWorker() {
	r.publishWorker()
	r.reconnectWorker()
}

func (r *RabbitMQ) Disconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil {
		return nil
	}

	r.cancelWorkerFn()
	r.wgPending.Wait()
	close(r.pending)
	close(r.confirms)
	r.wgWorker.Wait()

	if r.publishCh != nil {
		r.publishCh.Close()
	}

	if r.adminCh != nil {
		r.adminCh.Close()
	}

	if r.conn != nil {
		r.conn.Close()
	}

	r.conn = nil
	r.logger.Infof("[RabbitMQ:%s], disconnect", r.url)

	return nil
}

func (r *RabbitMQ) SafePublish(exchange, routingKey, replyTo, body string) error {
	if r.conn == nil {
		return fmt.Errorf("[RabbitMQ:%s] is disconnected", r.url)
	}

	if !r.exchangeIsExist(exchange) {
		return fmt.Errorf("[RabbitMQ:%s] [exchange:%s] Exchange is not exist", r.url, exchange)
	}

	select {
	case <-r.ctxWorker.Done():
		return fmt.Errorf("[RabbitMQ:%s] is disconnecting", r.url)
	default:
		r.wgPending.Add(1)
		r.pending <- OutgoingMessage{
			Exchange:   exchange,
			RoutingKey: routingKey,
			ReplyTo:    replyTo,
			Body:       []byte(body),
		}
	}

	return nil
}

func (r *RabbitMQ) unsafePublish(exchange, routingKey, replyTo, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil || r.conn.IsClosed() {
		return fmt.Errorf("[RabbitMQ:%s] connection is closed", r.url)
	}

	if r.publishCh == nil || r.publishCh.IsClosed() {
		return fmt.Errorf("[RabbitMQ:%s] publish channel is closed", r.url)
	}

	if !r.exchangeIsExist(exchange) {
		return fmt.Errorf("[RabbitMQ:%s] [exchange:%s] Exchange is not exist", r.url, exchange)
	}

	return r.publishCh.Publish(
		exchange,
		routingKey,
		false,
		false,
		amqp091.Publishing{
			Headers:         amqp091.Table{},    // type Table map[string]interface{}
			ContentType:     "text/plain",       // MIME content type
			ContentEncoding: "",                 // MIME content encoding
			DeliveryMode:    amqp091.Persistent, // Transient (0 or 1) or Persistent (2)
			Priority:        0,                  // 0 to 9
			CorrelationId:   "",                 // correlation identifier
			ReplyTo:         replyTo,            // address to reply to (ex: RPC)
			Expiration:      "",                 // message expiration spec
			MessageId:       "",                 // message identifier
			Timestamp:       time.Now(),         // message timestamp
			Type:            "application/json", // message type name
			UserId:          "",                 // creating user id - ex: "guest"
			AppId:           "",                 // creating application id
			Body:            []byte(body),       // The application specific payload of the message
		},
	)
}

func (r *RabbitMQ) exchangeIsExist(exchangeName string) bool {
	types := []string{"direct", "fanout", "topic", "headers", "default"}

	if exchangeName == "" {
		return true
	}

	for _, t := range types {
		err := r.adminCh.ExchangeDeclarePassive(exchangeName, t, true, false, false, false, nil)
		if err == nil {
			return true
		}

		if isNotFound(err) {
			continue
		}

		if isTypeMismatch(err) {
			continue
		}
	}

	return false
}

func (r *RabbitMQ) publishWorker() {
	go func() {
		r.wgWorker.Add(1)
		defer r.wgWorker.Done()

		for msg := range r.pending {
			for {
				err := r.unsafePublish(msg.Exchange, msg.RoutingKey, msg.ReplyTo, string(msg.Body))
				if err != nil {
					r.logger.Warnf("[RabbitMQ:%s] publish fail: %s (retry...)", msg.Exchange, err.Error())
					time.Sleep(r.waitTime)
					continue
				}

				confirm := <-r.confirms
				if confirm.Ack {
					r.wgPending.Done()
					break
				}

				r.logger.Warnf("[RabbitMQ:%s] [exchange:%s] publish to nack -> retry", r.url, msg.Exchange)
				time.Sleep(r.waitTime)
			}
		}
	}()
}

func (r *RabbitMQ) Subscribe(qName string, prefetchCount int) (<-chan amqp091.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil || r.conn.IsClosed() {
		return nil, fmt.Errorf("[RabbitMQ:%s] connection is closed", r.url)
	}

	_, err := r.adminCh.QueueDeclarePassive(
		qName,
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		if amqpErr, ok := err.(*amqp091.Error); ok && amqpErr.Code == 404 {
			err = fmt.Errorf("[RabbitMQ:%s] [qName:%s] queue does not exist", r.url, qName)
			r.logger.Errorf(err.Error())
			return nil, err
		} else {
			err = fmt.Errorf("[RabbitMQ:%s] [err:%v] unexpected error", r.url, err)
			r.logger.Errorf(err.Error())
			return nil, err
		}
	}

	proxy := make(chan amqp091.Delivery, prefetchCount)

	r.consumers[qName] = Consumer{
		PrefetchCount: prefetchCount,
		Queue:         qName,
	}

	r.consumerWorker(qName, proxy)

	return proxy, nil
}

func (r *RabbitMQ) consumerWorker(qName string, proxy chan amqp091.Delivery) {
	r.wgWorker.Add(1)

	go func() {
		defer r.wgWorker.Done()

		for {
			source, err := r.createConsumerChannel(qName)
			if err != nil {
				r.logger.Errorf("[RabbitMQ:%s] [consumer:%s] create error: %v", r.url, qName, err)
				time.Sleep(r.waitTime)
				continue
			}

			for msg := range source {
				select {
				case <-r.ctxWorker.Done():
					return
				default:
					proxy <- msg
				}

			}

			r.logger.Warnf("[RabbitMQ:%s] [consumer:%s] channel closed — reconnect…", qName)
		}
	}()
}

func (r *RabbitMQ) createConsumerChannel(queue string) (<-chan amqp091.Delivery, error) {
	ch, err := r.conn.Channel()
	if err != nil {
		return nil, err
	}

	c := r.consumers[queue]

	if err := ch.Qos(c.PrefetchCount, 0, false); err != nil {
		ch.Close()
		return nil, err
	}

	deliveries, err := ch.Consume(
		queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		return nil, err
	}

	return deliveries, nil
}

func (r *RabbitMQ) reconnectWorker() {
	r.mu.Lock()
	if r.conn == nil {
		r.mu.Unlock()
		return
	}
	oldUrl := r.url
	r.mu.Unlock()

	go func() {
		for {
			time.Sleep(r.waitTime)

			var err error

			select {
			case <-r.ctxWorker.Done():
				return
			default:
			}

			select {
			case <-r.closeConn:
				r.logger.Warnf("[RabbitMQ:%s] connection lost - trying to reconnect...", oldUrl)

				err = r.initConnection()
				if err != nil {
					r.logger.Errorf("[RabbitMQ:%s] [err:%v] reconnect fail", r.url, err)
				}

				continue
			default:
			}

			select {
			case <-r.closePublishCh:
				r.logger.Warnf("[RabbitMQ:%s] connection lost - trying to reconnect publish...", oldUrl)

				err = r.initPublishChannel()
				if err != nil {
					r.logger.Errorf("[RabbitMQ:%s] [err:%v] reconnect publish channel fail", r.url, err)
				} else {
					r.logger.Infof("[RabbitMQ:%s] reconnect publish channel success", oldUrl)
				}
			case <-r.closeAdminCh:
				r.logger.Warnf("[RabbitMQ:%s] connection lost - trying to reconnect admin...", oldUrl)

				err = r.initAdminChannel()
				if err != nil {
					r.logger.Errorf("[RabbitMQ:%s] [err:%v] reconnect admin channel fail", r.url, err)
				} else {
					r.logger.Infof("[RabbitMQ:%s] reconnect admin channel success", oldUrl)
				}
			default:

			}
		}
	}()
}

func (r *RabbitMQ) AdminChannel() *amqp091.Channel {
	return r.adminCh
}
