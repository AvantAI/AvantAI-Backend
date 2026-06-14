#!/bin/bash
###############################################################################
# Configuration
###############################################################################
export LC_ALL=C
LOG_FILE="./backtest_execution.log"
START_DATE="2026-05-22"
END_DATE="2026-05-22"
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
# Start WebSocket dashboard server (runs for the lifetime of this script)
###############################################################################
log_message "Starting WebSocket dashboard server..."
LOG_FILE="$LOG_FILE" \
WATCHLIST_CSV="./watchlist.csv" \
TRADES_CSV="./traderesults.csv" \
PORT="32768" \
    go run ws_server.go >> "$LOG_FILE" 2>&1 &
PID_WS=$!

# Give the server a moment to bind the port before backtests start
sleep 2

if ! kill -0 "$PID_WS" 2>/dev/null; then
    log_message "ERROR: WebSocket server failed to start (PID $PID_WS) — continuing without it"
else
    log_message "OK: WebSocket server running on :32768 (PID $PID_WS)"
fi

# Ensure the WS server is killed when this script exits for any reason
trap 'log_message "Shutting down WebSocket server (PID $PID_WS)..."; kill "$PID_WS" 2>/dev/null' EXIT
###############################################################################
# Main loop (epoch-based — unbreakable)
###############################################################################
log_message "Starting backtests from $START_DATE to $END_DATE"
log_message "Detected OS: $OSTYPE"
log_message "Main script PID: $$"

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

    # --- Step 1: Data Gathering ---
    go run cmd/avantai/ep/data_gathering/ep_manual_data.go --date "$current_date" \
        >> "$LOG_FILE" 2>&1 &
    PID_DATA=$!
    wait $PID_DATA
    if [[ $? -ne 0 ]]; then
        log_message "ERROR: Data gathering failed for $current_date"
    else
        log_message "OK: Data gathering completed for $current_date"
    fi

    # --- Step 2: Worker Agent Backtest ---
    go run cmd/avantai/ep/worker-agents/ep_worker_agent_backtest.go --date "$current_date" \
        >> "$LOG_FILE" 2>&1 &
    PID_WORKER=$!
    wait $PID_WORKER
    if [[ $? -ne 0 ]]; then
        log_message "ERROR: Worker agent backtest failed for $current_date"
    else
        log_message "OK: Worker agent completed for $current_date"
    fi

    # --- Step 3: Main Backtest ---
    go run cmd/avantai/ep/ep_main/ep_main_alpaca_backtest.go --date "$current_date" \
        >> "$LOG_FILE" 2>&1 &
    PID_MAIN=$!
    wait $PID_MAIN
    if [[ $? -ne 0 ]]; then
        log_message "ERROR: Main backtest failed for $current_date"
    else
        log_message "OK: Main backtest completed for $current_date"
    fi

    current_ts=$((current_ts + 86400))
done

log_message "All weekday backtest sequences completed."