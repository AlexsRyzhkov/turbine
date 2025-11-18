Готово — вот итоговый **README.md**, оформленный правильно, без упоминаний неэкспортируемых методов/полей (все названия — с заглавной буквы).
Готов к сохранению как есть.

---

# 🐇 RabbitMQ Client

Надёжный и простой в использовании клиент для RabbitMQ, который обеспечивает:

* Автоматическое переподключение
* Безопасную публикацию сообщений
* Обработку Publisher Confirm
* Автоматическое восстановление Consumer
* Пассивную проверку существования Exchange и Queue

Библиотека подходит для высоконагруженных и критичных систем.

---

## 📦 Установка

```bash
go get github.com/AlexsRyzhkov/turbine
```

---

# 🚀 Быстрый старт

## Инициализация подключения

```go
logger := MyLogger{}

rabbit := turbine.NewRabbitMQ(
    "amqp://guest:guest@localhost:5672/",
    logger,
)

if err := rabbit.Connect(); err != nil {
    logger.Errorf("RabbitMQ connection failed: %v", err)
    return
}
```

---

# 📤 Публикация сообщений

Используйте безопасный механизм отправки:

```go
err := rabbit.SafePublish(
    "MyExchange",
    "My.Routing.Key",
    "",
    `{"hello":"world"}`,
)

if err != nil {
    logger.Errorf("Publish error: %v", err)
}
```

### Гарантии `SafePublish`:

* Проверяет существование Exchange
* Автоматически восстанавливает каналы
* Повторяет попытку отправки при ошибке
* Ждёт подтверждение Publisher Confirm
* Обеспечивает надёжную доставку

---

# 📥 Подписка на очередь

```go
deliveries, err := rabbit.Subscribe("MyQueue", 10)
if err != nil {
    logger.Errorf("Subscribe error: %v", err)
    return
}
```

### Обработка сообщений:

```go
go func() {
    for msg := range deliveries {
        fmt.Println("Received:", string(msg.Body))

        msg.Ack(false)
    }
}()
```

Consumer:

* автоматически пересоздаётся при разрыве
* работает в отдельной горутине
* использует Prefetch

---

# 🛠 Полный пример (Publisher + Consumer)

```go
package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "your_project/turbine"
)

type MyLogger struct{}

func (MyLogger) Infof(f string, v ...interface{})  { fmt.Printf("[INFO] "+f+"\n", v...) }
func (MyLogger) Warnf(f string, v ...interface{})  { fmt.Printf("[WARN] "+f+"\n", v...) }
func (MyLogger) Errorf(f string, v ...interface{}) { fmt.Printf("[ERROR] "+f+"\n", v...) }

func main() {
    logger := MyLogger{}

    rabbit := turbine.NewRabbitMQ(
        "amqp://guest:guest@localhost:5672/",
        logger,
    )

    if err := rabbit.Connect(); err != nil {
        logger.Errorf("Connect failed: %v", err)
        return
    }

    deliveries, err := rabbit.Subscribe("MyQueue", 20)
    if err != nil {
        logger.Errorf("Subscribe error: %v", err)
        return
    }

    go func() {
        for msg := range deliveries {
            fmt.Println("Message:", string(msg.Body))
            msg.Ack(false)
        }
    }()

    rabbit.SafePublish(
        "MyExchange",
        "MyKey",
        "",
        "Test message",
    )

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    rabbit.Disconnect()
}
```

---

# ⚙ Работа с административным каналом

Административный канал позволяет создавать Exchange, Queue и Bind:

```go
ch := rabbit.AdminChannel()

ch.ExchangeDeclare(
    "MyExchange",
    "direct",
    true,
    false,
    false,
    false,
    nil,
)

ch.QueueDeclare(
    "MyQueue",
    true,
    false,
    false,
    false,
    nil,
)

ch.QueueBind(
    "MyQueue",
    "MyKey",
    "MyExchange",
    false,
    nil,
)
```

---

# 🧹 Завершение работы

```go
rabbit.Disconnect()
```

Метод корректно:

* Завершает фоновые воркеры
* Дожидается отправки очереди публикаций
* Закрывает каналы
* Закрывает соединение

---

# ❗ Рекомендации

* Подтверждайте сообщения вручную (`msg.Ack(false)`)
* Не используйте прямой вызов Publish канала — только `SafePublish`
* Держите длительные consumer'ы в отдельных горутинах
* Закрывайте соединение через `Disconnect()`

---

