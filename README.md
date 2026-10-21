# Videoscope

面向长视频的内容理解平台。上传课程或讲座视频，自动完成语音转写、画面文字识别与结构化总结生成。

## 能做什么

一次完整流程：注册登录 → 分片上传视频 → 提交分析 → 后台异步处理 → 获取结构化结果。

处理链路包含四步：从视频抽取音轨并按窗口切分、逐段调用语音识别转成带时间戳的文本、按固定间隔抽取关键帧并识别画面文字、把语音与画面按时间轴融合后交给大模型生成结构化总结。

## 技术栈

Go / Gin / GORM / MySQL / Redis（Lua）/ RabbitMQ / 对象存储（S3 兼容）/ ffmpeg

## 架构

```
客户端
  │  分片上传
  ▼
Gin 接口层 ──► 对象存储（分片落盘、合并成品）
  │                │
  │  提交分析       │ 下载
  ▼                ▼
RabbitMQ 队列 ──► 消费者 ──► ffmpeg 抽音轨、抽关键帧
                    │              │
                    │              ▼
                    │        语音识别 / 画面文字识别
                    │              │
                    ▼              ▼
              任务状态机 ◄── 多模态上下文融合
                    │              │
                    │              ▼
                    │        大模型生成结构化总结
                    ▼              ▼
                 MySQL        分析结果落库
```

## 关键设计

**上传与处理解耦。**分片合并落盘后，处理任务投递到消息队列异步执行，接口只做状态推进和消息投递。完整处理耗时以分钟计，接口响应在毫秒级。

**处理链路幂等。**同一视频可能被重复提交、消息可能重投。用三层防护：分布式锁按内容摘要互斥、消费前校验任务状态、状态推进走带条件的更新。

**多模态对齐不依赖识别服务。**语音识别接口不一定返回句子级时间戳，所以把音频切成固定长度分段，用分段边界作为时间轴刻度，配合同样粒度的上下文窗口完成对齐。

**结构化输出。**模型必须返回约定格式的 JSON，接口层强制 JSON 模式，解析前剥离可能存在的 Markdown 围栏。

## 快速开始

依赖：MySQL、Redis、RabbitMQ、S3 兼容对象存储（如 MinIO）、ffmpeg。

```bash
# 初始化数据库
mysql -u root < mysql-setup.sql

# 配置（未配置 AI 服务时使用模拟实现，链路可完整跑通）
export VIDEOSCOPE_ASR_BASE_URL=...
export VIDEOSCOPE_ASR_API_KEY=...
export VIDEOSCOPE_LLM_BASE_URL=...
export VIDEOSCOPE_LLM_API_KEY=...

go run .
```

服务默认监听 `:8080`。

## 接口

```
POST /api/register            注册
POST /api/login               登录

POST /api/upload/chunk        上传分片
GET  /api/upload/status       查询上传进度
POST /api/upload/merge        合并分片

POST /api/analyze             提交分析
GET  /api/task/:id/download   获取视频下载地址
GET  /healthz                 健康检查
```

## 目录结构

```
config/     配置读取，全部走环境变量
models/     数据模型与任务状态定义
repos/      数据访问层
services/   业务逻辑：存储、消息、锁、限流、媒体处理、模型调用
handlers/   HTTP 接口
scripts/    冒烟测试与压测
```

## 测试

```bash
go test ./...
```

`scripts/` 目录下有端到端冒烟测试和 k6 压测脚本。
