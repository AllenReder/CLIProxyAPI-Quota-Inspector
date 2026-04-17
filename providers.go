package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

func providerDefinitions() []*providerDef {
	return []*providerDef{
		{
			ID:           "codex",
			SectionTitle: "Codex",
			LoadAuths: func(ctx context.Context, cfg config) ([]authEntry, error) {
				return loadProviderAuths(ctx, cfg, "codex")
			},
			QueryReport: queryCodexQuota,
		},
		{
			ID:           "gemini-cli",
			SectionTitle: "Gemini CLI",
			LoadAuths: func(ctx context.Context, cfg config) ([]authEntry, error) {
				return loadProviderAuths(ctx, cfg, "gemini-cli")
			},
			QueryReport: queryGeminiQuota,
		},
		{
			ID:           "antigravity",
			SectionTitle: "Antigravity",
			LoadAuths: func(ctx context.Context, cfg config) ([]authEntry, error) {
				return loadProviderAuths(ctx, cfg, "antigravity")
			},
			QueryReport: queryAntigravityQuota,
		},
	}
}

var (
	geminiLoadMetadata = map[string]string{
		"ideType":    "IDE_UNSPECIFIED",
		"platform":   "PLATFORM_UNSPECIFIED",
		"pluginType": "GEMINI",
	}
	antigravityLoadMetadata = map[string]string{
		"ideType":    "ANTIGRAVITY",
		"platform":   "PLATFORM_UNSPECIFIED",
		"pluginType": "GEMINI",
	}
	antigravityModelGroups = []namedModelGroup{
		{ID: "claude-gpt", Label: "Claude/GPT", ModelIDs: []string{"claude-sonnet-4-6", "claude-opus-4-6-thinking", "gpt-oss-120b-medium"}},
		{ID: "gemini-3-1-pro-series", Label: "Gemini 3.1 Pro Series", ModelIDs: []string{"gemini-3.1-pro-high", "gemini-3.1-pro-low"}},
		{ID: "gemini-3-pro", Label: "Gemini 3 Pro", ModelIDs: []string{"gemini-3-pro-high", "gemini-3-pro-low"}},
		{ID: "gemini-2-5-flash", Label: "Gemini 2.5 Flash", ModelIDs: []string{"gemini-2.5-flash", "gemini-2.5-flash-thinking"}},
		{ID: "gemini-2-5-flash-lite", Label: "Gemini 2.5 Flash Lite", ModelIDs: []string{"gemini-2.5-flash-lite"}},
		{ID: "gemini-2-5-cu", Label: "Gemini 2.5 CU", ModelIDs: []string{"rev19-uic3-1p"}},
		{ID: "gemini-3-flash", Label: "Gemini 3 Flash", ModelIDs: []string{"gemini-3-flash"}},
		{ID: "gemini-image", Label: "gemini-3.1-flash-image", ModelIDs: []string{"gemini-3.1-flash-image"}, LabelFromModel: true},
	}
)

type namedModelGroup struct {
	ID             string
	Label          string
	ModelIDs       []string
	LabelFromModel bool
}

func loadProviderAuths(ctx context.Context, cfg config, provider string) ([]authEntry, error) {
	files, err := loadAuthFiles(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]authEntry, 0, len(files))
	for _, entry := range files {
		current := normalizePlan(firstValue(entry["provider"], entry["type"]))
		if current != provider {
			continue
		}
		out = append(out, authEntry{raw: entry})
	}
	return out, nil
}

