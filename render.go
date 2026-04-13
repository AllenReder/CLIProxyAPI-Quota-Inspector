package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

func renderPlain(reports []quotaReport, sum summary, summaryOnly bool) {
	for _, section := range buildSections(reports) {
		stats := providerSummaryStats(section.Provider, section.Title, section.Reports, sum)
		fmt.Printf("[%s Summary]\n", section.Title)
		fmt.Printf("Accounts: %d\n", stats.Accounts)
		fmt.Printf("Plans: %s\n", formatCountMap(stats.Plans))
		fmt.Printf("Statuses: %s\n", formatCountMap(stats.Statuses))
		for _, row := range stats.Extras {
			fmt.Printf("%s: %s\n", row.Label, row.Value)
		}
		fmt.Println()
	}
	if summaryOnly {
		return
	}
	for _, section := range buildSections(reports) {
		fmt.Printf("\n[%s]\n", section.Title)
		for _, report := range section.Reports {
			fmt.Printf("%s [%s] %s\n", report.Name, defaultString(report.PlanType, "unknown"), report.Status)
			if report.Error != "" {
				fmt.Printf("  error: %s\n", report.Error)
			}
			switch report.Provider {
			case "codex":
				for _, window := range report.Windows {
					fmt.Printf("  %s: %s reset=%s\n", window.Label, asciiProgress(window.RemainingPercent, 18), window.ResetLabel)
				}
			case "gemini-cli":
				for _, window := range report.Windows {
					fmt.Printf("  %s: %s reset=%s\n", window.Label, asciiProgress(window.RemainingPercent, 18), window.ResetLabel)
				}
				if note := geminiSummary(report); note != "-" {
					fmt.Printf("  info: %s\n", note)
				}
			}
		}
	}
}

func renderPrettyReport(reports []quotaReport, sum summary, cfg config) {
	themeTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F59E0B"))
	themeSub := lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D"))
	themeDim := lipgloss.NewStyle().Foreground(lipgloss.Color("#A8A29E"))
	tableHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FED7AA"))
	rowAlt := lipgloss.NewStyle().Foreground(lipgloss.Color("#F5F5F4"))
	rowBase := lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAF9"))

	fmt.Println(themeTitle.Render("CPA Quota Inspector"))
	fmt.Println(themeSub.Render(fmt.Sprintf("source=%s  timeout=%s  retry=%d", cfg.BaseURL, cfg.Timeout.String(), cfg.RetryAttempts)))
	fmt.Println()

	if len(reports) == 0 {
		fmt.Println(themeDim.Render("No rows match current filters."))
		return
	}

	for idx, section := range buildSections(reports) {
		if idx > 0 {
			fmt.Println()
		}
		fmt.Println(themeTitle.Render(section.Title))
		switch section.Provider {
		case "codex":
			renderCodexSection(section.Reports, cfg, tableHeader, rowBase, rowAlt, themeDim)
		case "gemini-cli":
			renderGeminiSection(section.Reports, cfg, tableHeader, rowBase, rowAlt, themeDim)
		default:
			renderGenericSection(section.Reports, tableHeader, rowBase, rowAlt, themeDim)
		}
	}

	fmt.Println()
	fmt.Println(themeTitle.Render("Summary"))
	renderSummaryTables(reports, sum, tableHeader, rowBase, themeDim)
}

