#!/bin/bash

LOG="./daily_trading_run.log"

CMD_PREMARKET="go run cmd/avantai/ep/pre-market/ep_premarket.go"
CMD_WORKER="go run cmd/avantai/ep/worker-agents/ep_worker_agent_main.go"
CMD_MAIN="go run cmd/avantai/ep/ep_main/ep_main_alpaca.go"

# ---------------------------------------------
# Logging helper
# ---------------------------------------------
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG"
}

# ---------------------------------------------
# Sleep until a target time today or tomorrow
# macOS safe — no timestamp arithmetic
# ---------------------------------------------
sleep_until() {
    target="$1"   # HH:MM format

    while true; do
        now_h=$(date +"%H")
        now_m=$(date +"%M")
        now_s=$(date +"%S")

        target_h=$(echo "$target" | cut -d: -f1)
        target_m=$(echo "$target" | cut -d: -f2)

        # Current and target in minutes
        now_total=$((now_h*60 + now_m))
        target_total=$((target_h*60 + target_m))

        if [ "$now_total" -lt "$target_total" ]; then
            # Sleep until target time
            diff_minutes=$((target_total - now_total))
            diff_seconds=$((diff_minutes*60 - now_s))

            log "Sleeping until $target ($diff_seconds seconds)..."
            sleep "$diff_seconds"
            break
        else
            # Already passed today → wait until tomorrow
            minutes_till_midnight=$(((24*60) - now_total))
            seconds_till_midnight=$((minutes_till_midnight*60 - now_s))

            log "Target time $target already passed today. Sleeping until midnight..."
            sleep "$seconds_till_midnight"
        fi
    done
}

# ---------------------------------------------
# Main loop — runs forever, every day
# ---------------------------------------------
while true; do

    # ---------------------------------------------
    # 5:30 AM — PREMARKET
    # ---------------------------------------------
    sleep_until "05:30"

    log "Running PREMARKET..."
    if $CMD_PREMARKET; then
        log "Premarket completed."
    else
        log "ERROR: Premarket failed."
    fi

    log "Running WORKER AGENT..."
    if $CMD_WORKER; then
        log "Worker agent completed."
    else
        log "ERROR: Worker agent failed."
    fi

    # ---------------------------------------------
    # 6:30 AM — MAIN PROGRAM
    # ---------------------------------------------
    sleep_until "06:30"

    log "Running MAIN TRADING ENGINE..."
    if $CMD_MAIN; then
        log "Main completed."
    else
        log "ERROR: Main failed."
    fi

    log "Finished today. Waiting for tomorrow..."
done