package main

import "testing"

func TestAssessProfileEvidenceMarksInsufficientTrades(t *testing.T) {
	thresholds := evidenceThresholds{
		MinTradesForCredibility:     10,
		MinActiveBarsForCredibility: 100,
		MinTestedDaysForCredibility: 5,
		MinStudyWindowDays:          30,
		MinUsableSymbols:            5,
		MinCoverageRatio:            0.60,
		MaxDominantSymbolShare:      0.65,
		MaxSegmentGapPct:            12,
	}
	result := profileResult{
		TotalTrades:              1,
		ActiveBarsTested:         180,
		ActiveDaysEstimate:       12,
		StudyWindowDays:          60,
		UsableSymbolCount:        8,
		CoverageRatio:            0.80,
		TradedSymbols:            1,
		DominantSymbolTradeShare: 1.0,
		FirstHalfReturnPct:       2,
		SecondHalfReturnPct:      1,
		SegmentStability:         0.80,
		Diversification:          0.0,
		CompositeScore:           0.55,
	}

	assessment := assessProfileEvidence(result, thresholds, 2)
	if assessment.RankingEligible {
		t.Fatalf("expected ranking to be blocked for insufficient sample, got %+v", assessment)
	}
	if assessment.CredibilityTier != "insufficient" {
		t.Fatalf("expected insufficient tier, got %q", assessment.CredibilityTier)
	}
	if !containsString(assessment.QualityFlags, "insufficient_trades") {
		t.Fatalf("expected insufficient_trades flag, got %v", assessment.QualityFlags)
	}
}

func TestSortProfileResultsPrefersCredibleEvidence(t *testing.T) {
	thresholds := evidenceThresholds{
		MinTradesForCredibility:     10,
		MinActiveBarsForCredibility: 100,
		MinTestedDaysForCredibility: 5,
		MinStudyWindowDays:          30,
		MinUsableSymbols:            5,
		MinCoverageRatio:            0.60,
		MaxDominantSymbolShare:      0.65,
		MaxSegmentGapPct:            12,
	}

	weak := profileResult{
		ProfileSlug:              "weak_one_trade",
		TotalTrades:              1,
		ActiveBarsTested:         180,
		ActiveDaysEstimate:       12,
		StudyWindowDays:          60,
		UsableSymbolCount:        8,
		CoverageRatio:            0.90,
		TradedSymbols:            1,
		DominantSymbolTradeShare: 1.0,
		FirstHalfReturnPct:       5,
		SecondHalfReturnPct:      4,
		SegmentStability:         0.75,
		Diversification:          0.0,
		CompositeScore:           0.90,
		ReturnPct:                4.0,
		MaxDrawdownPct:           1.0,
	}
	strong := profileResult{
		ProfileSlug:              "credible_sample",
		TotalTrades:              18,
		ActiveBarsTested:         220,
		ActiveDaysEstimate:       18,
		StudyWindowDays:          75,
		UsableSymbolCount:        12,
		CoverageRatio:            0.85,
		TradedSymbols:            4,
		DominantSymbolTradeShare: 0.40,
		FirstHalfReturnPct:       2.2,
		SecondHalfReturnPct:      2.0,
		SegmentStability:         0.82,
		Diversification:          0.78,
		CompositeScore:           0.38,
		ReturnPct:                1.6,
		MaxDrawdownPct:           2.5,
	}

	weakAssessment := assessProfileEvidence(weak, thresholds, 2)
	weak.EvidenceScore = weakAssessment.EvidenceScore
	weak.CredibilityTier = weakAssessment.CredibilityTier
	weak.RankingEligible = weakAssessment.RankingEligible
	weak.QualityFlags = weakAssessment.QualityFlags
	weak.RankingScore = weakAssessment.RankingScore

	strongAssessment := assessProfileEvidence(strong, thresholds, 2)
	strong.EvidenceScore = strongAssessment.EvidenceScore
	strong.CredibilityTier = strongAssessment.CredibilityTier
	strong.RankingEligible = strongAssessment.RankingEligible
	strong.QualityFlags = strongAssessment.QualityFlags
	strong.RankingScore = strongAssessment.RankingScore

	results := []profileResult{weak, strong}
	sortProfileResults(results)

	if results[0].ProfileSlug != "credible_sample" {
		t.Fatalf("expected credible profile to rank first, got %s", results[0].ProfileSlug)
	}
}

func TestAssessProfileEvidenceFlagsCoverageAndWindow(t *testing.T) {
	thresholds := evidenceThresholds{
		MinTradesForCredibility:     8,
		MinActiveBarsForCredibility: 60,
		MinTestedDaysForCredibility: 5,
		MinStudyWindowDays:          45,
		MinUsableSymbols:            6,
		MinCoverageRatio:            0.60,
		MaxDominantSymbolShare:      0.65,
		MaxSegmentGapPct:            12,
	}
	result := profileResult{
		TotalTrades:              16,
		ActiveBarsTested:         90,
		ActiveDaysEstimate:       8,
		StudyWindowDays:          12,
		UsableSymbolCount:        3,
		CoverageRatio:            0.30,
		TradedSymbols:            3,
		DominantSymbolTradeShare: 0.45,
		FirstHalfReturnPct:       1.0,
		SecondHalfReturnPct:      -14.0,
		SegmentStability:         0.32,
		Diversification:          0.62,
		CompositeScore:           0.22,
	}

	assessment := assessProfileEvidence(result, thresholds, 2)
	if !containsString(assessment.QualityFlags, "narrow_window") {
		t.Fatalf("expected narrow_window flag, got %v", assessment.QualityFlags)
	}
	if !containsString(assessment.QualityFlags, "insufficient_symbol_coverage") {
		t.Fatalf("expected insufficient_symbol_coverage flag, got %v", assessment.QualityFlags)
	}
	if !containsString(assessment.QualityFlags, "unstable_segments") {
		t.Fatalf("expected unstable_segments flag, got %v", assessment.QualityFlags)
	}
}
