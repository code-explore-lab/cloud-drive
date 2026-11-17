package xkafka

// Config 是 Kafka 生产者/消费者的通用配置。
// 由 storage.rpc / storage-worker 的 Config 直接嵌入，交给 conf.MustLoad 统一加载。
type Config struct {
	// Brokers Kafka broker 地址列表，如 ["redpanda:9092"]。
	// go-zero 的 default tag 支持 slice，但直接构造 Config（不经过配置加载）时
	// 不会走默认值填充，所以这里保留 optional，并在 withDefaults 里统一回退。
	Brokers []string `json:",optional"`

	// Topic 生产者默认写入 / 消费者默认订阅的 topic。
	Topic string `json:",default=storage.events"`

	// GroupID 消费者组 ID；生产者为空即可。
	GroupID string `json:",optional"`
}

// defaultBrokers 本地 docker compose 单节点 Redpanda 的默认地址。
// 保持未导出，避免调用方意外改掉包级默认值；withDefaults 会返回副本。
var defaultBrokers = []string{"redpanda:9092"}

// withDefaults 填充空字段，保证直接构造的 Config 也能正常工作。
// 已显式配置的字段原样保留。
func (c Config) withDefaults() Config {
	if len(c.Brokers) == 0 {
		c.Brokers = append([]string(nil), defaultBrokers...)
	}
	if c.Topic == "" {
		c.Topic = TopicStorageEvents
	}
	return c
}
