package htf

// Component 3: HTF Pattern Recognition Engine
//
// This file implements the rule-based intraday detection of when HTF breakout
// criteria are met. It receives one intraday bar at a time and updates a
// running IntradayState, emitting a BreakoutSignal when all conditions are met.
//
// This component ONLY detects and signals — it makes NO trade decisions.
// Entry/exit execution, position sizing, and risk management are out of scope.
//
// Breakout criteria (all must be met):
//   1. Price's intraday high crosses above the flag's upper resistance level.
//   2. The crossing bar's volume is >= BreakoutVolumeMinMultiplier * avgBarVolume.
//   3. Price closes above resistance for BreakoutConfirmationBars consecutive bars
//      (false-breakout protection — if it dips back below, the count resets).
//   4. Optionally: breakout occurs in the first half of the trading day.

import (
	"fmt"
	"time"
)

// barsPerSession is the approximate number of 1-minute bars in a regular
// US equity trading session (9:30–16:00 = 390 minutes).
// Used to convert the daily average share volume into a per-bar average.
const barsPerSession = 390.0

// NewIntradayState creates a fresh IntradayState for the given candidate,
// ready to process the first bar of the trading day.
func NewIntradayState(candidate HTFCandidate) *IntradayState {
	return &IntradayState{
		Candidate: candidate,
		Status:    StatusWatching,
	}
}

