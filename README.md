# 🐇 Работа с RabbitMQPool

`RabbitMQPool` — это высокоуровневый менеджер, позволяющий работать сразу с несколькими подключениями RabbitMQ, используя удобную маршрутизацию по alias-ам.

Пул автоматически:

* создаёт клиентов RabbitMQ по конфигурации
* управляет Publisher-ами и Consumer-ами
* восстанавливает соединения
* разворачивает worker-ы для обработки сообщений
* передаёт в обработчики удобный объект `Context`
* поддерживает middleware

Это удобный слой поверх базового клиента `RabbitMQ`.

---

# ⚙️ Конфигурация пула

`RabbitMQPool` создаётся на основе структуры настроек:

```go
type RabbitSetting struct {
    Connects []RabbitConnect
}

type RabbitConnect struct {
    Alias     string
    Host      string
    Port      string
    Login     string
    Pass      string
    Vhost     string
    Consumers  []RabbitConsumer
    Publishers []RabbitPublish
}

type RabbitConsumer struct {
    Alias         string
    Queue         string
    PrefetchCount int
}

type RabbitPublish struct {
    Alias      string
    Exchange   string
    RoutingKey string
    ReplyTo    string
}
```

Alias используется для построения путей вида:

```
consumer.Main.OrdersCreated
publisher.Main.SendOrder
```

---

# 🚀 Инициализация пула

```go
setting := RabbitSetting{
    Connects: []RabbitConnect{
        {
            Alias: "Main",
            Host:  "localhost",
            Port:  "5672",
            Login: "guest",
            Pass:  "guest",
            Vhost: "/",
            Consumers: []RabbitConsumer{
                {Alias: "OrdersCreated", Queue: "orders.created", PrefetchCount: 10},
            },
            Publishers: []RabbitPublish{
                {Alias: "OrdersOut", Exchange: "orders", RoutingKey: "orders.out"},
            },
        },
    },
}

pool := turbine.NewRabbitMQPool(setting, logger)
pool.Connect()
```

---

# 📥 Подписка на сообщения

Подписка выполняется по пути:

```
Main.OrdersCreated
```

Полный вызов:

```go
pool.Subscribe("Main.OrdersCreated", func(ctx *turbine.Context) error {
    fmt.Println("Received:", ctx.Body())
    return nil
}, 5)
```

Здесь:

* `"Main"` — alias подключения
* `"OrdersCreated"` — alias consumer
* `5` — количество worker-ов

Worker-ы автоматически восстанавливаются при reconnect.

---

# 📤 Публикация сообщений

Публикация выполняется по пути:

```
Main.OrdersOut
```

Пример:

```go
pool.Publish("Main.OrdersOut", `{"status": "ok"}`, "")
```

Если `ReplyTo` не указан — используется значение из настроек.

Публикация выполняется через `SafePublish`, что гарантирует:

* Publisher Confirm
* retry при ошибке
* проверку существования exchange
* восстановление канала

---

# 🔗 Middleware

Пул поддерживает цепочки middleware, которые оборачивают каждый Handler.

```go
pool.Use(func(next turbine.Handler) turbine.Handler {
    return func(ctx *turbine.Context) error {
        fmt.Println("[MW] Before:", ctx.Body())
        err := next(ctx)
        fmt.Println("[MW] After:", ctx.Body())
        return err
    }
})
```

Все middleware будут применяться в порядке добавления.

---

# 🧱 Структура Context

Каждый Handler получает объект `Context`, который инкапсулирует:

* тело сообщения
* контекст выполнения (`context.Context`)
* возможность обновлять контекст (например, добавлять таймауты или значения)

```go
type Context struct {
    context context.Context
    body    string
}

func (c *Context) Body() string
func (c *Context) Context() context.Context
func (c *Context) WithContext(ctx context.Context)
```

### Методы:

| Метод                | Описание                               |
| -------------------- | -------------------------------------- |
| **Body()**           | Возвращает тело входящего сообщения    |
| **Context()**        | Возвращает текущий контекст выполнения |
| **WithContext(ctx)** | Обновляет внутренний `context.Context` |

---

# 🧩 Работа с Context

### Пример обработки сообщения с таймаутом

```go
pool.Subscribe("Main.OrdersCreated", func(ctx *turbine.Context) error {
    fmt.Println("Body:", ctx.Body())

    // получить текущий контекст
    baseCtx := ctx.Context()

    // создать новый контекст с таймаутом
    newCtx, cancel := context.WithTimeout(baseCtx, time.Second*3)
    defer cancel()

    // обновить контекст внутри Context
    ctx.WithContext(newCtx)

    return nil
}, 5)
```

---

# 🧱 Пример использования Context в middleware

```go
pool.Use(func(next turbine.Handler) turbine.Handler {
    return func(ctx *turbine.Context) error {

        traceId := uuid.New().String()

        // добавляем trace-id
        newCtx := context.WithValue(ctx.Context(), "trace-id", traceId)
        ctx.WithContext(newCtx)

        return next(ctx)
    }
})
```

---

# 🛑 Отключение пула

```go
pool.Disconnect()
```

Метод корректно:

* останавливает worker-ы
* завершает подписки
* отключает всех RabbitMQ-клиентов