func renderCodexSection(reports []quotaReport, cfg config, tableHeader, rowBase, rowAlt, themeDim lipgloss.Style) {
	termWidth := detectTerminalWidth()
	wName, wPlan, wStatus, wBar, wReset, wExtra := computeCodexWidths(termWidth)
	header := padRight("File", wName) + " " +
		padRight("Plan", wPlan) + " " +
		padRight("Status", wStatus) + " " +
		padRight("5h", wBar) + " " +
		padRight("Reset 5h", wReset) + " " +
		padRight("7d", wBar) + " " +
		padRight("Reset 7d", wReset) + " " +
		padRight("Extra", wExtra)
	fmt.Println(tableHeader.Render(header))
	fmt.Println(themeDim.Render(strings.Repeat("-", lipgloss.Width(header))))

	for i, report := range reports {
		code5 := findWindow(report.Windows, "code-5h")
		code7 := findWindow(report.Windows, "code-7d")
		row := padRight(truncate(report.Name, wName), wName) + " " +
			stylePlan(report.PlanType).Render(padRight(defaultString(report.PlanType, "-"), wPlan)) + " " +
			styleStatus(report.Status).Render(padRight(report.Status, wStatus)) + " " +
			padRight(prettyBar(code5, wBar, cfg.ASCIIBars), wBar) + " " +
			padRight(resetLabel(code5), wReset) + " " +
			padRight(prettyBar(code7, wBar, cfg.ASCIIBars), wBar) + " " +
			padRight(resetLabel(code7), wReset) + " " +
			padRight(truncate(extraSummary(report.AdditionalWindows), wExtra), wExtra)
		if i%2 == 0 {
			fmt.Println(rowBase.Render(row))
		} else {
			fmt.Println(rowAlt.Render(row))
		}
		if report.Error != "" {
			fmt.Println(themeDim.Render("  error: " + report.Error))
		}
	}
}

func renderGeminiSection(reports []quotaReport, cfg config, tableHeader, rowBase, rowAlt, themeDim lipgloss.Style) {
	termWidth := detectTerminalWidth()
	wName, wTier, wStatus := computeGeminiHeaderWidths(termWidth)
	header := padRight("File", wName) + " " +
		padRight("Tier", wTier) + " " +
		padRight("Status", wStatus)
	fmt.Println(tableHeader.Render(header))
	fmt.Println(themeDim.Render(strings.Repeat("-", lipgloss.Width(header))))

	for i, report := range reports {
		row := padRight(truncate(report.Name, wName), wName) + " " +
			padRight(truncate(defaultString(report.MetaFields["tier"], "-"), wTier), wTier) + " " +
			styleStatus(report.Status).Render(padRight(report.Status, wStatus))
		if i%2 == 0 {
			fmt.Println(rowBase.Render(row))
		} else {
			fmt.Println(rowAlt.Render(row))
		}
		for _, window := range report.Windows {
			barWidth := max(18, min(34, termWidth-44))
			bar := prettyBar(&window, barWidth, cfg.ASCIIBars)
			sub := "  " + padRight(truncate(window.Label, 28), 28) + " " + padRight(bar, barWidth) + " " + defaultString(window.ResetLabel, "-")
			fmt.Println(themeDim.Render(sub))
		}
		if info := geminiSummary(report); info != "-" {
			fmt.Println(themeDim.Render("  info: " + info))
		}
		if report.Error != "" {
			fmt.Println(themeDim.Render("  error: " + report.Error))
		}
	}
}

func renderGenericSection(reports []quotaReport, tableHeader, rowBase, rowAlt, themeDim lipgloss.Style) {
	header := padRight("File", 48) + " " + padRight("Status", 12) + " " + padRight("Plan", 16)
	fmt.Println(tableHeader.Render(header))
	fmt.Println(themeDim.Render(strings.Repeat("-", lipgloss.Width(header))))
	for i, report := range reports {
		row := padRight(truncate(report.Name, 48), 48) + " " +
			styleStatus(report.Status).Render(padRight(report.Status, 12)) + " " +
			padRight(defaultString(report.PlanType, "-"), 16)
		if i%2 == 0 {
			fmt.Println(rowBase.Render(row))
		} else {
			fmt.Println(rowAlt.Render(row))
		}
	}
}