func queryCodexQuota(ctx context.Context, client *http.Client, cfg config, entry authEntry) (quotaReport, error) {
	report := quotaReport{
		Provider:  "codex",
		Name:      cleanString(firstValue(entry.raw["name"], entry.raw["id"], "unknown")),
		AuthIndex: cleanString(firstValue(entry.raw["auth_index"], entry.raw["authIndex"])),
		AccountID: parseAccountID(entry.raw),
		PlanType:  parsePlanType(entry.raw),
		Status:    "unknown",
	}
	if report.Name == "" {
		report.Name = "unknown"
	}
	if report.AuthIndex == "" {
		report.Error = "missing auth_index"
		report.Status = deriveCodexStatus(report)
		return report, nil
	}
	if report.AccountID == "" {
		report.Error = "missing chatgpt_account_id"
		report.Status = deriveCodexStatus(report)
		return report, nil
	}

	payload := map[string]any{
		"auth_index": report.AuthIndex,
		"method":     "GET",
		"url":        whamUsageURL,
		"header": mergeMaps(
			whamHeaders,
			map[string]string{"Chatgpt-Account-Id": report.AccountID},
		),
	}

	var lastErr string
	for attempt := 1; attempt <= cfg.RetryAttempts; attempt++ {
		response, err := postJSON(ctx, client, cfg, cfg.BaseURL+"/v0/management/api-call", payload)
		if err != nil {
			lastErr = err.Error()
			if attempt == cfg.RetryAttempts || !shouldRetryError(lastErr) {
				break
			}
			if waitErr := sleepBeforeRetry(ctx, attempt); waitErr != nil {
				lastErr = waitErr.Error()
				break
			}
			continue
		}
		statusCode := intFromAny(firstValue(response["status_code"], response["statusCode"]))
		bodyValue := response["body"]
		parsedBody, parseErr := parseBody(bodyValue)
		if statusCode < 200 || statusCode >= 300 {
			lastErr = bodyString(bodyValue)
			if lastErr == "" {
				lastErr = fmt.Sprintf("HTTP %d", statusCode)
			}
			if attempt == cfg.RetryAttempts || !shouldRetryError(lastErr) {
				break
			}
			if waitErr := sleepBeforeRetry(ctx, attempt); waitErr != nil {
				lastErr = waitErr.Error()
				break
			}
			continue
		}
		if parseErr != nil {
			lastErr = "empty or invalid quota payload"
			if attempt == cfg.RetryAttempts {
				break
			}
			if waitErr := sleepBeforeRetry(ctx, attempt); waitErr != nil {
				lastErr = waitErr.Error()
				break
			}
			continue
		}

		report.PlanType = firstNonEmpty(normalizePlan(firstValue(parsedBody["plan_type"], parsedBody["planType"])), report.PlanType)
		report.Windows = parseCodexWindows(parsedBody)
		report.AdditionalWindows = parseAdditionalWindows(parsedBody)
		report.Error = ""
		report.Status = deriveCodexStatus(report)
		return report, nil
	}

	report.Error = lastErr
	report.Status = deriveCodexStatus(report)
	return report, nil
}

func queryGeminiQuota(ctx context.Context, client *http.Client, cfg config, entry authEntry) (quotaReport, error) {
	return queryGoogleQuotaProvider(ctx, client, cfg, entry, googleQuotaProviderConfig{
		ProviderID:         "gemini-cli",
		ParseProjectID:     parseGeminiProjectID,
		LoadMetadata:       geminiLoadMetadata,
		IncludeDuetProject: true,
	})
}

func queryAntigravityQuota(ctx context.Context, client *http.Client, cfg config, entry authEntry) (quotaReport, error) {
	report := quotaReport{
		Provider:   "antigravity",
		Name:       cleanString(firstValue(entry.raw["name"], entry.raw["id"], "unknown")),
		AuthIndex:  cleanString(firstValue(entry.raw["auth_index"], entry.raw["authIndex"])),
		PlanType:   "unknown",
		Status:     "unknown",
		Cells:      map[string]quotaCell{},
		MetaFields: map[string]string{},
	}
	if report.Name == "" {
		report.Name = "unknown"
	}
	if report.AuthIndex == "" {
		report.Error = "missing auth_index"
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}

	projectID := parseAntigravityProjectID(entry.raw)
	if projectID == "" {
		loadBody, loadErr := callGoogleLoadCodeAssist(ctx, client, cfg, report.AuthIndex, antigravityLoadMetadata, "")
		if loadErr == nil {
			populateGoogleAssistReport(&report, loadBody)
			projectID = cleanString(firstValue(loadBody["cloudaicompanionProject"], nested(loadBody, "cloudaicompanionProject", "id")))
		}
	}
	if projectID == "" {
		report.Error = "missing project_id"
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}
	report.MetaFields["project"] = projectID

	modelsBody, err := callAntigravityFetchModels(ctx, client, cfg, report.AuthIndex, projectID)
	if err != nil {
		report.Error = err.Error()
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}
	report.Windows = parseAntigravityQuotaWindows(modelsBody)
	supplementGoogleTier(ctx, client, cfg, &report, projectID, antigravityLoadMetadata, false)
	report.Error = ""
	report.Status = deriveGoogleQuotaStatus(report)
	return report, nil
}

