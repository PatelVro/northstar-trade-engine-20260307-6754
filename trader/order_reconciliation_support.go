package trader

import "northstar/orders"

type orderReconciliationReporter interface {
	GetOrderReconciliationSummary() orders.Summary
}

func (at *AutoTrader) currentOrderReconciliationSummary() *orders.Summary {
	if at == nil || at.trader == nil {
		return nil
	}
	reporter, ok := at.trader.(orderReconciliationReporter)
	if !ok {
		return nil
	}
	summary := reporter.GetOrderReconciliationSummary()
	return &summary
}
