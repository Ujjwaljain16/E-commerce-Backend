// Package kafka wraps segmentio/kafka-go with the producer and consumer behaviour the services share:
// synchronous acks=all publishing, at-least-once consumption with bounded retries, and a dead-letter topic.
package kafka

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
	kafkago "github.com/segmentio/kafka-go"
)

// Brokers splits a comma-separated broker list such as "kafka:9092,kafka2:9092".
func Brokers(list string) []string {
	var out []string
	for _, b := range strings.Split(list, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// Publisher is what services depend on, so tests can swap in a fake.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, value []byte) error
}

// Producer publishes messages to any topic.
type Producer struct {
	w       *kafkago.Writer
	service string
}

// NewProducer creates a producer that waits for all in-sync replicas to ack.
func NewProducer(brokers []string, service string) *Producer {
	return &Producer{
		service: service,
		w: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Balancer:               &kafkago.Hash{},
			RequiredAcks:           kafkago.RequireAll,
			AllowAutoTopicCreation: true,
			BatchTimeout:           10 * time.Millisecond,
			WriteTimeout:           10 * time.Second,
		},
	}
}

// Publish writes one message and blocks until it is acknowledged.
func (p *Producer) Publish(ctx context.Context, topic, key string, value []byte) error {
	err := p.w.WriteMessages(ctx, kafkago.Message{Topic: topic, Key: []byte(key), Value: value})
	if err == nil {
		metrics.KafkaMessagesProduced.WithLabelValues(p.service, topic).Inc()
	}
	return err
}

// Close flushes and closes the writer.
func (p *Producer) Close() error { return p.w.Close() }

// Handler processes one message. Returning an error triggers a retry; after MaxAttempts the
// message goes to "<topic>.dlq" so one poison message cannot block the partition.
type Handler func(ctx context.Context, msg kafkago.Message) error

// ConsumerConfig configures a consumer group subscription.
type ConsumerConfig struct {
	Brokers     []string
	GroupID     string
	Topics      []string
	Service     string
	MaxAttempts int
}

// Consumer reads from a consumer group and commits only after the handler succeeds (or the message is dead-lettered).
type Consumer struct {
	r   *kafkago.Reader
	dlq *Producer
	cfg ConsumerConfig
	log *logger.Logger
}

// NewConsumer creates a consumer group reader.
func NewConsumer(cfg ConsumerConfig, log *logger.Logger) *Consumer {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	return &Consumer{
		cfg: cfg,
		log: log,
		dlq: NewProducer(cfg.Brokers, cfg.Service),
		r: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:        cfg.Brokers,
			GroupID:        cfg.GroupID,
			GroupTopics:    cfg.Topics,
			MinBytes:       1,
			MaxBytes:       10e6,
			MaxWait:        500 * time.Millisecond,
			StartOffset:    kafkago.FirstOffset,
			CommitInterval: 0,
			// Without this a group that joined before a topic existed never gets its partitions.
			WatchPartitionChanges: true,
		}),
	}
}

// EnsureTopics creates any missing topics with the broker's default partition count and
// replication factor, so consumers can join before the first producer writes.
func EnsureTopics(ctx context.Context, brokers []string, topics ...string) error {
	var d kafkago.Dialer
	conn, err := d.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	// The admin calls below ignore ctx, so carry its deadline onto the connections.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	cc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer cc.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = cc.SetDeadline(dl)
	}
	cfgs :=make([]kafkago.TopicConfig, len(topics))
	for i, t := range topics {
		cfgs[i] = kafkago.TopicConfig{Topic: t, NumPartitions: -1, ReplicationFactor: -1}
	}
	if err := cc.CreateTopics(cfgs...); err != nil && !errors.Is(err, kafkago.TopicAlreadyExists) {
		return err
	}
	return nil
}

// Run consumes until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, h Handler) error {
	for attempt := 0; ; attempt++ {
		// Bound each attempt: a dial to a broker pod that just went away otherwise waits out the
		// kernel's SYN retries (minutes), and nothing is consumed meanwhile.
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := EnsureTopics(attemptCtx, c.cfg.Brokers, c.cfg.Topics...)
		cancel()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil
		}
		c.log.Warn(ctx, "waiting for kafka to create topics", map[string]interface{}{"error": err.Error(), "attempt": attempt})
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
	for {
		msg, err := c.r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Error(ctx, "kafka fetch failed", map[string]interface{}{"error": err.Error()})
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		outcome := c.handle(ctx, h, msg)
		if ctx.Err() != nil {
			return nil
		}
		metrics.KafkaMessagesConsumed.WithLabelValues(c.cfg.Service, msg.Topic, outcome).Inc()
		if err := c.r.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
			c.log.Error(ctx, "kafka commit failed", map[string]interface{}{"error": err.Error(), "topic": msg.Topic})
		}
	}
}

func (c *Consumer) handle(ctx context.Context, h Handler, msg kafkago.Message) string {
	backoff := 200 * time.Millisecond
	var err error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if err = h(ctx, msg); err == nil {
			return "ok"
		}
		if errors.Is(err, ErrSkip) {
			c.log.Warn(ctx, "kafka message skipped", map[string]interface{}{"topic": msg.Topic, "error": err.Error()})
			return "skipped"
		}
		c.log.Warn(ctx, "kafka handler failed", map[string]interface{}{
			"topic": msg.Topic, "attempt": attempt, "error": err.Error(),
		})
		select {
		case <-ctx.Done():
			return "aborted"
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	dlqTopic := msg.Topic + ".dlq"
	if perr := c.dlq.Publish(ctx, dlqTopic, string(msg.Key), msg.Value); perr != nil {
		c.log.Error(ctx, "dead-letter publish failed", map[string]interface{}{"topic": dlqTopic, "error": perr.Error()})
	}
	c.log.Error(ctx, "kafka message dead-lettered", map[string]interface{}{"topic": msg.Topic, "error": err.Error()})
	return "dead_lettered"
}

// Close closes the reader and the dead-letter producer.
func (c *Consumer) Close() error {
	return errors.Join(c.r.Close(), c.dlq.Close())
}

// ErrSkip marks a message that can never succeed (e.g. malformed JSON) so it is committed without retries.
var ErrSkip = errors.New("skip message")
