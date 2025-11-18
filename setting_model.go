package turbine

type RabbitSetting struct {
	Connects []RabbitConnect `conf:"connects"`
}

type RabbitConnect struct {
	Alias string `conf:"alias"`
	Host  string `conf:"host"`
	Login string `conf:"login"`
	Pass  string `conf:"pass"`
	Vhost string `conf:"vHost"`

	Consumers  []RabbitConsumer `conf:"input"`
	Publishers []RabbitPublish  `conf:"output"`
}

type RabbitConsumer struct {
	Alias         string `conf:"alias"`
	PrefetchCount int    `conf:"prefetchCount"`
	Queue         string `conf:"queue"`
}
type RabbitPublish struct {
	Alias      string `conf:"alias"`
	Exchange   string `conf:"exchange"`
	RoutingKey string `conf:"routingKey"`
}