// UpdateState processes a single intraday bar and updates the IntradayState.
//
// Parameters:
//   - barHigh:   the high price of the current bar
//   - barClose:  the close price of the current bar
//   - barVolume: the share volume of the current bar
//   - barTime:   the timestamp of the current bar (in the exchange's local timezone)
//
// Returns a *BreakoutSignal if an HTF breakout is confirmed on this bar,
// or nil if no signal is ready yet. Once a signal is returned (StatusTriggered),
// subsequent calls for this state are no-ops.
//
// This is the primary entry point for the pattern recognition engine.
func UpdateState(state *IntradayState, barHigh, barClose, barVolume float64, barTime time.Time) *BreakoutSignal {
	// Once triggered or invalidated, this state is terminal — do nothing.
	if state.Status == StatusTriggered || state.Status == StatusInvalidated {
		return nil
	}

	// ---- Invalidation check ----
	// If the close breaks below the flag support level, the HTF pattern is
	// considered broken for the day. No further breakout can be detected.
	if barClose < state.Candidate.SupportLevel {
		fmt.Printf("[HTF Pattern] [%s] Pattern INVALIDATED at %s: close $%.2f broke below support $%.2f\n",
			state.Candidate.Symbol,
			barTime.Format("15:04"),
			barClose,
			state.Candidate.SupportLevel,
		)
		state.Status = StatusInvalidated
		return nil
	}

	// ---- First-half-of-day filter ----
	// When BreakoutFirstHalfOnly is enabled, ignore bars after the cutoff hour.
	// This filters out low-conviction, late-day breakouts.
	if BreakoutFirstHalfOnly && barTime.Hour() >= BreakoutFirstHalfEndHour {
		return nil
	}

	state.LatestPrice = barClose

	// ---- Per-bar average volume ----
	// The average per-bar volume is the daily average divided by the number
	// of bars in a session. This normalises intraday volume for comparison.
	avgBarVolume := state.Candidate.AvgShareVolume / barsPerSession

	resistance := state.Candidate.ResistanceLevel

	// ---- Phase 1: No breakout detected yet — watch for the first breakout bar ----
	if state.BreakoutFirstDetectedAt.IsZero() {
		// Check whether this bar's high crossed above the resistance level.
		if barHigh <= resistance {
			return nil
		}

		// The bar crossed resistance — now validate the volume.
		// The breakout bar must have meaningfully above-average volume to confirm
		// genuine buying pressure rather than a low-volume drift through the level.
		volRatio := 0.0
		if avgBarVolume > 0 {
			volRatio = barVolume / avgBarVolume
		}

		if volRatio < BreakoutVolumeMinMultiplier {
			// Volume insufficient — not a confirmed breakout bar. Keep watching.
			fmt.Printf("[HTF Pattern] [%s] Bar at %s crossed resistance $%.2f but volume %.1fx < %.1fx minimum — watching\n",
				state.Candidate.Symbol,
				barTime.Format("15:04"),
				resistance,
				volRatio,
				BreakoutVolumeMinMultiplier,
			)
			return nil
		}

		// First qualifying breakout bar detected.
		state.Status = StatusSettingUp
		state.BreakoutFirstDetectedAt = barTime
		state.BreakoutConfirmationBarsSeen = 1
		state.HighestBarVolume = barVolume

		strengthLabel := "strong"
		if volRatio >= BreakoutVolumeStrongMultiplier {
			strengthLabel = "VERY STRONG"
		}
		fmt.Printf("[HTF Pattern] [%s] First breakout bar at %s: high $%.2f > resistance $%.2f, volume %.1fx avg (%s) [%d/%d confirmation bars]\n",
			state.Candidate.Symbol,
			barTime.Format("15:04"),
			barHigh,
			resistance,
			volRatio,
			strengthLabel,
			state.BreakoutConfirmationBarsSeen,
			BreakoutConfirmationBars,
		)
		return nil
	}

	// ---- Phase 2: Breakout bar detected — confirm price is holding above resistance ----

	// False-breakout protection: if the close dips back below resistance before
	// we accumulate BreakoutConfirmationBars, the breakout is reset.
	if barClose < resistance {
		fmt.Printf("[HTF Pattern] [%s] False breakout reset at %s: close $%.2f < resistance $%.2f\n",
			state.Candidate.Symbol,
			barTime.Format("15:04"),
			barClose,
			resistance,
		)
		state.BreakoutFirstDetectedAt = time.Time{}
		state.BreakoutConfirmationBarsSeen = 0
		state.HighestBarVolume = 0
		state.Status = StatusWatching
		return nil
	}

	// Price closed above resistance — this is a confirmation bar.
	state.BreakoutConfirmationBarsSeen++
	if barVolume > state.HighestBarVolume {
		state.HighestBarVolume = barVolume
	}

	fmt.Printf("[HTF Pattern] [%s] Confirmation bar %d/%d at %s: close $%.2f above resistance $%.2f\n",
		state.Candidate.Symbol,
		state.BreakoutConfirmationBarsSeen,
		BreakoutConfirmationBars,
		barTime.Format("15:04"),
		barClose,
		resistance,
	)

	// ---- Phase 3: Check if we have enough confirmation bars ----
	if state.BreakoutConfirmationBarsSeen < BreakoutConfirmationBars {
		return nil
	}

	// All criteria met — emit the breakout signal.
	state.Status = StatusTriggered

	volRatio := 0.0
	if avgBarVolume > 0 {
		volRatio = state.HighestBarVolume / avgBarVolume
	}

	signal := &BreakoutSignal{
		Symbol:           state.Candidate.Symbol,
		BreakoutTime:     state.BreakoutFirstDetectedAt,
		BreakoutPrice:    barClose,
		ResistanceLevel:  resistance,
		BreakoutVolume:   state.HighestBarVolume,
		AvgDailyVolume:   state.Candidate.AvgShareVolume,
		VolumeRatio:      volRatio,
		ConfirmationBars: state.BreakoutConfirmationBarsSeen,
		Flagpole:         state.Candidate.Flagpole,
		Flag:             state.Candidate.Flag,
	}

	fmt.Printf("[HTF Pattern] [%s] *** BREAKOUT CONFIRMED *** at %s | price $%.2f | resistance $%.2f | volume %.1fx avg | %d confirmation bars | pole: %.1f%% in %d days\n",
		state.Candidate.Symbol,
		state.BreakoutFirstDetectedAt.Format("15:04"),
		barClose,
		resistance,
		volRatio,
		state.BreakoutConfirmationBarsSeen,
		state.Candidate.Flagpole.GainPct,
		state.Candidate.Flagpole.DurationTradingDays,
	)

	return signal
}
