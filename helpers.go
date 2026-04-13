package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func colorAtPercent(pct float64) string {
	pct = clampFloat(pct, 0, 100)
	switch {
	case pct <= 30:
		t := pct / 30.0
		return lerpHex("#EF4444", "#F59E0B", t)
	case pct <= 70:
		t := (pct - 30.0) / 40.0
		return lerpHex("#F59E0B", "#10B981", t)
	default:
		t := (pct - 70.0) / 30.0
		return lerpHex("#22C55E", "#84CC16", t)
	}
}

func lerpHex(fromHex, toHex string, t float64) string {
	t = clampFloat(t, 0, 1)
	fr, fg, fb := parseHexColor(fromHex)
	tr, tg, tb := parseHexColor(toHex)
	rr := int(float64(fr) + (float64(tr-fr) * t))
	rg := int(float64(fg) + (float64(tg-fg) * t))
	rb := int(float64(fb) + (float64(tb-fb) * t))
	return fmt.Sprintf("#%02X%02X%02X", rr, rg, rb)
}

func parseHexColor(hex string) (int, int, int) {
	s := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(s) != 6 {
		return 255, 255, 255
	}
	r, err1 := strconv.ParseInt(s[0:2], 16, 64)
	g, err2 := strconv.ParseInt(s[2:4], 16, 64)
	b, err3 := strconv.ParseInt(s[4:6], 16, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 255, 255, 255
	}
	return int(r), int(g), int(b)
}

func extraSummary(windows []quotaWindow) string {
	if len(windows) == 0 {
		return "-"
	}
	parts := make([]string, 0, 2)
	for i, window := range windows {
		if i >= 2 {
			break
		}
		label := truncate(window.Label, 10)
		pct := "?"
		if window.RemainingPercent != nil {
			pct = fmt.Sprintf("%.0f%%", *window.RemainingPercent)
		}
		parts = append(parts, label+" "+pct)
	}
	if len(windows) > 2 {
		parts = append(parts, fmt.Sprintf("+%d", len(windows)-2))
	}
	return strings.Join(parts, ", ")
}

func findWindow(windows []quotaWindow, id string) *quotaWindow {
	for i := range windows {
		if windows[i].ID == id {
			return &windows[i]
		}
	}
	return nil
}

func resetLabel(window *quotaWindow) string {
	if window == nil {
		return "-"
	}
	return window.ResetLabel
}

func formatCountMap(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func truncate(v string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(v) <= width {
		return v
	}
	if width <= 3 {
		runes := []rune(v)
		if len(runes) <= width {
			return v
		}
		return string(runes[:width])
	}
	runes := []rune(v)
	var b strings.Builder
	for _, r := range runes {
		next := b.String() + string(r)
		if displayWidth(next) > width-3 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "..."
}

func padRight(v string, width int) string {
	if displayWidth(v) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-displayWidth(v))
}

func displayWidth(v string) int {
	return lipgloss.Width(v)
}

func nested(m map[string]any, keys ...string) any {
	cur := any(m)
	for _, key := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[key]
	}
	return cur
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		return value
	}
	return nil
}

func anyFromMap(m map[string]any, keys ...string) any {
	if m == nil {
		return nil
	}
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
	}
	return nil
}

func cleanString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}

func normalizePlan(v any) string {
	return strings.ToLower(cleanString(v))
}

func bodyString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}

func isFalse(v any) bool {
	if b, ok := v.(bool); ok {
		return !b
	}
	return false
}

func numberPtr(v any) *float64 {
	n := numberFromAny(v)
	if n == 0 && !isNumberish(v) {
		return nil
	}
	return &n
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func numberFromAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		n, _ := x.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(x, "%")), 64)
		return n
	default:
		return 0
	}
}

func isNumberish(v any) bool {
	switch t := v.(type) {
	case float64, int, int64, json.Number:
		return true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return false
		}
		_, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return err == nil
	default:
		return false
	}
}

func clampFloat(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func extractParenValue(value string) string {
	raw := strings.TrimSpace(value)
	start := strings.LastIndex(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(raw[start+1 : end])
}

func extractFileProject(name string) string {
	raw := strings.TrimSpace(name)
	raw = strings.TrimSuffix(raw, ".json")
	parts := strings.Split(raw, "-")
	if len(parts) < 2 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" || strings.Contains(last, "@") {
		return ""
	}
	return last
}

func titleProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return "Codex"
	case "gemini-cli":
		return "Gemini CLI"
	default:
		parts := strings.FieldsFunc(provider, func(r rune) bool {
			return r == '-' || r == '_' || r == ' '
		})
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			runes := []rune(strings.ToLower(parts[i]))
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
			parts[i] = string(runes)
		}
		return strings.Join(parts, " ")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