type googleQuotaProviderConfig struct {
	ProviderID         string
	ParseProjectID     func(map[string]any) string
	LoadMetadata       map[string]string
	IncludeDuetProject bool
}

func queryGoogleQuotaProvider(ctx context.Context, client *http.Client, cfg config, entry authEntry, providerCfg googleQuotaProviderConfig) (quotaReport, error) {
	report := quotaReport{
		Provider:   providerCfg.ProviderID,
		Name:       cleanString(firstValue(entry.raw["name"], entry.raw["id"], "unknown")),
		AuthIndex:  cleanString(firstValue(entry.raw["auth_index"], entry.raw["authIndex"])),
		PlanType:   "unknown",
		Status:     "unknown",
		Cells:      map[string]quotaCell{},
		MetaFields: map[string]string{},
	}
	if report.Name == "" {
		report.Name = "unknown"
	}
	if report.AuthIndex == "" {
		report.Error = "missing auth_index"
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}

	projectID := ""
	if providerCfg.ParseProjectID != nil {
		projectID = providerCfg.ParseProjectID(entry.raw)
	}
	if projectID == "" {
		loadBody, loadErr := callGoogleLoadCodeAssist(ctx, client, cfg, report.AuthIndex, providerCfg.LoadMetadata, "")
		if loadErr == nil {
			populateGoogleAssistReport(&report, loadBody)
			projectID = cleanString(firstValue(loadBody["cloudaicompanionProject"], nested(loadBody, "cloudaicompanionProject", "id")))
		}
	}
	if projectID == "" {
		report.Error = "missing project_id"
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}
	report.MetaFields["project"] = projectID

	var lastErr string
	for attempt := 1; attempt <= cfg.RetryAttempts; attempt++ {
		parsedBody, err := callGoogleRetrieveQuota(ctx, client, cfg, report.AuthIndex, projectID, providerCfg.LoadMetadata)
		if err != nil {
			lastErr = err.Error()
			if attempt == cfg.RetryAttempts || !shouldRetryError(lastErr) {
				break
			}
			if waitErr := sleepBeforeRetry(ctx, attempt); waitErr != nil {
				lastErr = waitErr.Error()
				break
			}
			continue
		}
		report.Windows = parseGeminiQuotaWindows(parsedBody)
		supplementGoogleTier(ctx, client, cfg, &report, projectID, providerCfg.LoadMetadata, providerCfg.IncludeDuetProject)
		report.Error = ""
		report.Status = deriveGoogleQuotaStatus(report)
		return report, nil
	}

	report.Error = lastErr
	report.Status = deriveGoogleQuotaStatus(report)
	return report, nil
}

type geminiGroupedBucket struct {
	ID               string
	Label            string
	RemainingPercent *float64
	ResetLabel       string
}

type geminiQuotaBucket struct {
	ModelID          string
	RemainingPercent *float64
	ResetLabel       string
}

