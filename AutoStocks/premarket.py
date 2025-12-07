from dotenv import load_dotenv
import os
from alpaca_trade_api.rest import REST, TimeFrame
import pandas as pd
import numpy as np
from datetime import datetime, timedelta

# ==========================
# Load API and Create Client
# ==========================
load_dotenv()
API_KEY = os.getenv("ALPACA_API_KEY")
API_SECRET = os.getenv("ALPACA_SECRET_KEY")
BASE_URL = os.getenv("ALPACA_BASE_URL")

# Debug: Check if credentials are loaded
if not API_KEY or not API_SECRET:
    raise ValueError("API credentials not found. Check your .env file.")


api = REST(key_id=API_KEY, secret_key=API_SECRET, base_url=BASE_URL)

# ==========================
# Fetch Historical Bars
# ==========================
def get_bars(symbol, timeframe="1Day", days_back=60):
    """
    Fetch historical bars using proper date strings.
    Returns a DataFrame with columns: open, high, low, close, volume, trade_count, vwap
    """
    # Calculate date range
    end_date = datetime.now().strftime('%Y-%m-%d')
    start_date = (datetime.now() - timedelta(days=days_back)).strftime('%Y-%m-%d')
    
    print(f"Fetching {symbol} from {start_date} to {end_date}")
    
    # Get bars with proper parameters
    bars = api.get_bars(
        symbol,
        timeframe,
        start=start_date,
        end=end_date,
        adjustment='raw',
        feed='sip'
    )
    
    # Convert to DataFrame
    df = bars.df
    
    if df.empty:
        print(f"Warning: No data returned for {symbol}")
    else:
        print(f"Retrieved {len(df)} bars")
        print(f"Columns: {df.columns.tolist()}")
    
    return df

# ==========================
# SMA / EMA
# ==========================
def calc_sma(series, window=20):
    return series.rolling(window=window).mean()

def calc_ema(series, window=20):
    return series.ewm(span=window, adjust=False).mean()

# ==========================
# Dollar Volume
# ==========================
def calc_dollar_volume(df):
    """dolvol = close * volume"""
    return (df['close'] * df['volume']).mean()

# ==========================
# ADR (Average Daily Range)
# ==========================
def calc_adr(df, window=14):
    """ADR = average(high - low) over N days"""
    return (df['high'] - df['low']).rolling(window).mean().iloc[-1]

# ==========================
# Premarket Data Fetcher
# ==========================
def get_last_trading_day():
    """Get the most recent trading day (skips weekends)."""
    today = datetime.now()
    
    # If it's Saturday (5) or Sunday (6), go back to Friday
    if today.weekday() == 5:  # Saturday
        return today - timedelta(days=1)
    elif today.weekday() == 6:  # Sunday
        return today - timedelta(days=2)
    else:
        return today

def get_premarket_data(symbol):
    """
    Get premarket bars from 4:00–9:30 AM EST for the most recent trading day.
    """
    # Get the last trading day
    trading_day = get_last_trading_day()
    target_date = trading_day.date()
    
    # Create start and end times (in ISO format with timezone)
    # Alpaca expects times in the format: YYYY-MM-DDTHH:MM:SSZ
    start_time = f"{target_date}T04:00:00-05:00"  # 4 AM EST
    end_time = f"{target_date}T09:30:00-05:00"    # 9:30 AM EST
    
    day_name = trading_day.strftime("%A, %B %d, %Y")
    
    print(f"Fetching premarket data for {symbol}")
    print(f"  Trading Day: {day_name}")
    print(f"  From: {start_time}")
    print(f"  To: {end_time}")
    
    try:
        bars = api.get_bars(
            symbol,
            "1Min",
            start=start_time,
            end=end_time,
            adjustment="raw",
            feed="sip"
        )
        
        df = bars.df
        
        if df.empty:
            print("  No premarket data available")
        else:
            print(f"  Retrieved {len(df)} premarket bars")
        
        return df
    except Exception as e:
        print(f"  Error fetching premarket data: {e}")
        return pd.DataFrame()

# ==========================
# Run Tests
# ==========================
def analyze_symbol(symbol):
    print(f"\n{'='*50}")
    print(f"  ANALYZING {symbol}")
    print(f"{'='*50}")

    # Fetch daily bars
    df = get_bars(symbol, timeframe="1Day", days_back=90)

    # Check if DataFrame is empty
    if df.empty:
        print("No data returned for symbol")
        return None

    # SMA / EMA
    df["SMA20"] = calc_sma(df["close"], 20)
    df["EMA20"] = calc_ema(df["close"], 20)

    # ADR
    adr_val = calc_adr(df)

    # Dollar Volume
    dollar_volume = calc_dollar_volume(df)

    # Premarket
    premarket = get_premarket_data(symbol)

    # Print Results
    print(f"\n{'─'*50}")
    print("DAILY DATA")
    print(f"{'─'*50}")
    print(f"  Latest Close:      ${df['close'].iloc[-1]:.2f}")
    print(f"  SMA20:            ${df['SMA20'].iloc[-1]:.2f}")
    print(f"  EMA20:            ${df['EMA20'].iloc[-1]:.2f}")
    print(f"  Dollar Volume:     ${dollar_volume:,.0f}")
    print(f"  ADR (14-day):     ${adr_val:.2f}")
    
    if not premarket.empty:
        pm_open = premarket['open'].iloc[0]
        pm_high = premarket['high'].max()
        pm_low = premarket['low'].min()
        pm_close = premarket['close'].iloc[-1]
        pm_volume = premarket['volume'].sum()
        pm_change = ((pm_close - pm_open) / pm_open) * 100
        
        print(f"\n{'─'*50}")
        print("PREMARKET DATA")
        print(f"{'─'*50}")
        print(f"  Bars:             {len(premarket)}")
        print(f"  Open:             ${pm_open:.2f}")
        print(f"  High:             ${pm_high:.2f}")
        print(f"  Low:              ${pm_low:.2f}")
        print(f"  Current:          ${pm_close:.2f}")
        print(f"  Change:           {pm_change:+.2f}%")
        print(f"  Volume:           {pm_volume:,.0f}")
    else:
        print(f"\n{'─'*50}")
        print(" PREMARKET DATA")
        print(f"{'─'*50}")
        print("  No premarket data available")

    return {
        "df": df,
        "premarket": premarket,
        "adr": adr_val,
        "dolvol": dollar_volume,
        "sma20": df["SMA20"].iloc[-1],
        "ema20": df["EMA20"].iloc[-1]
    }

# ==========================
# Example Usage
# ==========================
if __name__ == "__main__":
    # Test account connection first
    try:
        account = api.get_account()
        print(f"\nAccount connected:")
        print(f"  Status: {account.status}")
        print(f"  Buying Power: ${float(account.buying_power):,.2f}")
    except Exception as e:
        print(f"\n✗ Account connection failed: {e}")
        print("\nTroubleshooting steps:")
        print("1. Verify your .env file has the correct variables:")
        print("   ALPACA_API_KEY=your_key_here")
        print("   ALPACA_SECRET_KEY=your_secret_here")
        print("   ALPACA_BASE_URL=https://paper-api.alpaca.markets")
        print("\n2. Make sure you're using the correct URL:")
        print("   - Paper trading: https://paper-api.alpaca.markets")
        print("   - Live trading: https://api.alpaca.markets")
        print("\n3. Check that your API keys are active in your Alpaca dashboard")
        exit(1)
    
    # Analyze symbols
    analyze_symbol("AAPL")
    analyze_symbol("TSLA")