#!/bin/bash
###############################################################################
# Configuration
###############################################################################
export LC_ALL=C
LOG_FILE="./backtest_execution.log"
START_DATE="2025-01-02"
END_DATE="2025-01-06"
###############################################################################
# Logging
###############################################################################
log_message() {
    echo "[$(/bin/date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}
###############################################################################
# OS-aware date helpers
###############################################################################
date_to_epoch() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        /bin/date -j -f "%Y-%m-%d" "$1" "+%s"
    else
        date -d "$1" "+%s"
    fi
}

epoch_to_date() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        /bin/date -j -f "%s" "$1" "+%Y-%m-%d"
    else
        date -d "@$1" "+%Y-%m-%d"
    fi
}

epoch_to_dow() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        /bin/date -j -f "%s" "$1" "+%u"
    else
        date -d "@$1" "+%u"
    fi
}
###############################################################################
# Convert dates to epoch ONCE
###############################################################################
start_ts=$(date_to_epoch "$START_DATE") || exit 1
end_ts=$(date_to_epoch "$END_DATE")     || exit 1
###############################################################################
# Main loop (epoch-based — unbreakable)
###############################################################################
log_message "Starting backtests from $START_DATE to $END_DATE"
log_message "Detected OS: $OSTYPE"

current_ts="$start_ts"
while [[ "$current_ts" -le "$end_ts" ]]; do
    current_date=$(epoch_to_date "$current_ts")
    dow=$(epoch_to_dow "$current_ts")

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

    current_ts=$((current_ts + 86400))
done

log_message "All weekday backtest sequences completed."
