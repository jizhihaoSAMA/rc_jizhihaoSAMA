package mq

import (
	"context"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
)

// NewProducer creates and starts a RocketMQ producer.
func NewProducer(endpoint, accessKey, secretKey string) (rocketmq.Producer, error) {
	opts := []producer.Option{
		producer.WithNsResolver(primitive.NewPassthroughResolver([]string{endpoint})),
		producer.WithRetry(2),
	}

	if accessKey != "" && secretKey != "" {
		opts = append(opts, producer.WithCredentials(primitive.Credentials{
			AccessKey: accessKey,
			SecretKey: secretKey,
		}))
	}

	p, err := rocketmq.NewProducer(opts...)
	if err != nil {
		return nil, err
	}

	if err := p.Start(); err != nil {
		return nil, err
	}

	return p, nil
}

// NewPushConsumer creates and starts a RocketMQ push consumer.
// Note: You must call Subscribe and then Start on the returned consumer.
func NewPushConsumer(endpoint, accessKey, secretKey, groupName string) (rocketmq.PushConsumer, error) {
	opts := []consumer.Option{
		consumer.WithNsResolver(primitive.NewPassthroughResolver([]string{endpoint})),
		consumer.WithGroupName(groupName),
		consumer.WithConsumeFromWhere(consumer.ConsumeFromLastOffset),
	}

	if accessKey != "" && secretKey != "" {
		opts = append(opts, consumer.WithCredentials(primitive.Credentials{
			AccessKey: accessKey,
			SecretKey: secretKey,
		}))
	}

	c, err := rocketmq.NewPushConsumer(opts...)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// SendMessage sends a message to the specified topic.
func SendMessage(ctx context.Context, p rocketmq.Producer, topic string, body []byte) error {
	msg := &primitive.Message{
		Topic: topic,
		Body:  body,
	}
	_, err := p.SendSync(ctx, msg)
	return err
}
