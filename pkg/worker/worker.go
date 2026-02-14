package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"

	"notification-system/pkg/config"
	"notification-system/pkg/event"
	"notification-system/pkg/mq"
)

// Worker handles the processing of events received from RocketMQ.
type Worker struct {
	Config      *config.Config
	Client      *http.Client
	Consumer    rocketmq.PushConsumer
	DLQProducer rocketmq.Producer
}

// NewWorker creates a new Worker instance and initializes the RocketMQ consumer.
func NewWorker(cfg *config.Config) (*Worker, error) {
	c, err := mq.NewPushConsumer(cfg.MQ.NameServer, cfg.MQ.AccessKey, cfg.MQ.SecretKey, cfg.MQ.GroupName)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Initialize Producer for DLQ
	p, err := mq.NewProducer(cfg.MQ.NameServer, cfg.MQ.AccessKey, cfg.MQ.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create DLQ producer: %w", err)
	}

	return &Worker{
		Config:      cfg,
		Client:      &http.Client{Timeout: 10 * time.Second},
		Consumer:    c,
		DLQProducer: p,
	}, nil
}

// Start subscribes to topics and starts the consumer.
func (w *Worker) Start(ctx context.Context) error {
	topics := make(map[string]bool)
	for _, n := range w.Config.Notifications {
		// Avoid duplicate subscriptions
		if _, exists := topics[n.QueueName]; exists {
			continue
		}

		// Subscribe to topic
		if err := w.Consumer.Subscribe(n.QueueName, consumer.MessageSelector{}, w.HandleMessage); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", n.QueueName, err)
		}
		topics[n.QueueName] = true
		log.Printf("Subscribed to topic: %s for event type: %s", n.QueueName, n.EventType)
	}

	if err := w.Consumer.Start(); err != nil {
		return fmt.Errorf("failed to start consumer: %w", err)
	}

	return nil
}

// Shutdown stops the consumer.
func (w *Worker) Shutdown() error {
	if err := w.DLQProducer.Shutdown(); err != nil {
		log.Printf("Failed to shutdown DLQ producer: %v", err)
	}
	return w.Consumer.Shutdown()
}

// HandleMessage is the callback function invoked by RocketMQ Consumer when a new message arrives.
// It implements the consumer logic: Unmarshal -> Find Config -> Render Body -> Send Request.
func (w *Worker) HandleMessage(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
	for _, msg := range msgs {
		fmt.Printf("[Worker] Received message from topic: %s, msgId: %s, reconsumeTimes: %d\n", msg.Topic, msg.MsgId, msg.ReconsumeTimes)

		// Check for MaxRetries (DLQ Logic)
		// RocketMQ uses int32 for ReconsumeTimes
		if int(msg.ReconsumeTimes) >= w.Config.MQ.MaxRetries {
			fmt.Printf("[Worker] Message %s exceeded max retries (%d). Sending to DLQ.\n", msg.MsgId, w.Config.MQ.MaxRetries)
			if err := w.sendToDLQ(ctx, msg); err != nil {
				fmt.Printf("[Worker] Failed to send message %s to DLQ: %v\n", msg.MsgId, err)
				// If DLQ send fails, we might want to retry later, or just log error and consume success to avoid infinite loop
				// Let's retry later to be safe, hoping DLQ issue is transient
				return consumer.ConsumeRetryLater, nil
			}
			return consumer.ConsumeSuccess, nil
		}

		// 1. Unmarshal Event
		var evt event.Event
		if err := json.Unmarshal(msg.Body, &evt); err != nil {
			fmt.Printf("[Worker] Error unmarshalling event data: %v. Skipping message.\n", err)
			// Return ConsumeSuccess to acknowledge the message and prevent infinite redelivery of bad data
			return consumer.ConsumeSuccess, nil
		}

		// 2. Find Notification Configuration
		notifyConfig := w.Config.FindNotificationConfig(evt.Type)
		if notifyConfig == nil {
			fmt.Printf("[Worker] No configuration found for event type: %s. Skipping message.\n", evt.Type)
			return consumer.ConsumeSuccess, nil
		}

		// 3. Process Notification
		if err := w.processNotification(notifyConfig, evt); err != nil {
			fmt.Printf("[Worker] Failed to send notification for event %s: %v. Will retry.\n", evt.ID, err)
			// Return ConsumeRetryLater to let RocketMQ handle the retry (with backoff)
			return consumer.ConsumeRetryLater, nil
		}
	}
	return consumer.ConsumeSuccess, nil
}

