# 分布式事件通知系统 (微服务 & RocketMQ)

这是一个健壮的事件通知系统，旨在通过 **RocketMQ** 可靠地处理业务事件并将其分发给外部 API 供应商。
系统采用解耦的微服务架构：**API (接收端)** 和 **Worker (处理端)**。

## 🏗 架构设计

系统采用生产者-消费者模型，以确保高吞吐量和可靠性：

1.  **通知 API (`cmd/api`)**: 
    - **角色**: 生产者 (事件接收)
    - **职责**: 暴露 HTTP 接口，校验传入事件，并将事件发布到 RocketMQ 主题。
    - **特点**: 无状态、高吞吐、快速响应。
    
2.  **RocketMQ Broker**: 
    - **角色**: 消息代理
    - **职责**: 持久化消息并处理投递给消费者。实现接收与处理逻辑的解耦。

3.  **通知 Worker (`cmd/worker`)**: 
    - **角色**: 消费者 (事件处理)
    - **职责**: 订阅 RocketMQ 主题，根据模板渲染请求体，并向外部供应商发送 HTTP 请求。
    - **特点**: 异步、可靠、自动重试。

## ✨ 核心特性

- **微服务架构**: 接收 (Ingestion) 与处理 (Processing) 逻辑清晰分离。
- **配置化路由**: 基于 `config.json` 中定义的 `event_type` 将事件分发到不同的外部 API。
- **灵活的 Payload 模板**: 支持使用类似 JSONPath 的语法（如 `{$.event.user_id}`）从事件中提取数据。
- **可靠性**: 利用 RocketMQ 进行消息持久化，确保不丢消息。
- **重试机制**: 利用 RocketMQ 的内置重试机制（指数退避）自动处理失败的外部 API 调用。

## 🚀 使用指南

### 1. 配置文件

编辑 `config.json` 以定义您的通知规则和 MQ 设置。

```json
{
    "mq": {
        "name_server": "127.0.0.1:9876",
        "access_key": "",
        "secret_key": "",
        "group_name": "notification_consumer_group"
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

### 2. 环境准备

您需要一个运行中的 RocketMQ 实例。
如果您使用 Docker，可以使用以下命令启动：

```bash
# 启动 NameServer
docker run -d --name rmqnamesrv -p 9876:9876 apache/rocketmq:latest sh mqnamesrv

# 启动 Broker
docker run -d --name rmqbroker --link rmqnamesrv:namesrv -e "NAMESRV_ADDR=namesrv:9876" -p 10911:10911 -p 10909:10909 apache/rocketmq:latest sh mqbroker -c ../conf/broker.conf
```

### 3. 启动服务

您需要在不同的终端中分别启动 API 和 Worker 服务。

**终端 1: 启动 API 服务**
```bash
go run cmd/api/main.go
# 输出: API Server started on :8080
```

**终端 2: 启动 Worker 服务**
```bash
go run cmd/worker/main.go
# 输出: RocketMQ Subscriber (Worker) started.
```

### 4. 发送测试事件

使用 `curl` 或项目提供的 `test_script.sh` 发送事件。

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

## 📂 项目结构

```
.
├── cmd
│   ├── api          # 接收服务入口 (HTTP Server -> RocketMQ)
│   └── worker       # 处理服务入口 (RocketMQ -> External API)
├── pkg
│   ├── config       # 配置加载与校验
│   ├── event        # 事件数据结构定义
│   ├── mq           # RocketMQ 生产者封装
│   ├── subscriber   # RocketMQ 消费者封装
│   └── worker       # 核心业务逻辑（模板渲染、HTTP 请求）
├── config.json      # 配置文件
└── README.md        # 说明文档
```

## 📋 环境要求

- Go 1.16+
- RocketMQ 4.x/5.x
