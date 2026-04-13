package main

import "testing"

func TestDeriveCodexStatusThresholds(t *testing.T) {
	makeReport := func(value float64) quotaReport {
		return quotaReport{
			Provider:  "codex",
			AuthIndex: "auth-1",
			AccountID: "acct-1",
			Windows: []quotaWindow{
				{ID: "code-7d", RemainingPercent: &value},
			},
		}
	}

	cases := []struct {
		name string
		pct  float64
		want string
	}{
		{name: "exhausted", pct: 0, want: "exhausted"},
		{name: "low", pct: 25, want: "low"},
		{name: "medium", pct: 50, want: "medium"},
		{name: "high", pct: 90, want: "high"},
		{name: "full", pct: 100, want: "full"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveCodexStatus(makeReport(tc.pct))
			if got != tc.want {
				t.Fatalf("deriveCodexStatus(%v) = %q, want %q", tc.pct, got, tc.want)
			}
		})
	}
}

func TestPopulateGeminiReport(t *testing.T) {
	report := quotaReport{
		Provider:   "gemini-cli",
		MetaFields: map[string]string{},
	}
	payload := map[string]any{
		"currentTier": map[string]any{
			"id":          "standard-tier",
			"name":        "Gemini Code Assist",
			"description": "Unlimited coding assistant with the most powerful Gemini models",
		},
		"releaseChannel": map[string]any{
			"name": "Preview Channel",
			"type": "EXPERIMENTAL",
		},
		"cloudaicompanionProject": "workspacecli-489315",
		"paidTier": map[string]any{
			"id":   "g1-pro-tier",
			"name": "Gemini Code Assist in Google One AI Pro",
		},
	}

	populateGeminiReport(&report, payload)

	if report.PlanType != "standard-tier" {
		t.Fatalf("plan_type = %q, want standard-tier", report.PlanType)
	}
	if report.MetaFields["tier"] != "Gemini Code Assist" {
		t.Fatalf("tier = %q, want Gemini Code Assist", report.MetaFields["tier"])
	}
	if report.MetaFields["channel"] != "Preview Channel" {
		t.Fatalf("channel = %q, want Preview Channel", report.MetaFields["channel"])
	}
}

func TestParseGeminiQuotaWindows(t *testing.T) {
	payload := map[string]any{
		"buckets": []any{
			map[string]any{"modelId": "gemini-2.5-flash", "remainingFraction": 0.99, "resetTime": "2026-04-14T06:32:48Z"},
			map[string]any{"modelId": "gemini-3-flash-preview", "remainingFraction": 0.99, "resetTime": "2026-04-14T06:32:48Z"},
			map[string]any{"modelId": "gemini-2.5-pro", "remainingFraction": 0.9866667, "resetTime": "2026-04-14T06:32:47Z"},
			map[string]any{"modelId": "gemini-3-pro-preview", "remainingFraction": 0.9866667, "resetTime": "2026-04-14T06:32:47Z"},
			map[string]any{"modelId": "gemini-3.1-pro-preview", "remainingFraction": 0.9866667, "resetTime": "2026-04-14T06:32:47Z"},
			map[string]any{"modelId": "gemini-2.5-flash-lite", "remainingFraction": 1.0, "resetTime": "2026-04-14T06:50:19Z"},
			map[string]any{"modelId": "gemini-3.1-flash-lite-preview", "remainingFraction": 1.0, "resetTime": "2026-04-14T06:50:19Z"},
		},
	}

	windows := parseGeminiQuotaWindows(payload)
	if len(windows) != 4 {
		t.Fatalf("len(windows) = %d, want 4", len(windows))
	}
	if windows[0].Label != "Gemini Flash Lite Series" {
		t.Fatalf("windows[0].Label = %q", windows[0].Label)
	}
	if windows[1].Label != "Gemini Flash Series" {
		t.Fatalf("windows[1].Label = %q", windows[1].Label)
	}
	if windows[2].Label != "Gemini Pro Series" {
		t.Fatalf("windows[2].Label = %q", windows[2].Label)
	}
	if windows[3].Label != "gemini-3.1-flash-lite-preview" {
		t.Fatalf("windows[3].Label = %q", windows[3].Label)
	}
	report := quotaReport{Provider: "gemini-cli", Windows: windows}
	if got := deriveGeminiStatus(report); got != "high" {
		t.Fatalf("deriveGeminiStatus() = %q, want high", got)
	}
}

func TestDeriveGeminiStatusThresholds(t *testing.T) {
	makeReport := func(values ...float64) quotaReport {
		windows := make([]quotaWindow, 0, len(values))
		for i, value := range values {
			v := value
			windows = append(windows, quotaWindow{
				ID:               "w" + string(rune('a'+i)),
				Label:            "w",
				RemainingPercent: &v,
			})
		}
		return quotaReport{Provider: "gemini-cli", Windows: windows}
	}

	cases := []struct {
		name   string
		values []float64
		want   string
	}{
		{name: "exhausted", values: []float64{0, 0, 0}, want: "exhausted"},
		{name: "low", values: []float64{20, 30, 10}, want: "low"},
		{name: "medium", values: []float64{50, 40, 60}, want: "medium"},
		{name: "high", values: []float64{80, 90, 95}, want: "high"},
		{name: "full", values: []float64{100, 100, 100}, want: "full"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveGeminiStatus(makeReport(tc.values...)); got != tc.want {
				t.Fatalf("deriveGeminiStatus(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

func TestFilterReportsByProvider(t *testing.T) {
	input := []quotaReport{
		{Provider: "codex", Name: "a"},
		{Provider: "gemini-cli", Name: "b"},
	}
	got := filterReportsByProvider(input, "gemini-cli")
	if len(got) != 1 || got[0].Provider != "gemini-cli" {
		t.Fatalf("filterReportsByProvider() = %+v, want only gemini-cli", got)
	}
}