func renderSummaryTables(reports []quotaReport, sum summary, tableHeader, rowBase, themeDim lipgloss.Style) {
	sectionTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FDE68A"))
	for idx, stats := range buildProviderSummaries(reports, sum) {
		if idx > 0 {
			fmt.Println()
		}
		fmt.Println(sectionTitle.Render(stats.Title))
		metricWidth := 22
		for _, row := range stats.Extras {
			metricWidth = max(metricWidth, displayWidth(row.Label))
		}
		header := padRight("Metric", metricWidth) + " " + "Value"
		fmt.Println(tableHeader.Render(header))
		fmt.Println(themeDim.Render(strings.Repeat("-", lipgloss.Width(header))))
		baseRows := []struct {
			label string
			value string
		}{
			{"Accounts", strconv.Itoa(stats.Accounts)},
			{"Plans", formatCountMap(stats.Plans)},
			{"Statuses", formatCountMap(stats.Statuses)},
		}
		for _, row := range baseRows {
			fmt.Println(rowBase.Render(padRight(row.label, metricWidth) + " " + row.value))
		}
		for _, row := range stats.Extras {
			fmt.Println(rowBase.Render(padRight(row.Label, metricWidth) + " " + row.Value))
		}
	}
}

func buildProviderSummaries(reports []quotaReport, sum summary) []providerSummary {
	sections := buildSections(reports)
	out := make([]providerSummary, 0, len(sections))
	for _, section := range sections {
		out = append(out, providerSummaryStats(section.Provider, section.Title, section.Reports, sum))
	}
	return out
}

func providerSummaryStats(provider string, title string, reports []quotaReport, sum summary) providerSummary {
	stats := providerSummary{
		Provider: provider,
		Title:    title,
		Accounts: len(reports),
		Plans:    map[string]int{},
		Statuses: map[string]int{},
	}
	for _, report := range reports {
		plan := report.PlanType
		if plan == "" {
			plan = "unknown"
		}
		stats.Plans[plan]++
		status := report.Status
		if status == "" {
			status = "unknown"
		}
		stats.Statuses[status]++
	}
	switch provider {
	case "codex":
		stats.Extras = append(stats.Extras,
			providerSummaryRow{Label: "Free Equivalent 7d", Value: fmt.Sprintf("%.0f%%", sum.FreeEquivalent7D)},
			providerSummaryRow{Label: "Plus Equivalent 7d", Value: fmt.Sprintf("%.0f%%", sum.PlusEquivalent7D)},
		)
	case "gemini-cli":
		if len(sum.GeminiEquivalents) == 0 {
			break
		}
		keys := make([]string, 0, len(sum.GeminiEquivalents))
		for key := range sum.GeminiEquivalents {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			stats.Extras = append(stats.Extras, providerSummaryRow{
				Label: key + " Equivalent",
				Value: fmt.Sprintf("%.0f%%", sum.GeminiEquivalents[key]),
			})
		}
	}
	return stats
}

func detectTerminalWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}
	return 140
}

func isStdoutTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func computeCodexWidths(total int) (int, int, int, int, int, int) {
	if total < 100 {
		total = 100
	}
	wPlan, wStatus, wReset := 8, 10, 12
	wName, wExtra, wBar := 28, 18, 22
	switch {
	case total >= 170:
		wName, wExtra, wBar = 36, 24, 28
	case total >= 150:
		wName, wExtra, wBar = 32, 22, 25
	case total >= 130:
		wName, wExtra, wBar = 28, 18, 21
	case total >= 110:
		wName, wExtra, wBar = 24, 12, 16
	default:
		wName, wExtra, wBar = 20, 8, 12
	}
	for {
		current := wName + wPlan + wStatus + wBar + wReset + wBar + wReset + wExtra + 7
		if current <= total {
			break
		}
		switch {
		case wExtra > 8:
			wExtra--
		case wName > 18:
			wName--
		case wBar > 10:
			wBar--
		case wPlan > 6:
			wPlan--
		case wStatus > 8:
			wStatus--
		case wReset > 10:
			wReset--
		default:
			return wName, wPlan, wStatus, wBar, wReset, wExtra
		}
	}
	return wName, wPlan, wStatus, wBar, wReset, wExtra
}