func parseGeminiQuotaWindows(payload map[string]any) []quotaWindow {
	rawBuckets, _ := payload["buckets"].([]any)
	if len(rawBuckets) == 0 {
		return nil
	}
	type bucket struct {
		ModelID          string
		RemainingPercent *float64
		ResetLabel       string
	}
	var buckets []geminiQuotaBucket
	for _, item := range rawBuckets {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modelID := cleanGeminiModelID(cleanString(firstValue(entry["modelId"], entry["model_id"])))
		if modelID == "" {
			continue
		}
		var remaining *float64
		if fraction := numberPtr(firstValue(entry["remainingFraction"], entry["remaining_fraction"], entry["remaining"])); fraction != nil {
			v := clampFloat(*fraction*100, 0, 100)
			remaining = &v
		}
		if remaining == nil {
			if amount := numberPtr(firstValue(entry["remainingAmount"], entry["remaining_amount"])); amount != nil {
				if *amount <= 0 {
					v := 0.0
					remaining = &v
				}
			}
		}
		resetLabel := formatGeminiResetLabel(cleanString(firstValue(entry["resetTime"], entry["reset_time"])))
		if remaining == nil && resetLabel != "-" {
			v := 0.0
			remaining = &v
		}
		buckets = append(buckets, geminiQuotaBucket{
			ModelID:          modelID,
			RemainingPercent: remaining,
			ResetLabel:       resetLabel,
		})
	}
	return groupGeminiBuckets(buckets)
}

func groupGeminiBuckets(buckets []geminiQuotaBucket) []quotaWindow {
	type groupDef struct {
		ID               string
		Label            string
		PreferredModelID string
		ModelIDs         []string
	}
	groupDefs := []groupDef{
		{
			ID:               "gemini-flash-lite-series",
			Label:            "Gemini Flash Lite Series",
			PreferredModelID: "gemini-2.5-flash-lite",
			ModelIDs:         []string{"gemini-2.5-flash-lite"},
		},
		{
			ID:               "gemini-flash-series",
			Label:            "Gemini Flash Series",
			PreferredModelID: "gemini-3-flash-preview",
			ModelIDs:         []string{"gemini-3-flash-preview", "gemini-2.5-flash"},
		},
		{
			ID:               "gemini-pro-series",
			Label:            "Gemini Pro Series",
			PreferredModelID: "gemini-3.1-pro-preview",
			ModelIDs:         []string{"gemini-3.1-pro-preview", "gemini-3-pro-preview", "gemini-2.5-pro"},
		},
	}
	groupByModel := map[string]groupDef{}
	groupOrder := map[string]int{}
	for i, def := range groupDefs {
		groupOrder[def.ID] = i
		for _, modelID := range def.ModelIDs {
			groupByModel[modelID] = def
		}
	}

	type agg struct {
		def             groupDef
		remaining       *float64
		reset           string
		preferredBucket *geminiQuotaBucket
		models          []string
	}
	groups := map[string]*agg{}
	var extras []quotaWindow

	for i := range buckets {
		bucket := buckets[i]
		if def, ok := groupByModel[bucket.ModelID]; ok {
			current := groups[def.ID]
			if current == nil {
				current = &agg{def: def}
				groups[def.ID] = current
			}
			current.models = append(current.models, bucket.ModelID)
			if current.remaining == nil || (bucket.RemainingPercent != nil && *bucket.RemainingPercent < *current.remaining) {
				current.remaining = bucket.RemainingPercent
			}
			if current.reset == "-" || current.reset == "" {
				current.reset = bucket.ResetLabel
			}
			if bucket.ModelID == def.PreferredModelID {
				current.preferredBucket = &bucket
			}
			continue
		}
		extras = append(extras, quotaWindow{
			ID:               bucket.ModelID,
			Label:            bucket.ModelID,
			RemainingPercent: bucket.RemainingPercent,
			ResetLabel:       bucket.ResetLabel,
		})
	}

	var windows []quotaWindow
	for _, def := range groupDefs {
		current := groups[def.ID]
		if current == nil {
			continue
		}
		remaining := current.remaining
		reset := current.reset
		if current.preferredBucket != nil {
			if current.preferredBucket.RemainingPercent != nil {
				remaining = current.preferredBucket.RemainingPercent
			}
			if current.preferredBucket.ResetLabel != "" && current.preferredBucket.ResetLabel != "-" {
				reset = current.preferredBucket.ResetLabel
			}
		}
		windows = append(windows, quotaWindow{
			ID:               def.ID,
			Label:            def.Label,
			RemainingPercent: remaining,
			ResetLabel:       defaultString(reset, "-"),
		})
	}
	sort.SliceStable(extras, func(i, j int) bool {
		return strings.ToLower(extras[i].Label) < strings.ToLower(extras[j].Label)
	})
	windows = append(windows, extras...)
	return windows
}

