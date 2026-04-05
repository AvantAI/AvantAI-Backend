#!/bin/bash

###############################################################################
# Configuration
###############################################################################

export LC_ALL=C
LOG_FILE="./backtest_execution.log"

START_DATE="2025-01-02"
END_DATE="2025-12-31"

###############################################################################
# Logging
###############################################################################

log_message() {
    echo "[$(/bin/date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

###############################################################################
# Convert dates to epoch ONCE
###############################################################################

start_ts=$(/bin/date -j -f "%Y-%m-%d" "$START_DATE" "+%s") || exit 1
end_ts=$(/bin/date -j -f "%Y-%m-%d" "$END_DATE" "+%s") || exit 1

###############################################################################
# Main loop (epoch-based — unbreakable)
###############################################################################

log_message "Starting backtests from $START_DATE to $END_DATE"

current_ts="$start_ts"

while [[ "$current_ts" -le "$end_ts" ]]; do

    # Convert epoch → YYYY-MM-DD (read-only usage)
    current_date=$(/bin/date -j -f "%s" "$current_ts" "+%Y-%m-%d")

    # Weekday check
    dow=$(/bin/date -j -f "%s" "$current_ts" "+%u")
    if [[ "$dow" -ge 6 ]]; then
        log_message "Skipping weekend: $current_date"
        current_ts=$((current_ts + 86400))
        continue
    fi

    log_message "Processing date: $current_date"

    go run cmd/avantai/ep/data_gathering/ep_manual_data.go --date "$current_date" \
        || log_message "ERROR: Data gathering failed for $current_date"

    go run cmd/avantai/ep/worker-agents/ep_worker_agent_backtest.go --date "$current_date" \
        || log_message "ERROR: Worker agent backtest failed for $current_date"

    go run cmd/avantai/ep/ep_main/ep_main_alpaca_backtest.go --date "$current_date" \
        || log_message "ERROR: Main backtest failed for $current_date"

    # Advance exactly one day (in seconds)
    current_ts=$((current_ts + 86400))

done

log_message "All weekday backtest sequences for 2025 completed."