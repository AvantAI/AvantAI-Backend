#!/bin/bash

LOG="./daily_trading_run.log"

CMD_PREMARKET="go run cmd/avantai/ep/pre-market/ep_premarket.go"
CMD_WORKER="go run cmd/avantai/ep/worker-agents/ep_worker_agent_main.go"
CMD_MAIN="go run cmd/avantai/ep/ep_main/ep_main_alpaca.go"

# ---------------------------------------------
# Logging helper
# FIX 1: Removed tee — causes SIGPIPE when the
# launching terminal closes under nohup, which
# silently kills the script. Since we redirect
# stdout/stderr to the log file at launch time
# (nohup bash script.sh >> log 2>&1 &), plain
# echo is sufficient and safe.
# ---------------------------------------------
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Catch unexpected exits and log the reason
trap 'log "SCRIPT EXITED unexpectedly at line $LINENO with status $?"' EXIT

# ---------------------------------------------
# OS-aware date helpers
# FIX 2: Force base-10 arithmetic with 10# prefix.
# Zero-padded values like 08 or 09 are treated as
# invalid octal by bash's $(( )) — 10# prevents that.
# ---------------------------------------------
get_hour()   { echo "10#$(date +'%H')"; }
get_minute() { echo "10#$(date +'%M')"; }
get_second() { echo "10#$(date +'%S')"; }

# ---------------------------------------------
# Sleep until a target time today or tomorrow
# ---------------------------------------------
sleep_until() {
    target="$1"   # HH:MM format

    while true; do
        now_h=$(get_hour)
        now_m=$(get_minute)
        now_s=$(get_second)

        target_h=$((10#$(echo "$target" | cut -d: -f1)))
        target_m=$((10#$(echo "$target" | cut -d: -f2)))

        now_total=$((now_h*60 + now_m))
        target_total=$((target_h*60 + target_m))

        if [ "$now_total" -lt "$target_total" ]; then
            diff_minutes=$((target_total - now_total))
            diff_seconds=$((diff_minutes*60 - now_s))

            log "Sleeping until $target ($diff_seconds seconds)..."
            sleep "$diff_seconds"
            break
        else
            minutes_till_midnight=$(((24*60) - now_total))
            seconds_till_midnight=$((minutes_till_midnight*60 - now_s))

            log "Target time $target already passed today. Sleeping until midnight..."
            sleep "$seconds_till_midnight"
        fi
    done
}

# ---------------------------------------------
# Run a command and wait for it
# ---------------------------------------------
run_step() {
    local label="$1"
    local cmd="$2"

    log "Running $label..."
    $cmd &
    local PID=$!
    wait $PID
    if [[ $? -ne 0 ]]; then
        log "ERROR: $label failed."
    else
        log "OK: $label completed."
    fi
}

# ---------------------------------------------
# Main loop — runs forever, every day
# ---------------------------------------------
log "Daily trading script started. PID: $$"
log "Launch with: nohup bash daily_trading_run.sh >> ./daily_trading_run.log 2>&1 &"

while true; do

    # ---------------------------------------------
    # 6:00 AM — PREMARKET
    # ---------------------------------------------
    sleep_until "06:00"
    run_step "PREMARKET"    "$CMD_PREMARKET"
    run_step "WORKER AGENT" "$CMD_WORKER"

    # ---------------------------------------------
    # 6:30 AM — MAIN PROGRAM
    # ---------------------------------------------
    sleep_until "06:30"
    run_step "MAIN TRADING ENGINE" "$CMD_MAIN"

    log "Finished today. Waiting for tomorrow..."

done