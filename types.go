package main

import (
	"context"
	"net/http"
	"time"
)

const (
	defaultCPABaseURL      = "http://127.0.0.1:8317"
	defaultTimeoutSeconds  = 30
	defaultRetryAttempts   = 3
	window5HSeconds        = 5 * 60 * 60
	window7DSeconds        = 7 * 24 * 60 * 60
	whamUsageURL           = "https://chatgpt.com/backend-api/wham/usage"
	geminiLoadCodeAssist   = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	geminiRetrieveQuotaURL = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	antigravityModelsURL   = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravityDailyURL    = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravitySandboxURL  = "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels"
)

var whamHeaders = map[string]string{
	"Authorization": "Bearer $TOKEN$",
	"Content-Type":  "application/json",
	"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
}

type config struct {
	BaseURL        string
	ManagementKey  string
	ShowVersion    bool
	JSON           bool
	Plain          bool
	SummaryOnly    bool
	ASCIIBars      bool
	NoProgress     bool
	FilterProvider string
	FilterPlan     string
	FilterStatus   string
	Concurrency    int
	Timeout        time.Duration
	RetryAttempts  int
}

type quotaWindow struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	UsedPercent      *float64 `json:"used_percent"`
	RemainingPercent *float64 `json:"remaining_percent"`
	ResetLabel       string   `json:"reset_label"`
	Exhausted        bool     `json:"exhausted"`
}

type quotaCell struct {
	Text             string   `json:"text,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	ResetLabel       string   `json:"reset_label,omitempty"`
	Severity         string   `json:"severity,omitempty"`
}

type quotaReport struct {
	Provider          string               `json:"provider"`
	Name              string               `json:"name"`
	AuthIndex         string               `json:"auth_index,omitempty"`
	AccountID         string               `json:"account_id,omitempty"`
	PlanType          string               `json:"plan_type,omitempty"`
	Status            string               `json:"status"`
	Windows           []quotaWindow        `json:"windows,omitempty"`
	AdditionalWindows []quotaWindow        `json:"additional_windows,omitempty"`
	Cells             map[string]quotaCell `json:"cells,omitempty"`
	MetaFields        map[string]string    `json:"meta_fields,omitempty"`
	Error             string               `json:"error,omitempty"`
}

type summary struct {
	Accounts          int                `json:"accounts"`
	ProviderCounts    map[string]int     `json:"provider_counts"`
	StatusCounts      map[string]int     `json:"status_counts"`
	PlanCounts        map[string]int     `json:"plan_counts"`
	ExhaustedAccounts int                `json:"exhausted_accounts"`
	LowAccounts       int                `json:"low_accounts"`
	ErrorAccounts     int                `json:"error_accounts"`
	AdditionalWindows int                `json:"additional_windows"`
	ExhaustedNames    []string           `json:"exhausted_names"`
	LowNames          []string           `json:"low_names"`
	ErrorNames        []string           `json:"error_names"`
	FreeEquivalent7D  float64            `json:"free_equivalent_7d"`
	PlusEquivalent7D  float64            `json:"plus_equivalent_7d"`
	GeminiEquivalents map[string]float64 `json:"gemini_equivalents,omitempty"`
	AntigravityEquivs map[string]float64 `json:"antigravity_equivalents,omitempty"`
}

type providerSummaryRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type providerSummary struct {
	Provider string               `json:"provider"`
	Title    string               `json:"title"`
	Accounts int                  `json:"accounts"`
	Plans    map[string]int       `json:"plans"`
	Statuses map[string]int       `json:"statuses"`
	Extras   []providerSummaryRow `json:"extras,omitempty"`
}

type authEntry struct {
	raw map[string]any
}

type providerDef struct {
	ID           string
	SectionTitle string
	LoadAuths    func(context.Context, config) ([]authEntry, error)
	QueryReport  func(context.Context, *http.Client, config, authEntry) (quotaReport, error)
}

type reportSection struct {
	Provider string        `json:"provider"`
	Title    string        `json:"title"`
	Reports  []quotaReport `json:"reports"`
}
