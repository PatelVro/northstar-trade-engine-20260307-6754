import os
import csv
import random
from datetime import datetime, timedelta

def generate_synthetic_data(symbol, start_price, output_dir="./data/csv", periods=11700):
    os.makedirs(output_dir, exist_ok=True)
    filename = os.path.join(output_dir, f"{symbol}.csv")
    
    # 9:30 AM EST start time
    current_time = datetime(2023, 10, 2, 9, 30, 0)
    current_price = start_price
    
    with open(filename, 'w', newline='') as csvfile:
        fieldnames = ['timestamp', 'open', 'high', 'low', 'close', 'volume']
        writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
        writer.writeheader()
        
        for i in range(periods):
            # Random walk with slight overall upward bias
            change_pct = random.uniform(-0.002, 0.0025) 
            open_price = current_price
            close_price = current_price * (1 + change_pct)
            
            # High and low bounds
            high_price = max(open_price, close_price) * (1 + random.uniform(0, 0.001))
            low_price = min(open_price, close_price) * (1 - random.uniform(0, 0.001))
            
            volume = random.randint(1000, 50000)
            
            # Standardize timestamp to millisecond unix time as NOFX expects
            ts_ms = int(current_time.timestamp() * 1000)
            
            writer.writerow({
                'timestamp': ts_ms,
                'open': round(open_price, 4),
                'high': round(high_price, 4),
                'low': round(low_price, 4),
                'close': round(close_price, 4),
                'volume': volume
            })
            
            current_price = close_price
            current_time += timedelta(minutes=1)
            
    print(f"✅ Generated synthetic local CSV data for {symbol}: {periods} 1-min candles.")

if __name__ == "__main__":
    symbols = {
        "AAPL": 170.0,
        "MSFT": 310.0,
        "NVDA": 450.0
    }
    for sym, price in symbols.items():
        generate_synthetic_data(sym, price)
