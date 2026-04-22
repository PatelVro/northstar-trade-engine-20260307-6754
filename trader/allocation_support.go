package trader

import (
	"northstar/allocator"
	"northstar/decision"
	"northstar/market"
	"northstar/selector"
	"strings"
)

func (at *AutoTrader) suggestAllocation(ctx *decision.Context, symbol, action string, entryPrice, stopLoss float64) allocator.Result {
	if ctx == nil {
		return allocator.Result{}
	}
	marketData := lookupMarketData(ctx, symbol)
	selection := at.allocationSelectionForMarketData(marketData)
	featureVector := allocationFeatureVector(marketData)
	peakEquity := at.peakEquitySeen
	if peakEquity <= 0 {
		peakEquity = ctx.Account.StrategyEquity
	}

	instrument := strings.ToLower(strings.TrimSpace(at.config.InstrumentType))
	fractional := instrument == "crypto" || instrument == "crypto_perp" || instrument == "perp"

	input := allocator.Input{
		Symbol:             strings.ToUpper(strings.TrimSpace(symbol)),
		Action:             strings.ToLower(strings.TrimSpace(action)),
		EntryPrice:         entryPrice,
		CurrentPrice:       entryPrice,
		StopLoss:           stopLoss,
		IncreasesExposure:  action == "open_long" || action == "open_short",
		Selection:          selection,
		FractionalQuantity: fractional,
		Account: allocator.AccountSnapshot{
			StrategyEquity:        ctx.Account.StrategyEquity,
			AccountEquity:         ctx.Account.AccountEquity,
			AvailableBalance:      ctx.Account.AvailableBalance,
			CurrentGrossExposure:  firstPositive(ctx.Account.GrossMarketValue, estimateGrossExposure(ctx.Positions)),
			CurrentNetExposure:    estimateNetExposure(ctx.Positions),
			CurrentSymbolExposure: currentSymbolExposure(ctx.Positions, symbol),
			PeakStrategyEquity:    peakEquity,
		},
		Config: allocator.Config{
			DynamicSizing:            at.config.DynamicPositionSizing,
			BaseRiskPerTradePct:      at.effectiveRiskPerTradePct(ctx, action),
			FallbackPositionPct:      at.config.FallbackPositionPct,
			MaxPositionPct:           at.config.MaxPositionPct,
			MaxGrossExposurePct:      at.config.MaxGrossExposure,
			MaxNetExposurePct:        at.config.MaxNetExposurePct,
			CashBufferPct:            0.95,
			DrawdownThrottleStartPct: at.config.DrawdownThrottleStartPct,
			DrawdownThrottleMinScale: at.config.DrawdownThrottleMinScale,
			MinTradeNotional:         25,
		},
	}
	if marketData != nil && marketData.CurrentPrice > 0 {
		input.CurrentPrice = marketData.CurrentPrice
	}
	if featureVector != nil {
		input.ATR14Pct = featureVector.ATR14Pct
		input.RealizedVol20 = featureVector.RealizedVol20
	}
	return allocator.Default().Allocate(input)
}

func (at *AutoTrader) allocationSelectionForMarketData(data *market.Data) *selector.Selection {
	return allocationSelectionForMarketDataStatic(data, at.config.StrategyMode)
}

func allocationFeatureVector(data *market.Data) *marketFeatureVector {
	if data == nil || data.Features == nil {
		return nil
	}
	if vector := data.Features.Vector("4h"); vector != nil {
		return &marketFeatureVector{ATR14Pct: vector.ATR14Pct, RealizedVol20: vector.RealizedVol20}
	}
	if vector := data.Features.Vector("3m"); vector != nil {
		return &marketFeatureVector{ATR14Pct: vector.ATR14Pct, RealizedVol20: vector.RealizedVol20}
	}
	return nil
}

type marketFeatureVector struct {
	ATR14Pct      float64
	RealizedVol20 float64
}

func currentSymbolExposure(positions []decision.PositionInfo, symbol string) float64 {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	total := 0.0
	for _, pos := range positions {
		if !strings.EqualFold(strings.TrimSpace(pos.Symbol), symbol) {
			continue
		}
		qty := pos.Quantity
		if qty < 0 {
			qty = -qty
		}
		price := pos.MarkPrice
		if price <= 0 {
			price = pos.EntryPrice
		}
		if qty <= 0 || price <= 0 {
			continue
		}
		total += qty * price
	}
	return total
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
