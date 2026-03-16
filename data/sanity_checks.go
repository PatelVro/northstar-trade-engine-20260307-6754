package dataquality

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func ValidateBars(symbol, interval string, bars []Bar, opts Options) Result {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	interval = strings.ToLower(strings.TrimSpace(interval))
	expectedBars := opts.ExpectedBars
	if expectedBars <= 0 {
		expectedBars = len(bars)
	}
	result := Result{
		Symbol:       strings.ToUpper(strings.TrimSpace(symbol)),
		Interval:     interval,
		CheckedAt:    now,
		ExpectedBars: expectedBars,
		ActualBars:   len(bars),
		Issues:       make([]Issue, 0, 4),
	}

	if len(bars) == 0 {
		result.Issues = append(result.Issues, Issue{
			Type:    IssueMissingBars,
			Message: fmt.Sprintf("no %s bars available for %s", interval, result.Symbol),
		})
		result.Summary = buildSummary(result)
		return result
	}

	duration := intervalDuration(interval)
	if duration <= 0 {
		duration = time.Minute
	}
	gapMultiple := opts.MissingBarGapMultiple
	if gapMultiple <= 0 {
		gapMultiple = 2.2
	}
	staleAfterBars := opts.StaleAfterBars
	if staleAfterBars <= 0 {
		staleAfterBars = 3
	}
	maxJump := opts.MaxPriceJumpPct
	if maxJump <= 0 {
		maxJump = defaultMaxJumpPct(interval)
	}

	latest := bars[len(bars)-1]
	latestBarTime := time.UnixMilli(latest.CloseTime)
	if latest.CloseTime <= 0 {
		latestBarTime = time.UnixMilli(latest.OpenTime)
	}
	result.LatestBarTime = latestBarTime.UTC()

	if expectedBars > 0 && len(bars) < expectedBars {
		result.Issues = append(result.Issues, Issue{
			Type:    IssueMissingBars,
			Message: fmt.Sprintf("received %d/%d %s bars for %s", len(bars), expectedBars, interval, result.Symbol),
		})
	}

	for i := 1; i < len(bars); i++ {
		prevClose := bars[i-1].Close
		currClose := bars[i].Close
		if prevClose > 0 && currClose > 0 {
			jumpPct := math.Abs((currClose-prevClose)/prevClose) * 100.0
			if jumpPct > result.MaxJumpPct {
				result.MaxJumpPct = jumpPct
			}
			if jumpPct > maxJump {
				result.Issues = append(result.Issues, Issue{
					Type:    IssueExtremePriceJump,
					Message: fmt.Sprintf("detected %.2f%% %s price jump in %s", jumpPct, interval, result.Symbol),
				})
				break
			}
		}

		if duration > 0 {
			prevTime := barCloseTime(bars[i-1])
			currTime := barCloseTime(bars[i])
			if currTime.After(prevTime) {
				maxGap := time.Duration(float64(duration) * gapMultiple)
				if currTime.Sub(prevTime) > maxGap {
					result.Issues = append(result.Issues, Issue{
						Type:    IssueMissingBars,
						Message: fmt.Sprintf("detected missing %s bars for %s (gap %s)", interval, result.Symbol, currTime.Sub(prevTime).Round(time.Second)),
					})
					break
				}
			}
		}
	}

	if latest.Volume <= 0 {
		result.Issues = append(result.Issues, Issue{
			Type:    IssueZeroVolume,
			Message: fmt.Sprintf("latest %s bar for %s has zero volume", interval, result.Symbol),
		})
	}

	if opts.CheckStaleness {
		staleLimit := duration * time.Duration(staleAfterBars)
		if staleLimit <= 0 {
			staleLimit = 3 * time.Minute
		}
		if !latestBarTime.IsZero() && now.Sub(latestBarTime) > staleLimit {
			result.Issues = append(result.Issues, Issue{
				Type:    IssueStaleData,
				Message: fmt.Sprintf("%s data for %s is stale by %s", interval, result.Symbol, now.Sub(latestBarTime).Round(time.Second)),
			})
		}
	}

	result.Summary = buildSummary(result)
	return result
}

func intervalDuration(interval string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return 0
	}
}

func defaultMaxJumpPct(interval string) float64 {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1m", "3m", "5m":
		return 25.0
	case "1h":
		return 35.0
	case "4h", "1d":
		return 50.0
	default:
		return 30.0
	}
}

func barCloseTime(bar Bar) time.Time {
	if bar.CloseTime > 0 {
		return time.UnixMilli(bar.CloseTime)
	}
	return time.UnixMilli(bar.OpenTime)
}

func buildSummary(result Result) string {
	if !result.Failed() {
		return fmt.Sprintf("data quality passed for %s %s (%d bars)", result.Symbol, result.Interval, result.ActualBars)
	}
	return fmt.Sprintf("data quality blocked %s %s: %s", result.Symbol, result.Interval, result.Issues[0].Message)
}
