// Package xkafka 封装 Kafka 生产者、消费者、topic 初始化与事件信封定义。
//
// 这一层只负责传输：事件格式见 event.go；重试、幂等、死信等业务语义
// 由调用方（storage-worker）决定。生产/消费都不在构造函数里连接 broker，
// 让服务在 Kafka 晚于进程启动时仍能起来，由调用方带重试地使用。
package xkafka