func cleanGeminiModelID(v string) string {
	raw := strings.TrimSpace(v)
	raw = strings.TrimSuffix(raw, "_vertex")
	return raw
}

func formatGeminiResetLabel(v string) string {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		if parsed, err2 := time.Parse("2006-01-02T15:04:05.999999Z", raw); err2 == nil {
			t = parsed
		} else {
			return "-"
		}
	}
	return t.Local().Format("01-02 15:04")
}

func callGoogleRetrieveQuota(ctx context.Context, client *http.Client, cfg config, authIndex, projectID string, metadata map[string]string) (map[string]any, error) {
	bodyRaw, err := json.Marshal(map[string]any{"project": projectID})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"auth_index": authIndex,
		"method":     "POST",
		"url":        geminiRetrieveQuotaURL,
		"header":     buildGoogleAPIHeaders(metadata),
		"data":       string(bodyRaw),
	}
	response, err := postJSON(ctx, client, cfg, cfg.BaseURL+"/v0/management/api-call", payload)
	if err != nil {
		return nil, err
	}
	statusCode := intFromAny(firstValue(response["status_code"], response["statusCode"]))
	bodyValue := response["body"]
	if statusCode < 200 || statusCode >= 300 {
		lastErr := bodyString(bodyValue)
		if lastErr == "" {
			lastErr = fmt.Sprintf("HTTP %d", statusCode)
		}
		return nil, fmt.Errorf("%s", lastErr)
	}
	parsedBody, parseErr := parseBody(bodyValue)
	if parseErr != nil {
		return nil, fmt.Errorf("empty or invalid retrieveUserQuota payload")
	}
	return parsedBody, nil
}

func callGoogleLoadCodeAssist(ctx context.Context, client *http.Client, cfg config, authIndex string, metadata map[string]string, projectID string) (map[string]any, error) {
	requestBody := map[string]any{
		"metadata": cloneStringMap(metadata),
	}
	if strings.TrimSpace(projectID) != "" {
		requestBody["cloudaicompanionProject"] = projectID
	}
	bodyRaw, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"auth_index": authIndex,
		"method":     "POST",
		"url":        geminiLoadCodeAssist,
		"header":     buildGoogleAPIHeaders(metadata),
		"data":       string(bodyRaw),
	}
	response, err := postJSON(ctx, client, cfg, cfg.BaseURL+"/v0/management/api-call", payload)
	if err != nil {
		return nil, err
	}
	statusCode := intFromAny(firstValue(response["status_code"], response["statusCode"]))
	bodyValue := response["body"]
	if statusCode < 200 || statusCode >= 300 {
		lastErr := bodyString(bodyValue)
		if lastErr == "" {
			lastErr = fmt.Sprintf("HTTP %d", statusCode)
		}
		return nil, fmt.Errorf("%s", lastErr)
	}
	parsedBody, parseErr := parseBody(bodyValue)
	if parseErr != nil {
		return nil, fmt.Errorf("empty or invalid loadCodeAssist payload")
	}
	return parsedBody, nil
}

func callAntigravityFetchModels(ctx context.Context, client *http.Client, cfg config, authIndex, projectID string) (map[string]any, error) {
	bodyRaw, err := json.Marshal(map[string]any{"project": projectID})
	if err != nil {
		return nil, err
	}
	basePayload := map[string]any{
		"auth_index": authIndex,
		"method":     "POST",
		"header": map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Content-Type":  "application/json",
			"User-Agent":    "antigravity/1.11.5 windows/amd64",
		},
		"data": string(bodyRaw),
	}
	var lastErr string
	for _, endpoint := range []string{antigravityModelsURL, antigravityDailyURL, antigravitySandboxURL} {
		payload := map[string]any{}
		for k, v := range basePayload {
			payload[k] = v
		}
		payload["url"] = endpoint
		response, err := postJSON(ctx, client, cfg, cfg.BaseURL+"/v0/management/api-call", payload)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		statusCode := intFromAny(firstValue(response["status_code"], response["statusCode"]))
		bodyValue := response["body"]
		if statusCode < 200 || statusCode >= 300 {
			lastErr = bodyString(bodyValue)
			if lastErr == "" {
				lastErr = fmt.Sprintf("HTTP %d", statusCode)
			}
			continue
		}
		parsedBody, parseErr := parseBody(bodyValue)
		if parseErr != nil {
			lastErr = "empty or invalid fetchAvailableModels payload"
			continue
		}
		return parsedBody, nil
	}
	if lastErr == "" {
		lastErr = "fetchAvailableModels failed"
	}
	return nil, fmt.Errorf("%s", lastErr)
}