func computeGeminiHeaderWidths(total int) (int, int, int) {
	if total < 70 {
		total = 70
	}
	wName, wTier, wStatus := 34, 24, 10
	switch {
	case total >= 140:
		wName, wTier = 48, 28
	case total >= 110:
		wName, wTier = 36, 24
	default:
		wName, wTier = 24, 18
	}
	for {
		current := wName + wTier + wStatus + 2
		if current <= total {
			return wName, wTier, wStatus
		}
		if wTier > 14 {
			wTier--
			continue
		}
		if wName > 18 {
			wName--
			continue
		}
		return wName, wTier, wStatus
	}
}

func prettyBar(window *quotaWindow, width int, ascii bool) string {
	if window == nil || window.RemainingPercent == nil {
		return "-"
	}
	return prettyPercentBar(*window.RemainingPercent, width, ascii)
}

func prettyPercentBar(value float64, width int, ascii bool) string {
	if width < 8 {
		return fmt.Sprintf("%3.0f%%", clampFloat(value, 0, 100))
	}
	v := clampFloat(value, 0, 100)
	percent := fmt.Sprintf(" %3.0f%%", v)
	barArea := width - displayWidth(percent) - 2
	if barArea < 4 {
		return fmt.Sprintf("%3.0f%%", v)
	}
	filled := int((v / 100 * float64(barArea)) + 0.5)
	if filled > barArea {
		filled = barArea
	}
	unfilledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A8A29E"))
	percentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAtPercent(v))).Bold(true)
	if ascii {
		var b strings.Builder
		for i := 0; i < filled; i++ {
			segStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAtPercent(segmentPercent(i, barArea))))
			b.WriteString(segStyle.Render("="))
		}
		if filled < barArea {
			segStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAtPercent(segmentPercent(max(0, filled), barArea))))
			b.WriteString(segStyle.Render(">"))
			b.WriteString(unfilledStyle.Render(strings.Repeat(".", max(0, barArea-filled-1))))
		}
		return "[" + b.String() + "]" + percentStyle.Render(percent)
	}
	var b strings.Builder
	for i := 0; i < filled; i++ {
		segStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAtPercent(segmentPercent(i, barArea))))
		b.WriteString(segStyle.Render("█"))
	}
	b.WriteString(unfilledStyle.Render(strings.Repeat("░", max(0, barArea-filled))))
	return "[" + b.String() + "]" + percentStyle.Render(percent)
}

func progressBar(window *quotaWindow) string {
	if window == nil || window.RemainingPercent == nil {
		return "-"
	}
	return compactProgress(*window.RemainingPercent, 10)
}

func compactProgress(value float64, width int) string {
	value = clampFloat(value, 0, 100)
	filled := int((value / 100 * float64(width)) + 0.5)
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + fmt.Sprintf("] %3.0f%%", value)
}

func asciiProgress(value *float64, width int) string {
	if value == nil {
		return "-"
	}
	v := clampFloat(*value, 0, 100)
	filled := int((v / 100 * float64(width)) + 0.5)
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + fmt.Sprintf("] %3.0f%%", v)
}

func stylePlan(plan string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "plus":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA"))
	case "team":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FACC15"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F5F5F4"))
	}
}

func styleStatus(status string) lipgloss.Style {
	return styleSeverity(status).Bold(true)
}

func styleSeverity(severity string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "full":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#65A30D"))
	case "high", "ok":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#16A34A"))
	case "medium":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
	case "low", "limited":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#D97706"))
	case "exhausted", "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#DC2626"))
	case "unknown", "missing", "muted":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#A8A29E"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F5F5F4"))
	}
}

func geminiSummary(report quotaReport) string {
	parts := []string{}
	for _, key := range []string{"channel", "project", "paid_tier"} {
		if value := strings.TrimSpace(report.MetaFields[key]); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " | ")
}

func segmentPercent(index, total int) float64 {
	if total <= 1 {
		return 100
	}
	return clampFloat((float64(index+1)/float64(total))*100, 0, 100)
}
