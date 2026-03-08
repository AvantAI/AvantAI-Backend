package htf

import "time"

// DailyBar holds a single day's OHLCV price data for a stock.
// Used by the morning scanner to analyse historical price action.
type DailyBar struct {
	Date   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64 // share volume
}

// FlagpoleStats holds the measured characteristics of the flagpole move —
// the sharp, rapid advance that precedes the flag consolidation.
type FlagpoleStats struct {
	// Date and price of the flagpole base (starting low)
	BaseDate  time.Time `json:"base_date"`
	BasePrice float64   `json:"base_price"`

	// Date and price of the flagpole peak (highest high of the advance)
	PeakDate  time.Time `json:"peak_date"`
	PeakPrice float64   `json:"peak_price"`

	// Gain from base to peak, expressed as a percentage.
	// Example: 120.5 means the stock gained 120.5% on the flagpole.
	GainPct float64 `json:"gain_pct"`

	// Number of trading days from the base to the peak.
	DurationTradingDays int `json:"duration_trading_days"`
}

// FlagStats holds the measured characteristics of the flag —
// the tight, orderly consolidation after the flagpole.
type FlagStats struct {
	// Highest high during the consolidation window (the resistance level)
	FlagHigh float64 `json:"flag_high"`

	// Lowest low during the consolidation window (the support level)
	FlagLow float64 `json:"flag_low"`

	// Range of the consolidation as a percent: (FlagHigh - FlagLow) / FlagHigh * 100.
	// A lower value means a tighter, higher-quality flag.
	RangePct float64 `json:"range_pct"`

	// How far the flag high is below the flagpole peak, as a percent.
	// Measured as: (peakPrice - flagHigh) / peakPrice * 100.
	// This quantifies the pullback from the high.
	PullbackFromPeakPct float64 `json:"pullback_from_peak_pct"`

	// Number of trading days in the consolidation (flag duration).
	TradingDays int `json:"trading_days"`
}

// HTFCandidate is a stock that passed all morning HTF scanner criteria.
// It is stored in the daily JSON scan output and loaded by the intraday monitor.
type HTFCandidate struct {
	Symbol string `json:"symbol"`

	// ResistanceLevel is the upper boundary of the flag (flag high).
	// An intraday close above this level triggers the breakout check.
	ResistanceLevel float64 `json:"resistance_level"`

	// SupportLevel is the lower boundary of the flag (flag low).
	// An intraday close below this level invalidates the pattern for the day.
	SupportLevel float64 `json:"support_level"`

	// Flagpole measurements from the morning scan
	Flagpole FlagpoleStats `json:"flagpole"`

	// Flag/consolidation measurements from the morning scan
	Flag FlagStats `json:"flag"`

	// CurrentPrice is the closing price as of the scan date
	CurrentPrice float64 `json:"current_price"`

	// AvgDollarVolume is the VolumeLookbackDays-day average daily dollar volume
	AvgDollarVolume float64 `json:"avg_dollar_volume"`

	// AvgShareVolume is the VolumeLookbackDays-day average daily share volume.
	// Also used by the intraday engine to normalize per-bar volume.
	AvgShareVolume float64 `json:"avg_share_volume"`

	// SMA20 is the 20-day simple moving average as of the scan date
	SMA20 float64 `json:"sma_20"`

	// SMA50 is the 50-day simple moving average as of the scan date
	SMA50 float64 `json:"sma_50"`
}

// HTFScanReport is the JSON file produced by the morning scanner.
// Mirrors BacktestReport in the EP strategy.
type HTFScanReport struct {
	ScanDate        string         `json:"scan_date"`
	GeneratedAt     string         `json:"generated_at"`
	TotalCandidates int            `json:"total_candidates"`
	QualifyingStocks []HTFCandidate `json:"qualifying_stocks"`
}

// WatchlistStatus tracks the intraday lifecycle state of a single candidate.
type WatchlistStatus string

const (
	// StatusWatching means the candidate is loaded and being monitored.
	StatusWatching WatchlistStatus = "watching"

	// StatusSettingUp means the first breakout bar has been detected
	// but confirmation bars have not yet been counted.
	StatusSettingUp WatchlistStatus = "setting_up"

	// StatusTriggered means all HTF breakout criteria were met and a
	// BreakoutSignal has been emitted. No further action is taken.
	StatusTriggered WatchlistStatus = "triggered"

	// StatusInvalidated means the price broke below the flag support level,
	// voiding the pattern for the rest of the day.
	StatusInvalidated WatchlistStatus = "invalidated"
)

// IntradayState tracks the running state of a single HTF candidate throughout
// the trading day. One IntradayState exists per candidate per session.
type IntradayState struct {
	// Candidate holds the pre-computed morning-scan data for this stock.
	Candidate HTFCandidate

	// Status reflects the current lifecycle stage of the pattern.
	Status WatchlistStatus

	// LatestPrice is the most recent intraday close price observed.
	LatestPrice float64

	// HighestBarVolume is the highest single-bar volume seen so far today.
	// Used to report the peak volume in the breakout signal.
	HighestBarVolume float64

	// BreakoutConfirmationBarsSeen counts how many consecutive bars have
	// closed above the resistance level since the first breakout bar.
	BreakoutConfirmationBarsSeen int

	// BreakoutFirstDetectedAt is the timestamp of the first bar that crossed
	// above resistance with qualifying volume. Zero if no breakout yet.
	BreakoutFirstDetectedAt time.Time
}

// BreakoutSignal is emitted by the pattern recognition engine when all HTF
// breakout criteria are met. This is a SIGNAL ONLY — no trade decisions are
// made or executed. Callers are responsible for any downstream actions.
type BreakoutSignal struct {
	Symbol string `json:"symbol"`

	// BreakoutTime is the timestamp of the first bar that triggered the breakout.
	BreakoutTime time.Time `json:"breakout_time"`

	// BreakoutPrice is the close of the final confirmation bar.
	BreakoutPrice float64 `json:"breakout_price"`

	// ResistanceLevel is the flag high that was broken.
	ResistanceLevel float64 `json:"resistance_level"`

	// BreakoutVolume is the highest single-bar volume seen during the breakout sequence.
	BreakoutVolume float64 `json:"breakout_volume"`

	// AvgDailyVolume is the 21-day average daily share volume from the morning scan.
	AvgDailyVolume float64 `json:"avg_daily_volume"`

	// VolumeRatio is BreakoutVolume divided by the average per-bar volume.
	// A value of 2.0 means the breakout bar had 2x the average per-bar volume.
	VolumeRatio float64 `json:"volume_ratio"`

	// ConfirmationBars is the number of consecutive bars that closed above resistance.
	ConfirmationBars int `json:"confirmation_bars"`

	// Flagpole and Flag carry the pattern stats for logging and downstream use.
	Flagpole FlagpoleStats `json:"flagpole"`
	Flag     FlagStats     `json:"flag"`
}

// HTFWatchlistEntry records a confirmed HTF breakout signal for the CSV watchlist.
// Mirrors WatchlistEntry in the EP strategy.
type HTFWatchlistEntry struct {
	Symbol          string
	BreakoutTime    string
	BreakoutPrice   string
	ResistanceLevel string
	VolumeRatio     string
	FlagpoleGainPct string
	FlagRangePct    string
	Date            string
}
