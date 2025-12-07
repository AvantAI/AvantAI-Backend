def check_all_orders():
    """
    Retrieves and displays all open orders with detailed information.
    
    Returns:
        list: List of all open orders
    """
    orders = api.list_orders(status="open")
    print(f"\n=== All Open Orders ({len(orders)}) ===")
    for order in orders:
        print(f"\nOrder ID: {order.id}")
        print(f"  Symbol: {order.symbol}")
        print(f"  Side: {order.side}")
        print(f"  Type: {order.type}")
        print(f"  Qty: {order.qty}")
        print(f"  Status: {order.status}")
        print(f"  Order Class: {order.order_class if hasattr(order, 'order_class') else 'N/A'}")
        if hasattr(order, 'limit_price') and order.limit_price:
            print(f"  Limit Price: ${order.limit_price}")
        if hasattr(order, 'stop_price') and order.stop_price:
            print(f"  Stop Price: ${order.stop_price}")
    return orders


from dotenv import load_dotenv
import os
from alpaca_trade_api.rest import REST
import time


load_dotenv()
API_KEY = os.getenv("ALPACA_API_KEY")
API_SECRET = os.getenv("ALPACA_SECRET_KEY")
BASE_URL = os.getenv("ALPACA_BASE_URL")  

api = REST(API_KEY, API_SECRET, BASE_URL)


def get_asset(symbol: str):
    """
    Retrieve the Alpaca Asset object for a given symbol and ensure it is tradable.

    Parameters:
        symbol (str): The stock ticker symbol to check.

    Returns:
        Asset: Alpaca Asset object.

    Raises:
        ValueError: If the asset is not tradable.
    """
    asset = api.get_asset(symbol)
    if not asset.tradable:
        raise ValueError(f"{symbol} is not tradable")
    return asset


def cancel_all_orders():
    """
    Cancels all open orders in the Alpaca account.

    Returns:
        None
    """
    open_orders = api.list_orders(status="open")
    for order in open_orders:
        api.cancel_order(order.id)
    print(f"Cancelled {len(open_orders)} open orders.")


def liquidate_all_positions():
    """
    Sells all positions in the Alpaca account at market price.

    Returns:
        None
    """
    positions = api.list_positions()
    for p in positions:
        print(f"Liquidating {p.symbol}: {p.qty} shares (side: {p.side})")
        api.submit_order(
            symbol=p.symbol,
            qty=abs(float(p.qty)),  # Use absolute value
            side="sell" if float(p.qty) > 0 else "buy",  # Sell if long, buy if short
            type="market",
            time_in_force="day"
        )
    print(f"Liquidated {len(positions)} positions.")


def get_account_info():
    """
    Retrieves key account information: status, cash, buying power, portfolio value.

    Returns:
        dict: Dictionary containing account info.
    """
    account = api.get_account()
    return {
        "status": account.status,
        "cash": account.cash,
        "buying_power": account.buying_power,
        "portfolio_value": account.portfolio_value
    }


