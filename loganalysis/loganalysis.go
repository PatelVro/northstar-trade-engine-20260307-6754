// Package loganalysis analyses runtime trading-system logs to surface silent
// failures, abnormal patterns, and inconsistencies that would not be caught by
// simply checking for explicit error messages.
package loganalysis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"northstar/audit"
	"northstar/logger"
)

// Category classifies what aspect of the system a finding relates to.
type Category string

const (
	CategoryCycleHealth    Category = "cycle_health"
	CategoryExecutionInteg Category = "execution_integrity"
	CategoryOrderPatterns  Category = "order_patterns"
	CategoryRiskBlocking   Category = "risk_blocking"
	CategoryReconciliation Category = "reconciliation"
	CategoryPositionInteg  Category = "position_integrity"
)

// Severity mirrors the incident severity levels used elsewhere in the system.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Verdict is the overall health assessment returned by Analyze.
type Verdict string

const (
	VerdictHealthy   Verdict = "healthy"
	VerdictDegraded  Verdict = "degraded"
	VerdictUnhealthy Verdict = "unhealthy"
)

// Finding represents one detected anomaly, pattern, or inconsistency.
type Finding struct {
	Category    Category          `json:"category"`
	Severity    Severity          `json:"severity"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Evidence    map[string]string `json:"evidence,omitempty"`
}

// Report is the complete output of an analysis run.
type Report struct {
	Verdict            Verdict   `json:"verdict"`
	AnalysedAt         time.Time `json:"analysed_at"`
	CyclesAnalysed     int       `json:"cycles_analysed"`
	EventsAnalysed     int       `json:"events_analysed"`
	Findings           []Finding `json:"findings"`
	SuspiciousPatterns []string  `json:"suspicious_patterns,omitempty"`
	LikelyCauses       []string  `json:"likely_causes,omitempty"`
}

// thresholds used by the detection rules.
const (
	thresholdStallMinutes        = 30   // gap between cycles suggesting a stall
	thresholdFailureRatePct      = 25.0 // % of failed cycles triggers warning
	thresholdCriticalFailRatePct = 50.0 // % of failed cycles triggers critical
	thresholdRepeatedErrorCount  = 3    // same error string must occur this many times
	thresholdOrderRejectCount    = 3    // same symbol rejected this many times
	thresholdRiskBlockCount      = 5    // same risk-block reason this many times
	thresholdPartialFillRatio    = 0.5  // filled/requested < this ratio → partial
	thresholdSilentNonExecution  = 3    // tradable-but-no-action cycles before flagging
)

// Analyze runs all detection rules over the provided decision records and
// journal events and returns a Report summarising system health.
//
// records must be in chronological order (oldest first).
func Analyze(records []*logger.DecisionRecord, events []audit.JournalEvent) Report {
	report := Report{
		AnalysedAt:     time.Now().UTC(),
		CyclesAnalysed: len(records),
		EventsAnalysed: len(events),
	}

	if len(records) == 0 && len(events) == 0 {
		report.Verdict = VerdictHealthy
		return report
	}

	var findings []Finding
	findings = append(findings, detectCycleFailureRate(records)...)
	findings = append(findings, detectStalledCycles(records)...)
	findings = append(findings, detectRepeatedErrors(records)...)
	findings = append(findings, detectSilentNonExecution(records)...)
	findings = append(findings, detectSuspiciousSuccessMessages(records)...)
	findings = append(findings, detectOrderRejectionPatterns(records)...)
	findings = append(findings, detectRiskBlockLoops(records)...)
	findings = append(findings, detectPartialFillAnomalies(records)...)
	findings = append(findings, detectPositionInconsistencies(records)...)
	findings = append(findings, detectJournalAnomalies(events)...)

	// Sort: critical first, then warning, then info.
	sort.SliceStable(findings, func(i, j int) bool {
		return severityRank(findings[i].Severity) > severityRank(findings[j].Severity)
	})

	report.Findings = findings
	report.Verdict = overallVerdict(findings)
	report.SuspiciousPatterns = suspiciousPatternSummary(findings)
	report.LikelyCauses = likelyCauses(findings)
	return report
}

// ---------------------------------------------------------------------------
// Detection rules
// ---------------------------------------------------------------------------

// detectCycleFailureRate flags an elevated proportion of decision-cycle failures.
func detectCycleFailureRate(records []*logger.DecisionRecord) []Finding {
	if len(records) == 0 {
		return nil
	}
	failed := 0
	for _, r := range records {
		if !r.Success {
			failed++
		}
	}
	pct := float64(failed) / float64(len(records)) * 100
	if pct < thresholdFailureRatePct {
		return nil
	}
	sev := SeverityWarning
	if pct >= thresholdCriticalFailRatePct {
		sev = SeverityCritical
	}
	return []Finding{{
		Category: CategoryCycleHealth,
		Severity: sev,
		Title:    "High decision-cycle failure rate",
		Description: fmt.Sprintf(
			"%.1f%% of decision cycles failed (%d of %d). The system may be reporting "+
				"errors silently or failing to catch exceptions in the AI reasoning path.",
			pct, failed, len(records)),
		Evidence: map[string]string{
			"failed_cycles": fmt.Sprintf("%d", failed),
			"total_cycles":  fmt.Sprintf("%d", len(records)),
			"failure_pct":   fmt.Sprintf("%.1f%%", pct),
		},
	}}
}

// detectStalledCycles looks for large time gaps between consecutive decision cycles.
func detectStalledCycles(records []*logger.DecisionRecord) []Finding {
	if len(records) < 2 {
		return nil
	}
	threshold := time.Duration(thresholdStallMinutes) * time.Minute
	var findings []Finding
	for i := 1; i < len(records); i++ {
		prev := records[i-1]
		curr := records[i]
		if prev.Timestamp.IsZero() || curr.Timestamp.IsZero() {
			continue
		}
		gap := curr.Timestamp.Sub(prev.Timestamp)
		if gap > threshold {
			findings = append(findings, Finding{
				Category: CategoryCycleHealth,
				Severity: SeverityWarning,
				Title:    "Stalled decision cycle detected",
				Description: fmt.Sprintf(
					"No decision cycle was recorded for %s (between cycle %d and %d). "+
						"The trading loop may have stalled or the process restarted unexpectedly.",
					formatDuration(gap), prev.CycleNumber, curr.CycleNumber),
				Evidence: map[string]string{
					"gap_minutes":  fmt.Sprintf("%.0f", gap.Minutes()),
					"before_cycle": fmt.Sprintf("%d", prev.CycleNumber),
					"after_cycle":  fmt.Sprintf("%d", curr.CycleNumber),
					"before_time":  prev.Timestamp.UTC().Format(time.RFC3339),
					"after_time":   curr.Timestamp.UTC().Format(time.RFC3339),
				},
			})
		}
	}
	return findings
}

// detectRepeatedErrors surfaces the same error message appearing many times.
func detectRepeatedErrors(records []*logger.DecisionRecord) []Finding {
	counts := make(map[string]int)
	for _, r := range records {
		if msg := strings.TrimSpace(r.ErrorMessage); msg != "" {
			counts[msg]++
		}
		for _, d := range r.Decisions {
			if msg := strings.TrimSpace(d.Error); msg != "" {
				counts[msg]++
			}
		}
	}
	var findings []Finding
	for msg, count := range counts {
		if count >= thresholdRepeatedErrorCount {
			short := msg
			if len(short) > 120 {
				short = short[:120] + "…"
			}
			findings = append(findings, Finding{
				Category: CategoryCycleHealth,
				Severity: SeverityWarning,
				Title:    "Repeated error message",
				Description: fmt.Sprintf(
					"The error %q appeared %d times. Recurring identical errors often "+
						"indicate an unresolved root cause that is being silently retried.",
					short, count),
				Evidence: map[string]string{
					"count":         fmt.Sprintf("%d", count),
					"error_preview": short,
				},
			})
		}
	}
	return findings
}

// detectSilentNonExecution flags tradable cycles where no action was attempted
// and no explicit non-tradable reason was provided.
func detectSilentNonExecution(records []*logger.DecisionRecord) []Finding {
	count := 0
	var examples []string
	for _, r := range records {
		if !r.CycleTradable || r.ExpectedNonTradable {
			continue
		}
		if len(r.Decisions) == 0 && r.Success {
			count++
			if len(examples) < 3 {
				examples = append(examples, fmt.Sprintf("cycle %d (%s)",
					r.CycleNumber, r.Timestamp.UTC().Format("15:04:05")))
			}
		}
	}
	if count < thresholdSilentNonExecution {
		return nil
	}
	return []Finding{{
		Category: CategoryExecutionInteg,
		Severity: SeverityWarning,
		Title:    "Tradable cycles with no actions taken",
		Description: fmt.Sprintf(
			"%d cycles were marked tradable, completed successfully, yet produced zero "+
				"trade decisions. This may indicate the AI model is always choosing 'hold' "+
				"or a silent gate is suppressing order submission.",
			count),
		Evidence: map[string]string{
			"count":    fmt.Sprintf("%d", count),
			"examples": strings.Join(examples, ", "),
		},
	}}
}

// detectSuspiciousSuccessMessages looks for decisions that claim Success=true
// but carry an order status that indicates a broker-side failure.
func detectSuspiciousSuccessMessages(records []*logger.DecisionRecord) []Finding {
	failStatuses := map[string]bool{
		"rejected":  true,
		"cancelled": true,
		"canceled":  true,
		"expired":   true,
		"error":     true,
		"failed":    true,
	}
	type hit struct {
		cycle  int
		symbol string
		action string
		status string
	}
	var hits []hit
	for _, r := range records {
		for _, d := range r.Decisions {
			if !d.Success {
				continue
			}
			st := strings.ToLower(strings.TrimSpace(d.OrderStatus))
			if failStatuses[st] {
				hits = append(hits, hit{r.CycleNumber, d.Symbol, d.Action, d.OrderStatus})
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}

	// Collect unique status values for the description.
	seenSt := make(map[string]bool)
	var statusList []string
	for _, h := range hits {
		st := strings.ToLower(h.status)
		if !seenSt[st] {
			seenSt[st] = true
			statusList = append(statusList, st)
		}
	}

	examples := make([]string, 0, 3)
	for i, h := range hits {
		if i >= 3 {
			break
		}
		examples = append(examples, fmt.Sprintf("cycle %d %s %s (status=%q)",
			h.cycle, h.action, h.symbol, h.status))
	}
	return []Finding{{
		Category: CategoryExecutionInteg,
		Severity: SeverityCritical,
		Title:    "Suspicious success: order marked OK but status indicates failure",
		Description: fmt.Sprintf(
			"%d order(s) were flagged Success=true but carry a terminal failure status "+
				"(%s). This is a direct inconsistency between the system's claim and the "+
				"actual broker outcome and may lead to phantom position tracking.",
			len(hits), strings.Join(statusList, ", ")),
		Evidence: map[string]string{
			"count":    fmt.Sprintf("%d", len(hits)),
			"examples": strings.Join(examples, "; "),
		},
	}}
}

// detectOrderRejectionPatterns identifies symbols repeatedly rejected by the
// broker, which may point to a misconfigured constraint.
func detectOrderRejectionPatterns(records []*logger.DecisionRecord) []Finding {
	rejectsBySymbol := make(map[string]int)
	for _, r := range records {
		for _, d := range r.Decisions {
			if d.Success {
				continue
			}
			st := strings.ToLower(strings.TrimSpace(d.OrderStatus))
			errLower := strings.ToLower(d.Error)
			if st == "rejected" || strings.Contains(errLower, "reject") {
				rejectsBySymbol[d.Symbol]++
			}
		}
	}
	var findings []Finding
	for sym, count := range rejectsBySymbol {
		if count >= thresholdOrderRejectCount {
			findings = append(findings, Finding{
				Category: CategoryOrderPatterns,
				Severity: SeverityWarning,
				Title:    fmt.Sprintf("Repeated order rejections for %s", sym),
				Description: fmt.Sprintf(
					"%d orders for %s were rejected. Persistent rejections on the same "+
						"symbol suggest an unresolved constraint such as minimum notional, "+
						"lot size, leverage limit, or a stale price reference.",
					count, sym),
				Evidence: map[string]string{
					"symbol":       sym,
					"reject_count": fmt.Sprintf("%d", count),
				},
			})
		}
	}
	return findings
}

// detectRiskBlockLoops finds cases where the same risk-block reason repeats so
// often that trading appears permanently suppressed without an incident being
// raised.
func detectRiskBlockLoops(records []*logger.DecisionRecord) []Finding {
	blockReasons := make(map[string]int)
	for _, r := range records {
		for _, d := range r.Decisions {
			if reason := strings.TrimSpace(d.RiskSummary); reason != "" && d.RiskOutcome == "blocked" {
				blockReasons[reason]++
			}
		}
		for _, p := range r.Pipeline {
			if reason := strings.TrimSpace(p.SelectionReason); reason != "" && !p.SelectionAllowTrade {
				blockReasons["pipeline:"+reason]++
			}
		}
	}
	var findings []Finding
	for reason, count := range blockReasons {
		if count >= thresholdRiskBlockCount {
			short := reason
			if len(short) > 100 {
				short = short[:100] + "…"
			}
			findings = append(findings, Finding{
				Category: CategoryRiskBlocking,
				Severity: SeverityWarning,
				Title:    "Persistent risk block — trading may be permanently suppressed",
				Description: fmt.Sprintf(
					"The risk gate blocked trading %d times with reason %q. This pattern "+
						"suggests a static condition (e.g. a breached daily-loss limit or "+
						"kill-switch) is preventing any entries, yet no incident was escalated.",
					count, short),
				Evidence: map[string]string{
					"block_count": fmt.Sprintf("%d", count),
					"reason":      short,
				},
			})
		}
	}
	return findings
}

// detectPartialFillAnomalies flags orders where filled quantity is significantly
// below the risk-approved quantity.
func detectPartialFillAnomalies(records []*logger.DecisionRecord) []Finding {
	type partialHit struct {
		cycle  int
		symbol string
		ratio  float64
	}
	var hits []partialHit
	for _, r := range records {
		for _, d := range r.Decisions {
			if d.RiskApprovedQuantity <= 0 || d.Quantity <= 0 {
				continue
			}
			ratio := d.Quantity / d.RiskApprovedQuantity
			if ratio < thresholdPartialFillRatio {
				hits = append(hits, partialHit{r.CycleNumber, d.Symbol, ratio})
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	examples := make([]string, 0, 3)
	for i, h := range hits {
		if i >= 3 {
			break
		}
		examples = append(examples, fmt.Sprintf("cycle %d %s (%.0f%% filled)",
			h.cycle, h.symbol, h.ratio*100))
	}
	return []Finding{{
		Category: CategoryExecutionInteg,
		Severity: SeverityWarning,
		Title:    "Partial fill anomalies detected",
		Description: fmt.Sprintf(
			"%d order(s) were filled at less than %.0f%% of the risk-approved quantity. "+
				"This may indicate illiquid markets, incorrect lot-size rounding, or the "+
				"broker silently reducing the order size.",
			len(hits), thresholdPartialFillRatio*100),
		Evidence: map[string]string{
			"count":    fmt.Sprintf("%d", len(hits)),
			"examples": strings.Join(examples, "; "),
		},
	}}
}

// detectPositionInconsistencies looks for positions present in the account
// snapshot that have no corresponding open action in the analysed window.
func detectPositionInconsistencies(records []*logger.DecisionRecord) []Finding {
	if len(records) == 0 {
		return nil
	}
	opened := make(map[string]bool)
	for _, r := range records {
		for _, d := range r.Decisions {
			if d.Success && (d.Action == "open_long" || d.Action == "open_short") {
				opened[strings.ToUpper(strings.TrimSpace(d.Symbol))] = true
			}
		}
	}
	last := records[len(records)-1]
	var unknown []string
	for _, pos := range last.Positions {
		sym := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		if sym == "" {
			continue
		}
		if !opened[sym] {
			unknown = append(unknown, sym)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return []Finding{{
		Category: CategoryPositionInteg,
		Severity: SeverityWarning,
		Title:    "Open positions without a logged open action",
		Description: fmt.Sprintf(
			"%d symbol(s) appear in the latest position snapshot but have no "+
				"corresponding open action in the analysed log window. The positions may "+
				"predate the window or the open-action record may have been lost.",
			len(unknown)),
		Evidence: map[string]string{
			"symbols": strings.Join(unknown, ", "),
			"count":   fmt.Sprintf("%d", len(unknown)),
		},
	}}
}

// detectJournalAnomalies analyses audit journal events for reconciliation
// failures and critical patterns.
func detectJournalAnomalies(events []audit.JournalEvent) []Finding {
	if len(events) == 0 {
		return nil
	}
	var criticalCount, reconCount int
	for _, ev := range events {
		if ev.Severity == audit.JournalSeverityCritical {
			criticalCount++
		}
		if strings.Contains(ev.Type, "reconcil") {
			reconCount++
		}
	}
	var findings []Finding
	if criticalCount > 0 {
		findings = append(findings, Finding{
			Category: CategoryReconciliation,
			Severity: SeverityCritical,
			Title:    "Critical audit journal events recorded",
			Description: fmt.Sprintf(
				"%d critical-severity events were written to the audit journal. Critical "+
					"journal events indicate confirmed execution-truth failures, unresolved "+
					"order outcomes, or reconciliation blocks.",
				criticalCount),
			Evidence: map[string]string{
				"critical_event_count": fmt.Sprintf("%d", criticalCount),
			},
		})
	}
	if reconCount > 0 {
		findings = append(findings, Finding{
			Category: CategoryReconciliation,
			Severity: SeverityWarning,
			Title:    "Order reconciliation mismatches in audit journal",
			Description: fmt.Sprintf(
				"%d reconciliation events were logged with mismatches or repairs. "+
					"Persistent reconciliation failures suggest broker and local order states "+
					"are diverging, which can lead to duplicate entries or missed position management.",
				reconCount),
			Evidence: map[string]string{
				"reconciliation_events": fmt.Sprintf("%d", reconCount),
			},
		})
	}
	return findings
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}

func overallVerdict(findings []Finding) Verdict {
	for _, f := range findings {
		if f.Severity == SeverityCritical {
			return VerdictUnhealthy
		}
	}
	for _, f := range findings {
		if f.Severity == SeverityWarning {
			return VerdictDegraded
		}
	}
	return VerdictHealthy
}

func suspiciousPatternSummary(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityWarning {
			out = append(out, fmt.Sprintf("[%s] %s", f.Category, f.Title))
		}
	}
	return out
}

func likelyCauses(findings []Finding) []string {
	seen := make(map[string]bool)
	var causes []string
	addCause := func(c string) {
		if !seen[c] {
			seen[c] = true
			causes = append(causes, c)
		}
	}
	for _, f := range findings {
		switch f.Category {
		case CategoryCycleHealth:
			addCause("AI model timeout or upstream API failure causing decision cycles to crash")
		case CategoryExecutionInteg:
			addCause("Broker API inconsistency or order-status mapping bug producing misleading success flags")
		case CategoryOrderPatterns:
			addCause("Stale or mis-configured order parameters (lot size, notional, leverage) causing persistent broker rejections")
		case CategoryRiskBlocking:
			addCause("A risk limit (daily loss, kill-switch, drawdown) has been breached but not resolved, permanently suppressing trading")
		case CategoryReconciliation:
			addCause("Broker and local order state have diverged; position accounting may be incorrect")
		case CategoryPositionInteg:
			addCause("Position was opened outside the analysed log window or the open-action record was not persisted")
		}
	}
	return causes
}

func formatDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.0f minutes", d.Minutes())
}
