# CLIProxyAPI Quota Inspector

[中文版本](./README_CN.md)

---

![CLIProxyAPI Quota Inspector](./img.png)

A cross-platform quota inspector for CLIProxyAPI / CPA.

It reads live data from a running CPA service and shows provider-based quota sections, statuses, progress bars, and summary statistics in the terminal.

## Features

- View Codex `5h` / `7d` quotas
- View Gemini CLI model quotas
- View Antigravity model quotas
- Show results in separate provider sections
- Summarize accounts by plan, status, and remaining quota
- Support batch inspection for many auth files

## Supported providers

- Codex
- Gemini CLI
- Antigravity
- More providers coming soon

## Requirements

- A running CPA service
- Available auth files already configured
- A management key if CPA management auth is enabled

## Build

```bash
go build -o cpa-quota-inspector .
```

## Quick start

```bash
./cpa-quota-inspector -k YOUR_MANAGEMENT_KEY
```

The default CPA endpoint is `http://127.0.0.1:8317`.

## Common flags

- `-k`, `--management-key`: CPA management key
- `--cpa-base-url`: CPA base URL, default `http://127.0.0.1:8317`
- `--concurrency`: concurrent workers, default `128`
- `--management-concurrency`: concurrent `/v0/management/api-call` requests, default `64`
- `--timeout`: request timeout in seconds
- `--retry-attempts`: retry count after failed queries
- `--filter-provider`: show only the specified provider
- `--filter-plan`: show only the specified plan type
- `--filter-status`: show only the specified status
- `--json`: print JSON output
- `--plain`: print plain text output
- `--summary-only`: show summary only
- `--ascii-bars`: use ASCII progress bars
- `--no-progress`: disable fetch progress display
- `--version`: print version information

## Examples

Show all accounts:

```bash
./cpa-quota-inspector -k YOUR_MANAGEMENT_KEY
```

Show only Codex:

```bash
./cpa-quota-inspector \
  --filter-provider codex \
  -k YOUR_MANAGEMENT_KEY
```

Show only Gemini CLI:

```bash
./cpa-quota-inspector \
  --filter-provider gemini-cli \
  -k YOUR_MANAGEMENT_KEY
```

Show summary only:

```bash
./cpa-quota-inspector \
  --summary-only \
  -k YOUR_MANAGEMENT_KEY
```

Print JSON:

```bash
./cpa-quota-inspector \
  --json \
  -k YOUR_MANAGEMENT_KEY
```

Run a larger batch with explicit concurrency:

```bash
./cpa-quota-inspector \
  --concurrency 128 \
  --management-concurrency 64 \
  -k YOUR_MANAGEMENT_KEY
```

Disable progress display:

```bash
./cpa-quota-inspector \
  --no-progress \
  -k YOUR_MANAGEMENT_KEY
```

## Output

- Auth file name
- Provider / plan / status
- Quota progress bars
- Per-provider summary statistics

## Notes

- Code review quota is not shown
- Current support includes `Codex + Gemini CLI + Antigravity`