func supplementGoogleTier(ctx context.Context, client *http.Client, cfg config, report *quotaReport, projectID string, metadata map[string]string, includeDuetProject bool) {
	loadMetadata := cloneStringMap(metadata)
	if includeDuetProject {
		loadMetadata["duetProject"] = projectID
	}
	parsedBody, err := callGoogleLoadCodeAssist(ctx, client, cfg, report.AuthIndex, loadMetadata, projectID)
	if err != nil {
		return
	}
	populateGoogleAssistReport(report, parsedBody)
}

func buildGoogleAPIHeaders(metadata map[string]string) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
	}
	if len(metadata) == 0 {
		return headers
	}
	headers["User-Agent"] = "google-api-nodejs-client/9.15.1"
	headers["X-Goog-Api-Client"] = "google-cloud-sdk vscode_cloudshelleditor/0.1"
	if body, err := json.Marshal(metadata); err == nil {
		headers["Client-Metadata"] = string(body)
	}
	return headers
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeMaps(base, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func parseAccountID(entry map[string]any) string {
	candidates := []any{
		entry["id_token"],
		nested(entry, "metadata", "id_token"),
		nested(entry, "attributes", "id_token"),
	}
	for _, candidate := range candidates {
		payload := parseJWTLike(candidate)
		if payload == nil {
			continue
		}
		if accountID := cleanString(payload["chatgpt_account_id"]); accountID != "" {
			return accountID
		}
		if authInfo, ok := payload["https://api.openai.com/auth"].(map[string]any); ok {
			if accountID := cleanString(authInfo["chatgpt_account_id"]); accountID != "" {
				return accountID
			}
		}
	}
	return ""
}

func parsePlanType(entry map[string]any) string {
	candidates := []any{
		entry["plan_type"],
		entry["planType"],
		nested(entry, "metadata", "plan_type"),
		nested(entry, "metadata", "planType"),
		nested(entry, "attributes", "plan_type"),
		nested(entry, "attributes", "planType"),
	}
	for _, candidate := range candidates {
		if plan := normalizePlan(candidate); plan != "" {
			return plan
		}
	}
	return ""
}

func parseGeminiProjectID(entry map[string]any) string {
	for _, candidate := range []string{
		cleanString(entry["project_id"]),
		cleanString(nested(entry, "metadata", "project_id")),
		extractParenValue(cleanString(entry["account"])),
		extractFileProject(cleanString(firstValue(entry["name"], entry["id"]))),
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func parseAntigravityProjectID(entry map[string]any) string {
	for _, candidate := range []string{
		cleanString(entry["project_id"]),
		cleanString(nested(entry, "metadata", "project_id")),
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func parseAntigravityQuotaWindows(payload map[string]any) []quotaWindow {
	modelsRaw, ok := payload["models"].(map[string]any)
	if !ok || len(modelsRaw) == 0 {
		return nil
	}
	type modelQuota struct {
		ModelID          string
		DisplayName      string
		RemainingPercent *float64
		ResetLabel       string
	}
	models := map[string]modelQuota{}
	for modelID, raw := range modelsRaw {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		quotaInfo, _ := firstValue(entry["quotaInfo"], entry["quota_info"]).(map[string]any)
		remaining := numberPtr(firstValue(
			nested(entry, "quotaInfo", "remainingFraction"),
			nested(entry, "quota_info", "remaining_fraction"),
			firstValue(quotaInfo["remainingFraction"], quotaInfo["remaining_fraction"], quotaInfo["remaining"]),
		))
		if remaining != nil {
			v := clampFloat(*remaining*100, 0, 100)
			remaining = &v
		}
		resetLabel := formatGeminiResetLabel(cleanString(firstValue(
			nested(entry, "quotaInfo", "resetTime"),
			nested(entry, "quota_info", "reset_time"),
			firstValue(quotaInfo["resetTime"], quotaInfo["reset_time"]),
		)))
		if remaining == nil && resetLabel != "-" {
			v := 0.0
			remaining = &v
		}
		models[modelID] = modelQuota{
			ModelID:          modelID,
			DisplayName:      cleanString(entry["displayName"]),
			RemainingPercent: remaining,
			ResetLabel:       resetLabel,
		}
	}

	var windows []quotaWindow
	addGroup := func(group namedModelGroup, resetOverride string) *quotaWindow {
		var matches []modelQuota
		for _, modelID := range group.ModelIDs {
			if model, ok := models[modelID]; ok && model.RemainingPercent != nil {
				matches = append(matches, model)
			}
		}
		if len(matches) == 0 {
			return nil
		}
		minRemaining := *matches[0].RemainingPercent
		resetLabel := matches[0].ResetLabel
		displayName := matches[0].DisplayName
		for _, item := range matches[1:] {
			if item.RemainingPercent != nil && *item.RemainingPercent < minRemaining {
				minRemaining = *item.RemainingPercent
			}
			if (resetLabel == "" || resetLabel == "-") && item.ResetLabel != "" {
				resetLabel = item.ResetLabel
			}
			if displayName == "" {
				displayName = item.DisplayName
			}
		}
		if resetOverride != "" && resetOverride != "-" {
			resetLabel = resetOverride
		}
		label := group.Label
		if group.LabelFromModel && displayName != "" {
			label = displayName
		}
		window := quotaWindow{
			ID:               group.ID,
			Label:            label,
			RemainingPercent: &minRemaining,
			ResetLabel:       defaultString(resetLabel, "-"),
		}
		windows = append(windows, window)
		return &window
	}

	addGroup(antigravityModelGroups[0], "")
	pro31 := addGroup(antigravityModelGroups[1], "")
	pro3 := addGroup(antigravityModelGroups[2], "")
	resetOverride := ""
	if pro31 != nil && pro31.ResetLabel != "" && pro31.ResetLabel != "-" {
		resetOverride = pro31.ResetLabel
	} else if pro3 != nil && pro3.ResetLabel != "" && pro3.ResetLabel != "-" {
		resetOverride = pro3.ResetLabel
	}
	for _, group := range antigravityModelGroups[3:] {
		override := ""
		if group.ID == "gemini-image" {
			override = resetOverride
		}
		addGroup(group, override)
	}
	return windows
}

func populateGoogleAssistReport(report *quotaReport, payload map[string]any) {
	currentTierID := cleanString(nested(payload, "currentTier", "id"))
	currentTierName := cleanString(nested(payload, "currentTier", "name"))
	channelName := cleanString(nested(payload, "releaseChannel", "name"))
	channelType := cleanString(nested(payload, "releaseChannel", "type"))
	projectID := cleanString(firstValue(payload["cloudaicompanionProject"], nested(payload, "cloudaicompanionProject", "id")))
	paidTierName := cleanString(nested(payload, "paidTier", "name"))
	paidTierID := cleanString(nested(payload, "paidTier", "id"))

	report.PlanType = firstNonEmpty(currentTierID, "unknown")
	report.MetaFields["tier"] = firstNonEmpty(currentTierName, currentTierID)
	report.MetaFields["channel"] = firstNonEmpty(channelName, channelType)
	report.MetaFields["project"] = projectID
	report.MetaFields["paid_tier"] = firstNonEmpty(paidTierName, paidTierID)
	report.MetaFields["manage_subscription_uri"] = cleanString(payload["manageSubscriptionUri"])
}

func deriveCodexStatus(report quotaReport) string {
	if report.Error != "" {
		return "error"
	}
	if report.AuthIndex == "" || report.AccountID == "" {
		return "missing"
	}
	window7d := findWindow(report.Windows, "code-7d")
	if window7d == nil || window7d.RemainingPercent == nil {
		return "unknown"
	}
	remaining := *window7d.RemainingPercent
	if remaining <= 0 {
		return "exhausted"
	}
	if remaining <= 30 {
		return "low"
	}
	if remaining <= 70 {
		return "medium"
	}
	if remaining < 100 {
		return "high"
	}
	return "full"
}

func deriveGoogleQuotaStatus(report quotaReport) string {
	if report.Error != "" {
		return "error"
	}
	if len(report.Windows) == 0 {
		return "unknown"
	}
	totalRemaining := 0.0
	count := 0
	hasValue := false
	for _, window := range report.Windows {
		if window.RemainingPercent == nil {
			continue
		}
		hasValue = true
		totalRemaining += clampFloat(*window.RemainingPercent, 0, 100)
		count++
	}
	if !hasValue || count == 0 {
		return "unknown"
	}
	avgRemaining := totalRemaining / float64(count)
	switch {
	case avgRemaining <= 0:
		return "exhausted"
	case avgRemaining <= 30:
		return "low"
	case avgRemaining <= 70:
		return "medium"
	case avgRemaining < 100:
		return "high"
	default:
		return "full"
	}
}

func deriveGeminiStatus(report quotaReport) string {
	return deriveGoogleQuotaStatus(report)
}

func sortReportsByProvider(reports []quotaReport) {
	sort.SliceStable(reports, func(i, j int) bool {
		left, right := reports[i], reports[j]
		if providerOrderRank(left.Provider) != providerOrderRank(right.Provider) {
			return providerOrderRank(left.Provider) < providerOrderRank(right.Provider)
		}
		switch left.Provider {
		case "codex":
			return codexLess(left, right)
		case "gemini-cli", "antigravity":
			return geminiLess(left, right)
		default:
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		}
	})
}

func codexLess(left, right quotaReport) bool {
	planRank := func(plan string) int {
		switch strings.ToLower(strings.TrimSpace(plan)) {
		case "free":
			return 0
		case "team":
			return 1
		case "plus":
			return 2
		default:
			return 3
		}
	}
	remaining7d := func(report quotaReport) float64 {
		window := findWindow(report.Windows, "code-7d")
		if window == nil || window.RemainingPercent == nil {
			return 101
		}
		return *window.RemainingPercent
	}
	if planRank(left.PlanType) != planRank(right.PlanType) {
		return planRank(left.PlanType) < planRank(right.PlanType)
	}
	if remaining7d(left) != remaining7d(right) {
		return remaining7d(left) < remaining7d(right)
	}
	return strings.ToLower(left.Name) < strings.ToLower(right.Name)
}

func geminiLess(left, right quotaReport) bool {
	statusRank := func(status string) int {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "full":
			return 0
		case "high":
			return 1
		case "medium":
			return 2
		case "low":
			return 3
		case "exhausted":
			return 4
		case "unknown":
			return 5
		case "error":
			return 6
		default:
			return 7
		}
	}
	availableCount := func(report quotaReport) int {
		total := 0
		for _, key := range []string{"claude", "gemini-3-flash", "gemini-3-pro"} {
			cell, ok := report.Cells[key]
			if !ok {
				continue
			}
			if cell.Severity == "ok" {
				total++
			}
		}
		return total
	}
	if statusRank(left.Status) != statusRank(right.Status) {
		return statusRank(left.Status) < statusRank(right.Status)
	}
	if availableCount(left) != availableCount(right) {
		return availableCount(left) > availableCount(right)
	}
	return strings.ToLower(left.Name) < strings.ToLower(right.Name)
}

func providerOrderRank(provider string) int {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return 0
	case "gemini-cli":
		return 1
	case "antigravity":
		return 2
	default:
		return 99
	}
}