def place_entry_with_stop(symbol: str, stop_loss: float, shares: int, entry_price: float | None = None):
    """
    Places a bracket buy order with:
      - Entry at $1 above current market price (if entry_price not specified)
      - Stop-loss at `stop_loss`
      - Take-profit at 7× entry price
      - Optional limit price; if None, uses market price + $1

    Parameters:
        symbol (str): Stock ticker symbol to buy
        stop_loss (float): Stop-loss price
        shares (int): Number of shares to buy
        entry_price (float | None): Optional limit price. Defaults to market price + $1.

    Returns:
        Order: The submitted Alpaca bracket order object
    """
    asset = get_asset(symbol)

    # Fetch current market price if entry_price not provided
    if entry_price is None:
        latest_trade = api.get_latest_trade(symbol)
        market_price = float(latest_trade.price)
        entry_price = market_price + 0.1  # Buy at $1 above market
    else:
        market_price = entry_price - 0.1  # Calculate what market was

    print(f"Market price for {symbol}: ${market_price:.2f}")
    print(f"Entry price (market + $1): ${entry_price:.2f}")
    print(f"Stop loss: ${stop_loss:.2f}")
    
    take_profit_price = round(entry_price * 7, 2)  # Use entry_price instead of market_price
    print(f"Take profit: ${take_profit_price:.2f}")

    print(f"\n=== Submitting Order ===")
    print(f"Symbol: {symbol}")
    print(f"Qty: {shares}")
    print(f"Side: buy")
    print(f"Type: limit")
    print(f"Limit Price: ${round(entry_price, 2)}")
    print(f"Stop Loss: ${round(stop_loss, 2)}")
    print(f"Take Profit: ${take_profit_price}")

    order = api.submit_order(
        symbol=symbol,
        qty=shares,
        side="buy",
        type="limit",
        time_in_force="day",
        limit_price=round(entry_price, 2),
        order_class="bracket",
        stop_loss={"stop_price": round(stop_loss, 2)},
        take_profit={"limit_price": take_profit_price}
    )

    print(f"\n=== Order Response ===")
    print(f"Order ID: {order.id}")
    print(f"Side: {order.side}")
    print(f"Symbol: {order.symbol}")
    print(f"Status: {order.status}")
    print(f"Order Class: {order.order_class if hasattr(order, 'order_class') else 'N/A'}")
    
    # Check if there are legs (child orders for bracket)
    if hasattr(order, 'legs') and order.legs:
        print(f"\n=== Bracket Legs ===")
        for i, leg in enumerate(order.legs):
            print(f"Leg {i+1}: {leg.side} @ {leg.type} - {leg.order_class if hasattr(leg, 'order_class') else 'N/A'}")
    
    return order


def place_sell_order(symbol: str, shares: int, sell_price=None):
    """
    Places a sell order for a given symbol, either market or limit.

    Parameters:
        symbol (str): The stock ticker symbol to sell.
        shares (int): Number of shares to sell.
        sell_price (float, optional): Limit price for the sell. Defaults to None (market order).

    Returns:
        Order: The submitted Alpaca order object.
    """
    get_asset(symbol)

    if sell_price is None:
        order = api.submit_order(
            symbol=symbol,
            qty=shares,
            side="sell",
            type="market",
            time_in_force="day"
        )
    else:
        order = api.submit_order(
            symbol=symbol,
            qty=shares,
            side="sell",
            type="limit",
            time_in_force="day",
            limit_price=sell_price
        )
    return order

# ----------------------
# Main Test Routine
# ----------------------
if __name__ == "__main__":
    print("API_KEY loaded:", bool(API_KEY))
    print("API_SECRET loaded:", bool(API_SECRET))
    print("BASE_URL:", BASE_URL)
    
    print("\n=== Account Info ===")
    print(get_account_info())

    # Clean slate for testing
    print("\n=== Cleaning Up ===")
    cancel_all_orders()
    liquidate_all_positions()
    time.sleep(3)  # Wait longer for liquidations to process

    # Check positions are cleared
    print("\n=== Checking Positions ===")
    positions = api.list_positions()
    print(f"Current positions: {len(positions)}")
    for p in positions:
        print(f"  {p.symbol}: {p.qty} shares")

    # Submit a limit buy with stop-loss
    print("\n=== Placing Order ===")
    symbol = "AAPL"
    entry_price = None
    stop_loss = 100
    shares = 50

    try:
        buy_order = place_entry_with_stop(
            symbol=symbol,
            stop_loss=stop_loss,
            shares=shares,
            entry_price=entry_price
        )

        print(f"\n✓ Buy order submitted successfully")
        
        # Wait a moment for the order to be processed
        time.sleep(2)
        
        # Check all orders in the account
        check_all_orders()
        
    except Exception as e:
        print(f"\n✗ Error submitting buy order: {e}")
        import traceback
        traceback.print_exc()