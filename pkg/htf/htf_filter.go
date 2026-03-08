package htf

// Component 1: Morning Pre-Market Filter
//
// This file implements the rule-based HTF morning scanner logic.
// It is called once per trading day, before market open, to build
// the candidate watchlist from a universe of stocks.
//
// HTF filter criteria — a stock must meet ALL of the following:
//   1. Flagpole: price gained FlagpoleMinGainPct%+ in <= FlagpoleMaxTradingDays trading days.
//   2. Flag duration: FlagMinTradingDays–FlagMaxTradingDays trading days since the peak.
//   3. Flag pullback: the flag high is FlagMinPullbackPct–FlagMaxPullbackPct% below the peak.
//   4. Flag tightness: the flag's high-to-low range is <= FlagMaxRangePct%.
//   5. Volume: average daily dollar volume >= MinAvgDollarVolume.
//   6. Moving averages: current price is above FastMAPeriod-day and SlowMAPeriod-day SMAs.

import (
	"fmt"
	"math"
)

// CalculateSMA returns the simple moving average of the last `period` closing prices.
// bars must be sorted oldest-to-newest. Returns 0 if insufficient data.
func CalculateSMA(bars []DailyBar, period int) float64 {
	n := len(bars)
	if n < period {
		return 0
	}
	sum := 0.0
	for i := n - period; i < n; i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

// CalculateAvgShareVolume returns the average daily share volume over the last
// `days` bars. Returns 0 if insufficient data.
func CalculateAvgShareVolume(bars []DailyBar, days int) float64 {
	n := len(bars)
	if n < days {
		days = n
	}
	if days == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for i := n - days; i < n; i++ {
		if bars[i].Volume > 0 {
			sum += bars[i].Volume
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// CalculateAvgDollarVolume returns the average daily dollar volume (close * volume)
// over the last `days` bars. Returns 0 if insufficient data.
func CalculateAvgDollarVolume(bars []DailyBar, days int) float64 {
	n := len(bars)
	if n < days {
		days = n
	}
	if days == 0 {
		return 0
	}
	sum := 0.0
	count := 0
	for i := n - days; i < n; i++ {
		if bars[i].Volume > 0 && bars[i].Close > 0 {
			sum += bars[i].Close * bars[i].Volume
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// DetectFlagpole scans bars (sorted oldest-to-newest) for a qualifying flagpole.
//
// It looks for a peak that occurred FlagMinTradingDays–FlagMaxTradingDays ago
// (leaving room for the current flag), where the move from a nearby low to that
// peak gained at least FlagpoleMinGainPct% in at most FlagpoleMaxTradingDays bars.
//
// Returns (FlagpoleStats, peakIndex, true) on success, (-1, false) on failure.
func DetectFlagpole(bars []DailyBar) (FlagpoleStats, int, bool) {
	n := len(bars)

	// We need enough bars to cover the flag + flagpole + SMA lookback.
	minRequired := FlagpoleMaxTradingDays + FlagMinTradingDays
	if n < minRequired {
		return FlagpoleStats{}, -1, false
	}

	// The flagpole peak must have occurred:
	//   - At least FlagMinTradingDays ago (so the flag has had time to form)
	//   - At most FlagMaxTradingDays ago (so the flag isn't over-extended)
	//
	// bars[n-1] is today. bars[n-1-FlagMinTradingDays] is the most recent
	// valid peak position. bars[n-1-FlagMaxTradingDays] is the oldest valid
	// peak position.
	peakSearchEnd := n - 1 - FlagMinTradingDays
	peakSearchStart := n - 1 - FlagMaxTradingDays

	// Ensure we have room to look back for a flagpole before the peak.
	if peakSearchStart < FlagpoleMaxTradingDays {
		peakSearchStart = FlagpoleMaxTradingDays
	}

	if peakSearchEnd < peakSearchStart {
		return FlagpoleStats{}, -1, false
	}

	// Iterate candidate peak positions from most recent to oldest.
	// Prefer the freshest setup to minimise time-in-flag risk.
	for peakIdx := peakSearchEnd; peakIdx >= peakSearchStart; peakIdx-- {
		peakPrice := bars[peakIdx].High

		// Look back up to FlagpoleMaxTradingDays to find the flagpole base
		// (the lowest low in the lookback window before the peak).
		baseSearchStart := peakIdx - FlagpoleMaxTradingDays
		if baseSearchStart < 0 {
			baseSearchStart = 0
		}

		baseLow := math.MaxFloat64
		baseIdx := -1
		for i := baseSearchStart; i < peakIdx; i++ {
			if bars[i].Low < baseLow && bars[i].Low > 0 {
				baseLow = bars[i].Low
				baseIdx = i
			}
		}

		if baseIdx == -1 || baseLow <= 0 {
			continue
		}

		// Check flagpole gain: must be >= FlagpoleMinGainPct%
		gainPct := (peakPrice - baseLow) / baseLow * 100.0
		if gainPct < FlagpoleMinGainPct {
			continue
		}

		// Check flagpole duration: base-to-peak in trading days must be
		// <= FlagpoleMaxTradingDays (the "tight" part of the High Tight Flag).
		durationDays := peakIdx - baseIdx
		if durationDays > FlagpoleMaxTradingDays {
			continue
		}

		// Qualifying flagpole found.
		return FlagpoleStats{
			BaseDate:            bars[baseIdx].Date,
			BasePrice:           baseLow,
			PeakDate:            bars[peakIdx].Date,
			PeakPrice:           peakPrice,
			GainPct:             gainPct,
			DurationTradingDays: durationDays,
		}, peakIdx, true
	}

	return FlagpoleStats{}, -1, false
}

// DetectFlag validates the consolidation (flag) that follows the flagpole peak.
//
// bars must be sorted oldest-to-newest. peakIdx is the index of the flagpole
// peak within bars. The flag bars are bars[peakIdx+1 : len(bars)].
//
// Returns (FlagStats, true) if the consolidation qualifies, (zero, false) if not.
func DetectFlag(bars []DailyBar, peakIdx int) (FlagStats, bool) {
	n := len(bars)
	if peakIdx+1 >= n {
		return FlagStats{}, false
	}

	flagBars := bars[peakIdx+1 : n]
	numFlagBars := len(flagBars)

	// Duration check: consolidation must be 3–5 weeks (FlagMinTradingDays–FlagMaxTradingDays).
	if numFlagBars < FlagMinTradingDays || numFlagBars > FlagMaxTradingDays {
		return FlagStats{}, false
	}

	// Calculate the flag high (resistance) and low (support) across all flag bars.
	flagHigh := 0.0
	flagLow := math.MaxFloat64
	for _, bar := range flagBars {
		if bar.High > flagHigh {
			flagHigh = bar.High
		}
		if bar.Low < flagLow {
			flagLow = bar.Low
		}
	}

	if flagHigh <= 0 || flagLow <= 0 || flagLow >= flagHigh {
		return FlagStats{}, false
	}

	peakPrice := bars[peakIdx].High

	// Tightness check: the flag's high-to-low range must be <= FlagMaxRangePct%.
	// A tight, orderly flag has a small range relative to the flag high.
	rangePct := (flagHigh - flagLow) / flagHigh * 100.0
	if rangePct > FlagMaxRangePct {
		return FlagStats{}, false
	}

	// Pullback check: the flag high must be FlagMinPullbackPct–FlagMaxPullbackPct%
	// below the flagpole peak. This ensures:
	//   - The stock has actually pulled back from its peak (not still running up).
	//   - It hasn't crashed so far below the peak that the pattern is broken.
	pullbackPct := (peakPrice - flagHigh) / peakPrice * 100.0
	if pullbackPct < FlagMinPullbackPct || pullbackPct > FlagMaxPullbackPct {
		return FlagStats{}, false
	}

	return FlagStats{
		FlagHigh:            flagHigh,
		FlagLow:             flagLow,
		RangePct:            rangePct,
		PullbackFromPeakPct: pullbackPct,
		TradingDays:         numFlagBars,
	}, true
}

// ScanForHTFCandidate applies all HTF morning filter criteria to bars for a
// single stock. bars must be sorted oldest-to-newest and should cover at least
// HistoricalLookbackDays of data.
//
// Returns (*HTFCandidate, true) if the stock qualifies, (nil, false) if not.
func ScanForHTFCandidate(symbol string, bars []DailyBar) (*HTFCandidate, bool) {
	n := len(bars)

	// Minimum data required: flagpole window + flag window + SMA slow period.
	minBars := FlagpoleMaxTradingDays + FlagMaxTradingDays + SlowMAPeriod
	if n < minBars {
		fmt.Printf("[HTF Filter] %s: insufficient data (%d bars, need %d)\n", symbol, n, minBars)
		return nil, false
	}

	// ---- Volume filter ----
	avgShareVol := CalculateAvgShareVolume(bars, VolumeLookbackDays)
	if avgShareVol < MinAvgShareVolume {
		return nil, false
	}

	avgDollarVol := CalculateAvgDollarVolume(bars, VolumeLookbackDays)
	if avgDollarVol < MinAvgDollarVolume {
		return nil, false
	}

	// ---- Moving average filter ----
	sma20 := CalculateSMA(bars, FastMAPeriod)
	sma50 := CalculateSMA(bars, SlowMAPeriod)
	currentPrice := bars[n-1].Close

	if RequireAboveFastMA && sma20 > 0 && currentPrice < sma20 {
		return nil, false
	}
	if RequireAboveSlowMA && sma50 > 0 && currentPrice < sma50 {
		return nil, false
	}

	// ---- Flagpole detection ----
	flagpole, peakIdx, found := DetectFlagpole(bars)
	if !found {
		return nil, false
	}

	// ---- Flag detection ----
	flag, found := DetectFlag(bars, peakIdx)
	if !found {
		return nil, false
	}

	fmt.Printf("[HTF Filter] %s: QUALIFIES — pole: %.1f%% in %d days | flag: %.1f%% range, %d days | resistance: $%.2f\n",
		symbol,
		flagpole.GainPct, flagpole.DurationTradingDays,
		flag.RangePct, flag.TradingDays,
		flag.FlagHigh,
	)

	return &HTFCandidate{
		Symbol:          symbol,
		ResistanceLevel: flag.FlagHigh,
		SupportLevel:    flag.FlagLow,
		Flagpole:        flagpole,
		Flag:            flag,
		CurrentPrice:    currentPrice,
		AvgDollarVolume: avgDollarVol,
		AvgShareVolume:  avgShareVol,
		SMA20:           sma20,
		SMA50:           sma50,
	}, true
}
