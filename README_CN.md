# CLIProxyAPI Quota Inspector

[English](./README.md)

---

![CLIProxyAPI Quota Inspector](./img.png)

面向 CLIProxyAPI / CPA 的跨平台配额查询工具。

它会直接读取已运行的 CPA 服务中的真实数据，在终端里按 provider 展示账号配额、状态、进度条和汇总信息。

##  功能

- 查看 Codex 账号的 `5h` / `7d` 配额
- 查看 Gemini CLI 的模型配额
- 查看 Antigravity 的模型配额
- 按 provider 分区展示结果
- 按账号计划、状态和剩余额度做汇总分析
- 支持批量查询大量认证文件

## 支持的 provider

- Codex
- Gemini CLI
- Antigravity
- 更多 provider 计划中

## 使用前提

- 已启动 CPA 服务
- 已配置可用的认证文件
- 若管理接口开启鉴权，需要提供 management key

## 构建

```bash
go build -o cpa-quota-inspector .
```

## 快速开始

```bash
./cpa-quota-inspector -k YOUR_MANAGEMENT_KEY
```

默认会连接 `http://127.0.0.1:8317`。

## 常用参数

- `-k`, `--management-key`: CPA management key
- `--cpa-base-url`: CPA 地址，默认 `http://127.0.0.1:8317`
- `--concurrency`: 并发查询数，默认 `32`
- `--timeout`: 请求超时秒数
- `--retry-attempts`: 查询失败后的重试次数
- `--filter-provider`: 仅查看指定 provider
- `--filter-plan`: 仅查看指定计划类型
- `--filter-status`: 仅查看指定状态
- `--json`: 输出 JSON
- `--plain`: 输出纯文本
- `--summary-only`: 仅显示汇总
- `--ascii-bars`: 使用 ASCII 进度条
- `--no-progress`: 关闭查询进度显示
- `--version`: 显示版本信息

## 使用示例

查看全部账号：

```bash
./cpa-quota-inspector -k YOUR_MANAGEMENT_KEY
```

仅查看 Codex：

```bash
./cpa-quota-inspector \
  --filter-provider codex \
  -k YOUR_MANAGEMENT_KEY
```

仅查看 Gemini CLI：

```bash
./cpa-quota-inspector \
  --filter-provider gemini-cli \
  -k YOUR_MANAGEMENT_KEY
```

仅输出汇总：

```bash
./cpa-quota-inspector \
  --summary-only \
  -k YOUR_MANAGEMENT_KEY
```

输出 JSON：

```bash
./cpa-quota-inspector \
  --json \
  -k YOUR_MANAGEMENT_KEY
```

关闭进度显示：

```bash
./cpa-quota-inspector \
  --no-progress \
  -k YOUR_MANAGEMENT_KEY
```

## 输出内容

- 账号文件名
- provider / plan / 状态
- 各类配额进度条
- 各 provider 的汇总统计

## 说明

- 当前不展示 code review 配额
- 当前正式支持 `Codex + Gemini CLI + Antigravity`
