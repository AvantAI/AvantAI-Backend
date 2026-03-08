package htf

// HTF (High Tight Flag) strategy configuration.
//
// ALL numerical thresholds for the HTF strategy are defined here.
// Change a value in this file and it flows through everywhere.
// Every constant has a comment explaining what it controls and the unit.
//
// NOTE: The values below are PLACEHOLDERS. A finance professional should
// review and tune each threshold before this strategy goes live.

const (
	// =========================================================================
	// FLAGPOLE CRITERIA
	// The "flagpole" is the sharp, rapid advance that precedes the flag.
	// =========================================================================

	// FlagpoleMinGainPct is the minimum price gain (as a percent) from the
	// flagpole base to the flagpole peak.
	// Example: a stock that moved from $10 to $19.50+ qualifies (95% gain).
	FlagpoleMinGainPct = 90.0

	// FlagpoleMaxTradingDays is the maximum number of trading days in which
	// the flagpole move must occur. 8 calendar weeks ≈ 40 trading days.
	FlagpoleMaxTradingDays = 40

	// =========================================================================
	// FLAG (CONSOLIDATION) CRITERIA
	// The "flag" is the tight, orderly pullback/consolidation after the pole.
	// =========================================================================

	// FlagMinTradingDays is the minimum number of trading days of consolidation
	// required after the flagpole peak. 3 calendar weeks ≈ 15 trading days.
	FlagMinTradingDays = 3

	// FlagMaxTradingDays is the maximum number of trading days of consolidation
	// allowed after the flagpole peak. 5 calendar weeks ≈ 25 trading days.
	// If the flag is longer than this, the pattern is considered extended.
	FlagMaxTradingDays = 10

	// FlagMinPullbackPct is the minimum percentage the flag high must be below
	// the flagpole peak. Ensures the stock has actually pulled back from its high
	// and is not still running up (which would not be a proper flag).
	// Measured as: (peakPrice - flagHigh) / peakPrice * 100
	FlagMinPullbackPct = 0.0

	// FlagMaxPullbackPct is the maximum percentage the flag high may be below
	// the flagpole peak. If the stock has pulled back more than this, the pattern
	// is considered broken down rather than consolidating near highs.
	// Measured as: (peakPrice - flagHigh) / peakPrice * 100
	FlagMaxPullbackPct = 25.0

	// FlagMaxRangePct is the maximum allowed range within the consolidation zone,
	// measured as: (flagHigh - flagLow) / flagHigh * 100.
	// A "tight" flag has a narrow price range — this enforces that tightness.
	FlagMaxRangePct = 10.0

	// =========================================================================
	// VOLUME CRITERIA
	// Applied during the morning scan to ensure sufficient liquidity.
	// =========================================================================

	// MinAvgDollarVolume is the minimum 21-day average daily dollar volume
	// (price * shares) required for a stock to be considered liquid enough.
	// Mirrors EP's MIN_DOLLAR_VOLUME threshold.
	MinAvgDollarVolume = 10_000_000.0 // $10M per day

	// MinAvgShareVolume is the minimum 21-day average daily share volume.
	// This is also used intraday to normalize per-bar volume for breakout detection.
	MinAvgShareVolume = 200_000.0 // 200k shares per day

	// VolumeLookbackDays is the number of trading days used to calculate
	// the average daily volume metrics in the morning scan.
	VolumeLookbackDays = 21

	// =========================================================================
	// MOVING AVERAGE CRITERIA
	// Price must be above key moving averages to qualify in the morning scan.
	// =========================================================================

	// FastMAPeriod is the period (in trading days) for the fast moving average.
	FastMAPeriod = 20 // 20-day simple moving average

	// SlowMAPeriod is the period (in trading days) for the slow moving average.
	SlowMAPeriod = 50 // 50-day simple moving average

	// RequireAboveFastMA controls whether the current price must be above
	// the FastMAPeriod moving average. Set false to disable this filter.
	RequireAboveFastMA = true

	// RequireAboveSlowMA controls whether the current price must be above
	// the SlowMAPeriod moving average. Set false to disable this filter.
	RequireAboveSlowMA = true

	// =========================================================================
	// INTRADAY BREAKOUT DETECTION CRITERIA
	// Used by the pattern recognition engine during the trading day.
	// =========================================================================

	// BreakoutVolumeMinMultiplier is the minimum ratio of the breakout bar's
	// volume to the average per-bar volume required to confirm a valid breakout.
	// The average per-bar volume is estimated as: avgDailyVolume / barsPerSession.
	// Example: 1.5 means the breakout bar must have at least 1.5x the average bar volume.
	BreakoutVolumeMinMultiplier = 1.5

	// BreakoutVolumeStrongMultiplier defines a "strong" breakout volume level.
	// Used only for logging/metadata — does not block signal emission.
	BreakoutVolumeStrongMultiplier = 2.0

	// BreakoutConfirmationBars is the number of consecutive bars that must
	// close above the flag's resistance level to confirm a non-false breakout.
	// If price dips back below the resistance before this count is reached,
	// the tentative breakout is reset (false breakout protection).
	BreakoutConfirmationBars = 2

	// BreakoutFirstHalfOnly controls whether breakout signals are only emitted
	// during the first half of the trading day (before BreakoutFirstHalfEndHour).
	// Set to true to filter out late-day, lower-conviction breakouts.
	BreakoutFirstHalfOnly = false

	// BreakoutFirstHalfEndHour is the hour (Eastern Time, 24-hour clock) at
	// which the "first half" of the trading day ends. Only relevant when
	// BreakoutFirstHalfOnly is true.
	// 13 = 1:00 PM ET, approximately the midpoint of the 9:30–16:00 session.
	BreakoutFirstHalfEndHour = 13

	// =========================================================================
	// DATA & FILE PATHS
	// Paths and lookback windows for data fetching and output storage.
	// =========================================================================

	// HistoricalLookbackDays is the number of calendar days of historical
	// daily price data fetched for each stock during the morning scan.
	// Must be long enough to cover: FlagpoleMaxTradingDays + FlagMaxTradingDays + SlowMAPeriod.
	HistoricalLookbackDays = 180 // ~6 calendar months

	// StockUniverseCSVPath is the path to the CSV file containing the list of
	// stock tickers to scan. Matches the file used by the EP strategy.
	StockUniverseCSVPath = "pkg/ep/config.csv"

	// OutputDir is the directory where the morning scan JSON results are saved.
	OutputDir = "data/htf"

	// WatchlistCSVFilename is the name of the CSV file where confirmed intraday
	// HTF breakout signals are written during the trading day.
	WatchlistCSVFilename = "htf_watchlist.csv"
)
