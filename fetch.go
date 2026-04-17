package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type authTask struct {
	Provider *providerDef
	Entry    authEntry
}

type runtimeState struct {
	client        *http.Client
	apiCallSem    chan struct{}
	authFilesOnce sync.Once
	authFiles     []map[string]any
	authFilesErr  error
}

func queryAllQuotas(ctx context.Context, cfg config, tasks []authTask, showProgress bool) ([]quotaReport, error) {
	if len(tasks) == 0 {
		return []quotaReport{}, nil
	}
	if cfg.Runtime == nil {
		cfg.Runtime = newRuntimeState(cfg)
	}
	client := managementHTTPClient(cfg)
	reports := make([]quotaReport, len(tasks))
	errCh := make(chan error, len(tasks))
	progressCh := make(chan string, len(tasks))
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup

	for i := range tasks {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			report, err := tasks[i].Provider.QueryReport(ctx, client, cfg, tasks[i].Entry)
			if err != nil {
				errCh <- err
				return
			}
			reports[i] = report
			progressCh <- fmt.Sprintf("%s / %s", tasks[i].Provider.SectionTitle, report.Name)
		}()
	}

	done := make(chan struct{})
	if showProgress {
		go func(total int) {
			completed := 0
			current := "-"
			for name := range progressCh {
				completed++
				current = name
				renderFetchProgress(completed, total, current)
			}
			if completed > 0 {
				fmt.Print("\r" + strings.Repeat(" ", 160) + "\r")
			}
			close(done)
		}(len(tasks))
	}

	wg.Wait()
	close(progressCh)
	if showProgress {
		<-done
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}
	return reports, nil
}

func renderFetchProgress(done, total int, current string) {
	if total <= 0 {
		return
	}
	termWidth := 120
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		termWidth = w
	}
	left := fmt.Sprintf("Querying %d/%d", done, total)
	name := truncate(current, 48)
	barArea := termWidth - displayWidth(left) - displayWidth(name) - 10
	if barArea < 10 {
		barArea = 10
	}
	pct := float64(done) * 100 / float64(total)
	filled := int((pct / 100 * float64(barArea)) + 0.5)
	if filled > barArea {
		filled = barArea
	}
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", max(0, barArea-filled)) + "]"
	fmt.Printf("\r%s %s %3.0f%% %s", left, bar, pct, name)
}

func fetchJSON(ctx context.Context, client *http.Client, cfg config, url string) (map[string]any, error) {
	return doJSONRequest(ctx, client, cfg, http.MethodGet, url, nil)
}

func postJSON(ctx context.Context, client *http.Client, cfg config, url string, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return doJSONRequest(ctx, client, cfg, http.MethodPost, url, raw)
}

