# 分布式事件通知系统（微服务 & RocketMQ）

这是一个用于“关键事件触发后通知外部供应商 HTTP(S) API”的事件通知系统，依赖 **RocketMQ** 提供可靠投递能力。
系统采用解耦的微服务架构：**API（接收端）** 和 **Worker（处理端）**。

## 架构设计

系统采用生产者-消费者模型，以确保高吞吐量和可靠性：

1. **通知 API（cmd/api）**
   - 角色：生产者（事件接收）
   - 职责：暴露 HTTP 接口，校验传入事件，并将事件发布到 RocketMQ Topic（由配置决定）。
   - 特点：无状态、快速响应。

2. **RocketMQ Broker**
   - 角色：消息代理
   - 职责：持久化消息并负责投递给消费者，实现接收与处理逻辑解耦。

3. **通知 Worker（cmd/worker）**
   - 角色：消费者（事件处理）
   - 职责：订阅 Topic，按配置渲染 HTTP 请求（Header/Body），调用外部供应商接口；失败时执行退避重试与死信队列投递。
   - 特点：异步处理、可靠投递、自动重试。

## 核心特性

- 微服务架构：接收（Ingestion）与处理（Processing）清晰分离
- 配置化路由：基于 config.json 中的 event_type 决定发往哪个 Topic，以及外部 API 的 Method/URL/Header/Body
- Payload 模板：支持 `{$.event.user_id}` 这类占位符从事件 data 中取值
- 双层重试
  - 本地 HTTP 退避重试：Worker 单次消费内进行少量快速重试，吸收瞬时抖动
  - MQ 重试：本地重试仍失败则返回 ConsumeRetryLater，交由 RocketMQ 进行重投（reconsume）
- 死信队列（DLQ）：当 RocketMQ 重投次数超过阈值时，Worker 将消息投递到 DLQ Topic，避免无限重试

## 使用指南

### 1. 配置文件

编辑 config.json 以定义通知规则和 MQ 设置。

```json
{
  "mq": {
    "name_server": "127.0.0.1:9876",
    "access_key": "",
    "secret_key": "",
    "group_name": "notification_consumer_group",
    "max_retries": 16
  },
  "notifications": [
    {
      "event_type": "registration",
      "queue_name": "registration_queue",
      "http_method": "POST",
      "http_url": "https://httpbin.org/post",
      "headers": { "Content-Type": "application/json" },
      "body": {
        "user_id": "{$.event.user_id}",
        "source": "internal_api"
      }
    }
  ]
}
```

字段说明：
- mq.max_retries：RocketMQ 重投（reconsume）达到该次数后，Worker 会将消息投递到死信队列并确认消费成功（默认 16）
- notifications[].queue_name：RocketMQ Topic 名称

### 2. 环境准备

需要一个运行中的 RocketMQ 实例。使用 Docker 可以快速启动：

```bash
docker run -d --name rmqnamesrv -p 9876:9876 apache/rocketmq:latest sh mqnamesrv
docker run -d --name rmqbroker --link rmqnamesrv:namesrv -e "NAMESRV_ADDR=namesrv:9876" -p 10911:10911 -p 10909:10909 apache/rocketmq:latest sh mqbroker -c ../conf/broker.conf
```

### 3. 启动服务

在不同终端分别启动 API 和 Worker：

终端 1：启动 API
```bash
go run cmd/api/main.go
```

终端 2：启动 Worker
```bash
go run cmd/worker/main.go
```

### 4. 发送测试事件

可以使用 curl 或 test_script.sh 发送事件到 API：

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "registration",
    "data": {
      "user_id": "12345",
      "email": "test@example.com"
    }
  }'
```

## 失败处理与死信队列

- 本地 HTTP 退避重试：Worker 在一次消费回调中最多进行 3 次本地重试（指数退避），用于应对网络抖动/短暂 5xx/429
- MQ 重试：若本地重试后仍失败，Worker 返回 ConsumeRetryLater，RocketMQ 会按其策略重新投递消息
- 死信队列：当 msg.ReconsumeTimes >= mq.max_retries 时，Worker 会将原消息体投递到 DLQ Topic，然后返回 ConsumeSuccess

DLQ Topic 命名规则：
- 原 Topic：registration_queue
- DLQ Topic：DLQ_registration_queue

## 项目结构

```
.
├── cmd
│   ├── api          # 接收服务入口（HTTP Server -> RocketMQ）
│   └── worker       # 处理服务入口（RocketMQ -> External API，含 DLQ 投递）
├── pkg
│   ├── config       # 配置加载、校验、查找
│   ├── event        # 事件数据结构定义
│   ├── mq           # RocketMQ Producer/Consumer 封装
│   └── worker       # Worker 核心逻辑（订阅、消费、HTTP 发送、重试、DLQ）
├── config.json      # 配置文件
└── README.md        # 说明文档
```

## 环境要求

- Go 1.16+
- RocketMQ 4.x/5.x
