package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
)

// NotificationConfig defines how to notify an external system for a specific event type.
type NotificationConfig struct {
	EventType string                 `json:"event_type"`
	QueueName string                 `json:"queue_name"`
	Method    string                 `json:"http_method"`
	URL       string                 `json:"http_url"`
	Headers   map[string]string      `json:"headers"`
	Body      map[string]interface{} `json:"body"`
}

// MQConfig holds the configuration for RocketMQ.
type MQConfig struct {
	NameServer string `json:"name_server"`
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key"`
	GroupName  string `json:"group_name"`
	MaxRetries int    `json:"max_retries"`
}

// Config holds the list of all notification configurations.
type Config struct {
	MQ            MQConfig             `json:"mq"`
	Notifications []NotificationConfig `json:"notifications"`
}

// LoadConfig reads the configuration from a JSON file.
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(bytes, &config); err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.MQ.NameServer == "" {
		return fmt.Errorf("mq.name_server is required")
	}
	if c.MQ.GroupName == "" {
		return fmt.Errorf("mq.group_name is required")
	}

	if c.MQ.MaxRetries < 0 {
		return fmt.Errorf("mq.max_retries cannot be negative")
	}
	if c.MQ.MaxRetries == 0 {
		c.MQ.MaxRetries = 16 // Default RocketMQ behavior
	}

	if len(c.Notifications) == 0 {
		return fmt.Errorf("no notifications configured")
	}

	for i, n := range c.Notifications {
		if n.EventType == "" {
			return fmt.Errorf("notifications[%d].event_type is required", i)
		}
		if n.QueueName == "" {
			return fmt.Errorf("notifications[%d].queue_name is required", i)
		}
		if n.Method == "" {
			return fmt.Errorf("notifications[%d].http_method is required", i)
		}
		validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true}
		if !validMethods[strings.ToUpper(n.Method)] {
			return fmt.Errorf("notifications[%d].http_method '%s' is invalid", i, n.Method)
		}
		if n.URL == "" {
			return fmt.Errorf("notifications[%d].http_url is required", i)
		}
		if _, err := url.ParseRequestURI(n.URL); err != nil {
			return fmt.Errorf("notifications[%d].http_url '%s' is invalid: %v", i, n.URL, err)
		}
	}
	return nil
}

// FindNotificationConfig returns the notification configuration for a given event type.
func (c *Config) FindNotificationConfig(eventType string) *NotificationConfig {
	for _, n := range c.Notifications {
		if n.EventType == eventType {
			return &n
		}
	}
	return nil
}
