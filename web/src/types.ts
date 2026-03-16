export interface SystemStatus {
  trader_id: string;
  trader_name: string;
  ai_model: string;
  is_running: boolean;
  start_time: string;
  runtime_minutes: number;
  call_count: number;
  initial_balance: number;
  scan_interval: string;
  stop_until: string;
  last_reset_time: string;
  ai_provider: string;
  mode: string;
  exchange?: string;
  is_demo_mode?: boolean;
  demo_last_cycle_time?: string;
}

export interface AccountInfo {
  accounting_version: number;
  account_cash: number;
  account_equity: number;
  available_balance: number;
  gross_market_value: number;
  unrealized_pnl: number;
  realized_pnl: number;
  total_pnl: number;
  strategy_initial_capital: number;
  strategy_equity: number;
  strategy_return_pct: number;
  daily_pnl: number;
  position_count: number;
  margin_used: number;
  margin_used_pct: number;
}

export interface Position {
  symbol: string;
  side: string;
  entry_price: number;
  mark_price: number;
  quantity: number;
  leverage: number;
  unrealized_pnl: number;
  unrealized_pnl_pct: number;
  liquidation_price: number;
  margin_used: number;
}

export interface DecisionAction {
  action: string;
  symbol: string;
  quantity: number;
  leverage: number;
  price: number;
  fees_usd: number;
  realized_pnl: number;
  order_id: number;
  timestamp: string;
  success: boolean;
  error?: string;
}

export interface AccountSnapshot {
  accounting_version: number;
  account_cash: number;
  account_equity: number;
  available_balance: number;
  gross_market_value: number;
  unrealized_pnl: number;
  realized_pnl: number;
  total_pnl: number;
  strategy_initial_capital: number;
  strategy_equity: number;
  strategy_return_pct: number;
  daily_pnl: number;
  position_count: number;
  margin_used: number;
  margin_used_pct: number;
  total_balance?: number;
  total_unrealized_profit?: number;
}

export interface DecisionRecord {
  timestamp: string;
  cycle_number: number;
  input_prompt: string;
  cot_trace: string;
  decision_json: string;
  account_state: AccountSnapshot;
  positions: any[];
  candidate_coins: string[];
  decisions: DecisionAction[];
  execution_log: string[];
  success: boolean;
  error_message?: string;
}

export interface Statistics {
  total_cycles: number;
  successful_cycles: number;
  failed_cycles: number;
  total_open_positions: number;
  total_close_positions: number;
}

// 
export interface TraderInfo {
  trader_id: string;
  trader_name: string;
  ai_model: string;
}

export interface CompetitionTraderData {
  trader_id: string;
  trader_name: string;
  ai_model: string;
  account_equity: number;
  strategy_equity: number;
  total_pnl: number;
  strategy_return_pct: number;
  position_count: number;
  margin_used_pct: number;
  call_count: number;
  is_running: boolean;
}

export interface CompetitionData {
  traders: CompetitionTraderData[];
  count: number;
}
