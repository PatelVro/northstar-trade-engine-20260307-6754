package trader

import (
	"northstar/risk"
	"strings"
	"time"
)

type portfolioRiskState struct {
	EvaluatedAt time.Time
	Outcome     risk.Outcome
	Summary     string
	Metrics     risk.PortfolioMetrics
}

func (at *AutoTrader) observePortfolioRiskEvaluation(evaluation risk.Evaluation) {
	snapshot := &portfolioRiskState{
		EvaluatedAt: time.Now(),
		Outcome:     evaluation.Outcome,
		Summary:     strings.TrimSpace(evaluation.Summary),
		Metrics:     evaluation.Portfolio.Clone(),
	}

	at.portfolioRiskMu.Lock()
	at.portfolioRiskState = snapshot
	at.portfolioRiskMu.Unlock()

	if !at.paperSessionReportsEnabled() {
		return
	}
	at.ensurePaperSessionReportingForTime(snapshot.EvaluatedAt)
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.observePortfolioRisk(snapshot)
	}
}

func (at *AutoTrader) currentPortfolioRiskState() *portfolioRiskState {
	at.portfolioRiskMu.RLock()
	defer at.portfolioRiskMu.RUnlock()
	if at.portfolioRiskState == nil {
		return nil
	}
	cloned := *at.portfolioRiskState
	cloned.Metrics = at.portfolioRiskState.Metrics.Clone()
	return &cloned
}

var portfolioSectorMap = map[string]string{
	"AAPL": "technology",
	"ADBE": "technology",
	"AMD":  "technology",
	"AMAT": "technology",
	"ASML": "technology",
	"AVGO": "technology",
	"CRM":  "technology",
	"CSCO": "technology",
	"IBM":  "technology",
	"INTC": "technology",
	"MU":   "technology",
	"MSFT": "technology",
	"NVDA": "technology",
	"ORCL": "technology",
	"PANW": "technology",
	"PLTR": "technology",
	"QCOM": "technology",
	"SAP":  "technology",
	"SHOP": "technology",
	"SNOW": "technology",
	"TXN":  "technology",

	"CMCSA": "communication_services",
	"DIS":   "communication_services",
	"GOOGL": "communication_services",
	"META":  "communication_services",
	"NFLX":  "communication_services",
	"T":     "communication_services",
	"TMUS":  "communication_services",
	"VZ":    "communication_services",

	"AMZN": "consumer_discretionary",
	"BKNG": "consumer_discretionary",
	"EBAY": "consumer_discretionary",
	"HD":   "consumer_discretionary",
	"LOW":  "consumer_discretionary",
	"MCD":  "consumer_discretionary",
	"NKE":  "consumer_discretionary",
	"SBUX": "consumer_discretionary",
	"TSLA": "consumer_discretionary",

	"BNS":  "financials",
	"BAC":  "financials",
	"BLK":  "financials",
	"BMO":  "financials",
	"C":    "financials",
	"CM":   "financials",
	"GS":   "financials",
	"JPM":  "financials",
	"MA":   "financials",
	"MS":   "financials",
	"RY":   "financials",
	"SCHW": "financials",
	"TD":   "financials",
	"V":    "financials",
	"WFC":  "financials",
	"AXP":  "financials",

	"ABBV": "health_care",
	"ABT":  "health_care",
	"BMY":  "health_care",
	"DHR":  "health_care",
	"JNJ":  "health_care",
	"LLY":  "health_care",
	"MRK":  "health_care",
	"PFE":  "health_care",
	"TMO":  "health_care",
	"UNH":  "health_care",

	"BA":  "industrials",
	"CAT": "industrials",
	"DE":  "industrials",
	"GE":  "industrials",
	"HON": "industrials",
	"LMT": "industrials",
	"MMM": "industrials",
	"RTX": "industrials",
	"UNP": "industrials",
	"UPS": "industrials",

	"COP": "energy",
	"CVX": "energy",
	"EOG": "energy",
	"ENB": "energy",
	"MPC": "energy",
	"OXY": "energy",
	"SLB": "energy",
	"SU":  "energy",
	"XOM": "energy",

	"COST": "consumer_staples",
	"KO":   "consumer_staples",
	"MDLZ": "consumer_staples",
	"MO":   "consumer_staples",
	"PEP":  "consumer_staples",
	"PG":   "consumer_staples",
	"PM":   "consumer_staples",
	"WMT":  "consumer_staples",

	"AEP": "utilities",
	"DUK": "utilities",
	"EXC": "utilities",
	"NEE": "utilities",
	"SO":  "utilities",

	"AMT":  "real_estate",
	"EQIX": "real_estate",
	"O":    "real_estate",
	"PLD":  "real_estate",
	"SPG":  "real_estate",

	"APD": "materials",
	"DD":  "materials",
	"FCX": "materials",
	"LIN": "materials",
	"NEM": "materials",
	"SHW": "materials",

	"SMH":  "technology",
	"VGT":  "technology",
	"XLK":  "technology",
	"VOX":  "communication_services",
	"XLC":  "communication_services",
	"FDIS": "consumer_discretionary",
	"IBUY": "consumer_discretionary",
	"RTH":  "consumer_discretionary",
	"XLY":  "consumer_discretionary",
	"KBE":  "financials",
	"KRE":  "financials",
	"XLF":  "financials",
	"IHI":  "health_care",
	"XBI":  "health_care",
	"XLV":  "health_care",
	"ITA":  "industrials",
	"VIS":  "industrials",
	"XLI":  "industrials",
	"OIH":  "energy",
	"XLE":  "energy",
	"XOP":  "energy",
	"PBJ":  "consumer_staples",
	"VDC":  "consumer_staples",
	"XLP":  "consumer_staples",
	"IDU":  "utilities",
	"VPU":  "utilities",
	"XLU":  "utilities",
	"IYR":  "real_estate",
	"VNQ":  "real_estate",
	"XLRE": "real_estate",
	"VAW":  "materials",
	"XLB":  "materials",
}

func portfolioRiskSector(symbol string) (string, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	sector, ok := portfolioSectorMap[symbol]
	return sector, ok
}

func hasPortfolioSector(symbol string) bool {
	_, ok := portfolioRiskSector(symbol)
	return ok
}

func sectorName(symbol string) string {
	sector, ok := portfolioRiskSector(symbol)
	if !ok {
		return ""
	}
	return sector
}