func decodeResponse(resp *http.Response) (map[string]any, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("management API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func newRuntimeState(cfg config) *runtimeState {
	limit := cfg.MgmtConcurrency
	if limit < 1 {
		limit = defaultMgmtConcurrency
	}
	return &runtimeState{
		client:     newManagementHTTPClient(cfg.Timeout, max(cfg.Concurrency*2, limit*2)),
		apiCallSem: make(chan struct{}, limit),
	}
}

func newManagementHTTPClient(timeout time.Duration, maxConnsPerHost int) *http.Client {
	if maxConnsPerHost < 32 {
		maxConnsPerHost = 32
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          max(128, maxConnsPerHost*2),
		MaxIdleConnsPerHost:   max(64, maxConnsPerHost),
		MaxConnsPerHost:       maxConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func managementHTTPClient(cfg config) *http.Client {
	if cfg.Runtime != nil && cfg.Runtime.client != nil {
		return cfg.Runtime.client
	}
	return newManagementHTTPClient(cfg.Timeout, max(cfg.Concurrency*2, defaultMgmtConcurrency*2))
}

func doJSONRequest(ctx context.Context, client *http.Client, cfg config, method, url string, body []byte) (map[string]any, error) {
	if client == nil {
		client = managementHTTPClient(cfg)
	}
	isAPICall := isManagementAPICallURL(url)
	attempts := 1
	if isAPICall {
		attempts = cfg.RetryAttempts
		if attempts < 1 {
			attempts = 1
		}
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := acquireAPICallSlot(ctx, cfg, isAPICall); err != nil {
			return nil, err
		}
		respPayload, err := func() (map[string]any, error) {
			defer releaseAPICallSlot(cfg, isAPICall)
			var reader io.Reader
			if len(body) > 0 {
				reader = bytes.NewReader(body)
			}
			req, err := http.NewRequestWithContext(ctx, method, url, reader)
			if err != nil {
				return nil, err
			}
			if cfg.ManagementKey != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.ManagementKey)
			}
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			return decodeResponse(resp)
		}()
		if err == nil {
			return respPayload, nil
		}
		lastErr = err
		if !isAPICall || attempt == attempts || !shouldRetryError(err.Error()) {
			break
		}
		if waitErr := sleepBeforeRetry(ctx, attempt); waitErr != nil {
			return nil, waitErr
		}
	}
	return nil, lastErr
}

func isManagementAPICallURL(url string) bool {
	return strings.Contains(url, "/v0/management/api-call")
}

func acquireAPICallSlot(ctx context.Context, cfg config, enabled bool) error {
	if !enabled || cfg.Runtime == nil || cfg.Runtime.apiCallSem == nil {
		return nil
	}
	select {
	case cfg.Runtime.apiCallSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseAPICallSlot(cfg config, enabled bool) {
	if !enabled || cfg.Runtime == nil || cfg.Runtime.apiCallSem == nil {
		return
	}
	select {
	case <-cfg.Runtime.apiCallSem:
	default:
	}
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	if attempt < 1 {
		attempt = 1
	}
	delay := 200 * time.Millisecond
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= 2*time.Second {
			delay = 2 * time.Second
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func loadAuthFiles(ctx context.Context, cfg config) ([]map[string]any, error) {
	if cfg.Runtime == nil {
		cfg.Runtime = newRuntimeState(cfg)
	}
	cfg.Runtime.authFilesOnce.Do(func() {
		payload, err := fetchJSON(ctx, managementHTTPClient(cfg), cfg, cfg.BaseURL+"/v0/management/auth-files")
		if err != nil {
			cfg.Runtime.authFilesErr = err
			return
		}
		files, ok := payload["files"].([]any)
		if !ok {
			cfg.Runtime.authFilesErr = fmt.Errorf("unexpected auth-files payload from CPA management API")
			return
		}
		out := make([]map[string]any, 0, len(files))
		for _, item := range files {
			entry, ok := item.(map[string]any)
			if ok {
				out = append(out, entry)
			}
		}
		cfg.Runtime.authFiles = out
	})
	if cfg.Runtime.authFilesErr != nil {
		return nil, cfg.Runtime.authFilesErr
	}
	return cfg.Runtime.authFiles, nil
}

func parseBody(body any) (map[string]any, error) {
	switch v := body.(type) {
	case map[string]any:
		return v, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, errors.New("empty")
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, errors.New("invalid")
	}
}

func parseJWTLike(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return nil
		}
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) == nil {
			return out
		}
		parts := strings.Split(raw, ".")
		if len(parts) < 2 {
			return nil
		}
		payload, err := decodeBase64URL(parts[1])
		if err != nil {
			return nil
		}
		if json.Unmarshal(payload, &out) != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

func decodeBase64URL(v string) ([]byte, error) {
	switch len(v) % 4 {
	case 2:
		v += "=="
	case 3:
		v += "="
	}
	return base64.URLEncoding.DecodeString(v)
}

func parseCodexWindows(payload map[string]any) []quotaWindow {
	rateLimit, _ := firstValue(payload["rate_limit"], payload["rateLimit"]).(map[string]any)
	fiveHour, weekly := findQuotaWindows(rateLimit)
	mainLimitReached := anyFromMap(rateLimit, "limit_reached", "limitReached")
	mainAllowed := anyFromMap(rateLimit, "allowed")
	var windows []quotaWindow
	if window := buildWindow("code-5h", "5h", fiveHour, mainLimitReached, mainAllowed); window != nil {
		windows = append(windows, *window)
	}
	if window := buildWindow("code-7d", "7d", weekly, mainLimitReached, mainAllowed); window != nil {
		windows = append(windows, *window)
	}
	return windows
}

func parseAdditionalWindows(payload map[string]any) []quotaWindow {
	raw, ok := firstValue(payload["additional_rate_limits"], payload["additionalRateLimits"]).([]any)
	if !ok {
		return nil
	}
	var windows []quotaWindow
	for i, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rateLimit, ok := firstValue(entry["rate_limit"], entry["rateLimit"]).(map[string]any)
		if !ok {
			continue
		}
		name := cleanString(firstValue(entry["limit_name"], entry["limitName"], entry["metered_feature"], entry["meteredFeature"]))
		if name == "" {
			name = fmt.Sprintf("additional-%d", i+1)
		}
		primary, _ := firstValue(rateLimit["primary_window"], rateLimit["primaryWindow"]).(map[string]any)
		secondary, _ := firstValue(rateLimit["secondary_window"], rateLimit["secondaryWindow"]).(map[string]any)
		if window := buildWindow(name+"-primary", name+" 5h", primary, anyFromMap(rateLimit, "limit_reached", "limitReached"), anyFromMap(rateLimit, "allowed")); window != nil {
			windows = append(windows, *window)
		}
		if window := buildWindow(name+"-secondary", name+" 7d", secondary, anyFromMap(rateLimit, "limit_reached", "limitReached"), anyFromMap(rateLimit, "allowed")); window != nil {
			windows = append(windows, *window)
		}
	}
	return windows
}

func findQuotaWindows(rateLimit map[string]any) (map[string]any, map[string]any) {
	if rateLimit == nil {
		return nil, nil
	}
	primary, _ := firstValue(rateLimit["primary_window"], rateLimit["primaryWindow"]).(map[string]any)
	secondary, _ := firstValue(rateLimit["secondary_window"], rateLimit["secondaryWindow"]).(map[string]any)
	candidates := []map[string]any{primary, secondary}
	var fiveHour, weekly map[string]any
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		duration := numberFromAny(firstValue(candidate["limit_window_seconds"], candidate["limitWindowSeconds"]))
		if duration == window5HSeconds && fiveHour == nil {
			fiveHour = candidate
		}
		if duration == window7DSeconds && weekly == nil {
			weekly = candidate
		}
	}
	if fiveHour == nil && primary != nil {
		fiveHour = primary
	}
	if weekly == nil && secondary != nil {
		weekly = secondary
	}
	return fiveHour, weekly
}

func buildWindow(id, label string, window map[string]any, limitReached, allowed any) *quotaWindow {
	if window == nil {
		return nil
	}
	usedPercent := deduceUsedPercent(window, limitReached, allowed)
	var remaining *float64
	if usedPercent != nil {
		v := clampFloat(100.0-*usedPercent, 0, 100)
		remaining = &v
	}
	exhausted := usedPercent != nil && *usedPercent >= 100
	return &quotaWindow{
		ID:               id,
		Label:            label,
		UsedPercent:      usedPercent,
		RemainingPercent: remaining,
		ResetLabel:       formatResetLabel(window),
		Exhausted:        exhausted,
	}
}

func deduceUsedPercent(window map[string]any, limitReached, allowed any) *float64 {
	if used := numberPtr(firstValue(window["used_percent"], window["usedPercent"])); used != nil {
		v := clampFloat(*used, 0, 100)
		return &v
	}
	exhaustedHint := boolFromAny(limitReached) || isFalse(allowed)
	if exhaustedHint && formatResetLabel(window) != "-" {
		v := 100.0
		return &v
	}
	return nil
}

func formatResetLabel(window map[string]any) string {
	if ts := numberFromAny(firstValue(window["reset_at"], window["resetAt"])); ts > 0 {
		return time.Unix(int64(ts), 0).Local().Format("01-02 15:04")
	}
	if secs := numberFromAny(firstValue(window["reset_after_seconds"], window["resetAfterSeconds"])); secs > 0 {
		return time.Now().Add(time.Duration(secs) * time.Second).Local().Format("01-02 15:04")
	}
	return "-"
}

func shouldRetryError(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}
	markers := []string{
		"request failed",
		"timed out",
		"timeout",
		"temporarily unavailable",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
		"connection reset",
		"remote end closed connection",
		"operation not permitted",
	}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func filterReports(reports []quotaReport, plan, status string) []quotaReport {
	var out []quotaReport
	plan = strings.ToLower(strings.TrimSpace(plan))
	status = strings.ToLower(strings.TrimSpace(status))
	for _, report := range reports {
		if plan != "" && strings.ToLower(report.PlanType) != plan {
			continue
		}
		if status != "" && strings.ToLower(report.Status) != status {
			continue
		}
		out = append(out, report)
	}
	return out
}

func filterReportsByProvider(reports []quotaReport, provider string) []quotaReport {
	want := strings.ToLower(strings.TrimSpace(provider))
	if want == "" {
		return reports
	}
	var out []quotaReport
	for _, report := range reports {
		if strings.EqualFold(report.Provider, want) {
			out = append(out, report)
		}
	}
	return out
}

func summarize(reports []quotaReport) summary {
	sum := summary{
		Accounts:          len(reports),
		ProviderCounts:    map[string]int{},
		StatusCounts:      map[string]int{},
		PlanCounts:        map[string]int{},
		GeminiEquivalents: map[string]float64{},
		AntigravityEquivs: map[string]float64{},
	}
	for _, report := range reports {
		sum.ProviderCounts[report.Provider]++
		sum.StatusCounts[report.Status]++
		plan := report.PlanType
		if plan == "" {
			plan = "unknown"
		}
		sum.PlanCounts[plan]++
		if report.Status == "exhausted" {
			sum.ExhaustedAccounts++
			sum.ExhaustedNames = append(sum.ExhaustedNames, report.Name)
		}
		if report.Status == "low" {
			sum.LowAccounts++
			sum.LowNames = append(sum.LowNames, report.Name)
		}
		if report.Status == "error" || report.Status == "missing" {
			sum.ErrorAccounts++
			sum.ErrorNames = append(sum.ErrorNames, report.Name)
		}
		sum.AdditionalWindows += len(report.AdditionalWindows)

		if report.Provider != "codex" {
			switch report.Provider {
			case "gemini-cli":
				for _, window := range report.Windows {
					if window.RemainingPercent == nil {
						continue
					}
					sum.GeminiEquivalents[window.Label] += *window.RemainingPercent
				}
			case "antigravity":
				for _, window := range report.Windows {
					if window.RemainingPercent == nil {
						continue
					}
					sum.AntigravityEquivs[window.Label] += *window.RemainingPercent
				}
			}
			continue
		}
		window7d := findWindow(report.Windows, "code-7d")
		if window7d == nil || window7d.RemainingPercent == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(report.PlanType)) {
		case "free":
			sum.FreeEquivalent7D += *window7d.RemainingPercent
		case "plus":
			sum.PlusEquivalent7D += *window7d.RemainingPercent
		}
	}
	return sum
}

func buildSections(reports []quotaReport) []reportSection {
	grouped := map[string][]quotaReport{}
	for _, report := range reports {
		grouped[report.Provider] = append(grouped[report.Provider], report)
	}
	var sections []reportSection
	for _, provider := range providerDefinitions() {
		items := grouped[provider.ID]
		if len(items) == 0 {
			continue
		}
		sections = append(sections, reportSection{
			Provider: provider.ID,
			Title:    provider.SectionTitle,
			Reports:  items,
		})
		delete(grouped, provider.ID)
	}
	if len(grouped) == 0 {
		return sections
	}
	leftovers := make([]string, 0, len(grouped))
	for provider := range grouped {
		leftovers = append(leftovers, provider)
	}
	sort.Strings(leftovers)
	for _, provider := range leftovers {
		sections = append(sections, reportSection{
			Provider: provider,
			Title:    titleProvider(provider),
			Reports:  grouped[provider],
		})
	}
	return sections
}
