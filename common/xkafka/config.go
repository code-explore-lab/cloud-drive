package xkafka

import "strings"

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
// 已显式配置的字段原样保留；配置文件里的 `${KAFKA_BROKERS}` 在环境变量缺失时
// 会展开成空串，这种占位项按「未配置」处理，否则会去连空地址。
func (c Config) withDefaults() Config {
	brokers := make([]string, 0, len(c.Brokers))
	for _, broker := range c.Brokers {
		if broker = strings.TrimSpace(broker); broker != "" {
			brokers = append(brokers, broker)
		}
	}
	if len(brokers) == 0 {
		c.Brokers = append([]string(nil), defaultBrokers...)
	} else {
		c.Brokers = brokers
	}
	if c.Topic == "" {
		c.Topic = TopicStorageEvents
	}
	return c
}
