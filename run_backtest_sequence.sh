#!/bin/bash

# Path to the file containing dates (one per line)
DATES_FILE="./dates.txt"

# Log file for tracking execution
LOG_FILE="./backtest_execution.log"

# Function to log messages
log_message() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Check if dates file exists
if [ ! -f "$DATES_FILE" ]; then
    log_message "ERROR: Dates file not found: $DATES_FILE"
    exit 1
fi

# Read dates file line by line
while IFS= read -r target_date || [ -n "$target_date" ]; do
    # Skip empty lines and comments
    [[ -z "$target_date" || "$target_date" =~ ^[[:space:]]*# ]] && continue
    
    log_message "Starting backtest sequence for date: $target_date"
    
    # Command 1: Data gathering
    log_message "Running data gathering for $target_date"
    if go run cmd/avantai/ep/data_gathering/ep_manual_data.go --date $target_date; then
        log_message "Data gathering completed successfully for $target_date"
    else
        log_message "ERROR: Data gathering failed for $target_date"
        continue  # Skip to next date if this step fails
    fi
    
    # Command 2: Worker agent backtest
    log_message "Running worker agent backtest for $target_date"
    if go run cmd/avantai/ep/worker-agents/ep_worker_agent_backtest.go --date $target_date; then
        log_message "Worker agent backtest completed successfully for $target_date"
    else
        log_message "ERROR: Worker agent backtest failed for $target_date"
        continue  # Skip to next date if this step fails
    fi
    
    # Command 3: Main backtest
    log_message "Running main backtest for $target_date"
    if go run cmd/avantai/ep/ep_main/ep_main_backtest.go --date $target_date; then
        log_message "Main backtest completed successfully for $target_date"
    else
        log_message "ERROR: Main backtest failed for $target_date"
        continue  # Skip to next date if this step fails
    fi
    
    log_message "Completed all commands for date: $target_date"
    log_message "----------------------------------------"
    
done < "$DATES_FILE"

log_message "All backtest sequences completed"