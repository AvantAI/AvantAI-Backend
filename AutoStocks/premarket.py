from dotenv import load_dotenv
import os
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
import pytz

# Alpaca-py imports
from alpaca.data import StockHistoricalDataClient
from alpaca.data.requests import StockBarsRequest, StockTradesRequest, StockLatestTradeRequest
from alpaca.data.timeframe import TimeFrame
from alpaca.trading.client import TradingClient

# ==========================
# Load API Keys
# ==========================
load_dotenv()
API_KEY = os.getenv("ALPACA_API_KEY")
API_SECRET = os.getenv("ALPACA_SECRET_KEY")

if not API_KEY or not API_SECRET:
    raise ValueError("API credentials not found. Check your .env file.")

# Trading (account info/orders)
trading_client = TradingClient(API_KEY, API_SECRET, paper=True)

# Market Data
data_client = StockHistoricalDataClient(API_KEY, API_SECRET)

# ==========================
# Historical Bars (Daily)
# ==========================
def get_bars(symbol, days_back=60):
    end = datetime.now()
    start = end - timedelta(days=days_back)

    print(f"Fetching {symbol} from {start.date()} to {end.date()}")

    req = StockBarsRequest(
        symbol_or_symbols=symbol,
        timeframe=TimeFrame.Day,
        start=start,
        end=end,
    )

    bars = data_client.get_stock_bars(req)
    df = bars.df

    if df.empty:
        print("No data returned.")
    else:
        print(f"Retrieved {len(df)} bars.")

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
    return (df["close"] * df["volume"]).mean()

# ==========================
# ADR (Average Daily Range)
# ==========================
def calc_adr(df, window=14):
    return (df["high"] - df["low"]).rolling(window).mean().iloc[-1]

# ==========================
# Get last trading day
# ==========================
def get_last_trading_day():
    today = datetime.now()
    weekday = today.weekday()

    if weekday == 5:   # Saturday
        return today - timedelta(days=1)
    elif weekday == 6: # Sunday
        return today - timedelta(days=2)
    else:
        return today

# ==========================
# Premarket Data (4:00–9:30 AM EST)
# ==========================
def get_premarket_data(symbol):
    eastern = pytz.timezone("US/Eastern")
    trading_day = get_last_trading_day().date()

    start = eastern.localize(datetime.combine(trading_day, datetime.min.time()) + timedelta(hours=4))
    end = eastern.localize(datetime.combine(trading_day, datetime.min.time()) + timedelta(hours=9, minutes=30))

    print(f"\nFetching PREMARKET for {symbol}")
    print(f"  From: {start}")
    print(f"  To:   {end}")

    req = StockBarsRequest(
        symbol_or_symbols=symbol,
        timeframe=TimeFrame.Minute,
        start=start,
        end=end,
    )

    bars = data_client.get_stock_bars(req)
    df = bars.df

    if df.empty:
        print("  No premarket bars.")
    else:
        print(f"  Retrieved {len(df)} bars.")

    return df

# ==========================
# ANALYSIS FUNCTION
# ==========================
def analyze_symbol(symbol):
    print(f"\n{'=' * 50}")
    print(f"  ANALYZING {symbol}")
    print(f"{'=' * 50}")

    df = get_bars(symbol, days_back=90)

    if df.empty:
        print("No historical data.")
        return

    # Indicators
    df["SMA20"] = calc_sma(df["close"], 20)
    df["EMA20"] = calc_ema(df["close"], 20)
    adr_val = calc_adr(df)
    dollar_volume = calc_dollar_volume(df)

    # Premarket
    pm = get_premarket_data(symbol)

    print(f"\n{'─'*50}")
    print("DAILY DATA")
    print(f"{'─'*50}")
    print(f"  Latest Close:   ${df['close'].iloc[-1]:.2f}")
    print(f"  SMA20:          ${df['SMA20'].iloc[-1]:.2f}")
    print(f"  EMA20:          ${df['EMA20'].iloc[-1]:.2f}")
    print(f"  Dollar Volume:  ${dollar_volume:,.0f}")
    print(f"  ADR(14):        ${adr_val:.2f}")

    # Premarket stats
    print(f"\n{'─'*50}")
    print("PREMARKET DATA")
    print(f"{'─'*50}")

    if pm.empty:
        print("  No premarket data available.")
    else:
        pm_open  = pm["open"].iloc[0]
        pm_high  = pm["high"].max()
        pm_low   = pm["low"].min()
        pm_close = pm["close"].iloc[-1]
        pm_volume = pm["volume"].sum()
        pm_change = ((pm_close - pm_open) / pm_open) * 100

        print(f"  Bars:     {len(pm)}")
        print(f"  Open:     ${pm_open:.2f}")
        print(f"  High:     ${pm_high:.2f}")
        print(f"  Low:      ${pm_low:.2f}")
        print(f"  Close:    ${pm_close:.2f}")
        print(f"  Change:   {pm_change:+.2f}%")
        print(f"  Volume:   {pm_volume:,}")

# ==========================
# MAIN
# ==========================
if __name__ == "__main__":
    # Test account connection
    try:
        account = trading_client.get_account()
        print("\nAccount Connected:")
        print("  Status:", account.status)
        print("  Buying Power:", account.buying_power)
    except Exception as e:
        print("Account connection failed:", e)
        exit(1)

    analyze_symbol("AAUS")
    analyze_symbol("PR")