# CLIProxyAPI Quota Inspector

[中文版本](./README_CN.md)

---

![CLIProxyAPI Quota Inspector](./img.png)

Live provider-aware quota inspector for CPA management APIs.

This project queries real quota data from a running CPA instance and renders provider-specific terminal sections with status coloring, quota bars, and multi-account summaries.

## Why this tool

- Uses live data from CPA management routes instead of offline estimation.
- Shows provider-specific sections instead of forcing one shared table schema.
- Shows Codex `5h` and `7d` quota windows.
- Shows Gemini CLI grouped model quota sections and supplemental tier information.
- Aggregates equivalent quota percentages per plan (`free`, `plus`).
- Supports progress display while querying many auth files.

## Data source

The tool mirrors CPA management flow for currently supported providers:

1. `GET /v0/management/auth-files`
2. `POST /v0/management/api-call`
3. CPA forwards provider-specific upstream requests

Currently implemented:

- Codex -> `https://chatgpt.com/backend-api/wham/usage`
- Gemini CLI -> `https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota`
- Gemini CLI supplemental tier info -> `https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`

## Status model

Codex status is derived from `7d` remaining percentage:

- `0` -> `exhausted`
- `0-30` -> `low`
- `30-70` -> `medium`
- `70-100` -> `high`
- `100` -> `full`

Gemini CLI status uses the same levels, but is derived from the average remaining percentage across all recognized model quota buckets:

- `0` -> `exhausted`
- `0-30` -> `low`
- `30-70` -> `medium`
- `70-100` -> `high`
- `100` -> `full`

## Features

- Static report output (default) with colored plan and status.
- Provider-specific sections with independent columns and sorting.
- Terminal-width adaptive table layout.
- Unicode gradient quota bars with `--ascii-bars` fallback.
- Optional real-time fetch progress with current auth file name.
- Supports `pretty`, `plain`, and `json` output modes.
- `plain` and `json` also expose provider-specific summaries.
- Retry for transient query failures.

## Requirements

- Go `1.25+`
- Running CPA service
- CPA management key (if enabled)

## Build

```bash
go build -o cpa-quota-inspector .
```

## Quick start

```bash
./cpa-quota-inspector -k YOUR_MANAGEMENT_KEY
```

## CLI flags

- `--cpa-base-url`: CPA base URL, default `http://127.0.0.1:8317`
- `--management-key`, `-k`: management bearer key
- `--concurrency`: concurrent quota workers, default `32`
- `--timeout`: HTTP timeout seconds
- `--retry-attempts`: transient retry count
- `--version`: print version/build metadata
- `--filter-plan`: filter by `plan_type`
- `--filter-provider`: filter by provider, such as `codex` or `gemini-cli`
- `--filter-status`: filter by status
- `--json`: print JSON payload
- `--plain`: plain text output
- `--summary-only`: summary only
- `--ascii-bars`: ASCII quota bars instead of Unicode bars
- `--no-progress`: disable fetch progress line

## Examples

JSON output:

```bash
./cpa-quota-inspector \
  --json \
  -k YOUR_MANAGEMENT_KEY
```

Disable progress line:

```bash
./cpa-quota-inspector \
  --no-progress \
  -k YOUR_MANAGEMENT_KEY
```

ASCII bars:

```bash
./cpa-quota-inspector \
  --ascii-bars \
  -k YOUR_MANAGEMENT_KEY
```

Only show Gemini CLI:

```bash
./cpa-quota-inspector \
  --filter-provider gemini-cli \
  -k YOUR_MANAGEMENT_KEY
```

Print version metadata:

```bash
./cpa-quota-inspector --version
```

## Sorting and summary

- Default order is provider-aware:
  - Codex: plan rank (`free`, `team`, `plus`, others) then ascending `7d` remaining
  - Gemini CLI: status, then available model information, then file name
- Terminal summary is split by provider:
  - Codex: `Accounts`, `Plans`, `Statuses`, `Free Equivalent 7d`, `Plus Equivalent 7d`
  - Gemini CLI: `Accounts`, `Plans`, `Statuses`, and per-group `Equivalent` metrics
- JSON output includes both global `summary` and `provider_summaries`

## Project structure

- `main.go`: CLI entrypoint and orchestration
- `types.go`: constants and data models
- `fetch.go`: shared management API calls, filtering, summaries
- `providers.go`: provider registry and provider-specific quota queries
- `render.go`: terminal report rendering
- `helpers.go`: shared helpers and formatting utilities

## Development

Format and test:

```bash
gofmt -w *.go
go test ./...
```

## Release

Create and push a semantic tag:

```bash
git checkout main
git pull
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

Build multi-platform artifacts with GoReleaser:

```bash
goreleaser release --clean
```

## Notes

- Code review quota is intentionally not displayed.
- Current multi-provider support is `Codex + Gemini CLI`.