func (w *Worker) sendToDLQ(ctx context.Context, msg *primitive.MessageExt) error {
	dlqTopic := fmt.Sprintf("DLQ_%s", msg.Topic)
	dlqMsg := &primitive.Message{
		Topic: dlqTopic,
		Body:  msg.Body,
	}
	// Copy properties if needed
	dlqMsg.WithProperties(msg.GetProperties())

	_, err := w.DLQProducer.SendSync(ctx, dlqMsg)
	return err
}

func (w *Worker) processNotification(cfg *config.NotificationConfig, evt event.Event) error {
	// 1. Render Request Body using the template from config
	reqBody, err := w.renderBody(cfg.Body, evt)
	if err != nil {
		return fmt.Errorf("failed to render body: %w", err)
	}

	// Local Retry Logic with Exponential Backoff
	maxLocalRetries := 3
	var lastErr error

	for i := 0; i < maxLocalRetries; i++ {
		if i > 0 {
			// Exponential backoff: 200ms, 400ms, 800ms...
			backoff := time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
			fmt.Printf("[Worker] Local retry %d/%d for event %s in %v\n", i+1, maxLocalRetries, evt.ID, backoff)
			time.Sleep(backoff)
		}

		// 2. Create HTTP Request
		req, err := http.NewRequest(cfg.Method, cfg.URL, bytes.NewBuffer(reqBody))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		// 3. Set Headers
		for k, v := range cfg.Headers {
			req.Header.Set(k, v)
		}

		// 4. Execute Request
		resp, err := w.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request network error: %w", err)
			continue // Retry on network error
		}
		defer resp.Body.Close()

		// 5. Check Response Status
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("[Worker] Notification sent successfully for event %s to %s\n", evt.ID, cfg.URL)
			return nil
		}

		// If 5xx, retry. If 4xx (client error), maybe don't retry?
		// For simplicity and robustness, let's retry 5xx and 429.
		// Fail fast on 400, 401, 403, 404
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			body, _ := ioutil.ReadAll(resp.Body)
			return fmt.Errorf("request failed with client error status %d: %s", resp.StatusCode, string(body))
		}

		body, _ := ioutil.ReadAll(resp.Body)
		lastErr = fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return lastErr
}

// renderBody replaces placeholders in the template body with actual values from the event.
func (w *Worker) renderBody(templateBody map[string]interface{}, evt event.Event) ([]byte, error) {
	rendered := w.replacePlaceholders(templateBody, evt)
	return json.Marshal(rendered)
}

// replacePlaceholders recursively traverses the template and replaces strings matching {$.event.field}.
func (w *Worker) replacePlaceholders(v interface{}, evt event.Event) interface{} {
	switch val := v.(type) {
	case string:
		return w.resolveValue(val, evt)
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v := range val {
			newMap[k] = w.replacePlaceholders(v, evt)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(val))
		for i, v := range val {
			newSlice[i] = w.replacePlaceholders(v, evt)
		}
		return newSlice
	default:
		return val
	}
}

// resolveValue checks if a string is a placeholder and resolves it.
// Supported syntax: "{$.event.field}"
func (w *Worker) resolveValue(val string, evt event.Event) interface{} {
	if strings.HasPrefix(val, "{$.event.") && strings.HasSuffix(val, "}") {
		path := val[2 : len(val)-1] // Remove { and } -> $.event.field
		parts := strings.Split(path, ".")

		// Currently only supports $.event.field (depth of 3: $, event, field)
		// Can be extended to support nested JSON path if needed
		if len(parts) == 3 && parts[0] == "$" && parts[1] == "event" {
			key := parts[2]
			if v, ok := evt.Data[key]; ok {
				return v
			}
			// If key not found, return original string or null?
			// Returning original string helps debugging configuration errors
			return val
		}
	}
	return val
}
