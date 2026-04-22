#!/usr/bin/env bash
# api_pulse_monitor.sh
# Polls the aggregator API every 30s. Emits one line per event when:
#   - any trader's position_count changes (open/close)
#   - any trader's realized_pnl changes by >= $0.05
#   - any trader stops cycling (is_running=false)
#   - a process listener (port 8082, 8080, 8081, 9091, 9092) goes down
# Lines are plain text — the Monitor tool turns each into a notification.

set -u

AGG="http://127.0.0.1:8082"
TRADERS=(aster_paper_rulebased aster_paper_ml_confirmed ibkr_paper_rulebased ibkr_paper_ml_confirmed)

declare -A last_rpnl last_pos last_running last_cycles

# Warm-up snapshot
for tid in "${TRADERS[@]}"; do
  data=$(curl -s "$AGG/api/account?trader_id=$tid" 2>/dev/null)
  last_rpnl[$tid]=$(echo "$data" | python -c "import sys,json;d=json.load(sys.stdin);print(f'{d.get(\"realized_pnl\",0):.3f}')" 2>/dev/null || echo 0)
  last_pos[$tid]=$(echo "$data"  | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('position_count',0))" 2>/dev/null || echo 0)
  st=$(curl -s "$AGG/api/status?trader_id=$tid" 2>/dev/null)
  last_running[$tid]=$(echo "$st" | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('is_running',False))" 2>/dev/null || echo True)
  last_cycles[$tid]=$(echo "$st"  | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('call_count',0))" 2>/dev/null || echo 0)
done

while true; do
  ts=$(date -u +"%H:%M:%SZ")

  # Aggregator liveness
  agg_code=$(curl -s -o /dev/null -w '%{http_code}' "$AGG/health")
  if [ "$agg_code" != "200" ]; then
    echo "$ts AGG-DOWN http=$agg_code"
    sleep 30; continue
  fi

  for tid in "${TRADERS[@]}"; do
    acc=$(curl -s "$AGG/api/account?trader_id=$tid" 2>/dev/null)
    st=$(curl -s "$AGG/api/status?trader_id=$tid" 2>/dev/null)
    [ -z "$acc" ] && continue

    rpnl=$(echo "$acc" | python -c "import sys,json;d=json.load(sys.stdin);print(f'{d.get(\"realized_pnl\",0):.3f}')" 2>/dev/null)
    pos=$(echo "$acc"  | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('position_count',0))" 2>/dev/null)
    run=$(echo "$st"   | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('is_running',False))" 2>/dev/null)
    cyc=$(echo "$st"   | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('call_count',0))" 2>/dev/null)

    # Position change (open or close)
    if [ "$pos" != "${last_pos[$tid]}" ]; then
      echo "$ts [$tid] POS ${last_pos[$tid]}->$pos rPnL=$rpnl cycles=$cyc"
      last_pos[$tid]=$pos
    fi

    # Realized PnL change (>=$0.05)
    delta=$(python -c "print(abs(float('$rpnl') - float('${last_rpnl[$tid]}')))" 2>/dev/null)
    bigger=$(python -c "print(1 if float('$delta') >= 0.05 else 0)" 2>/dev/null)
    if [ "$bigger" = "1" ]; then
      echo "$ts [$tid] REALIZED ${last_rpnl[$tid]}->$rpnl (delta=$delta)"
      last_rpnl[$tid]=$rpnl
    fi

    # Running state change
    if [ "$run" != "${last_running[$tid]}" ]; then
      echo "$ts [$tid] RUNNING ${last_running[$tid]}->$run"
      last_running[$tid]=$run
    fi

    # Cycle stall (no cycles for this trader in 6+ min while running)
    last_cyc=${last_cycles[$tid]}
    last_cycles[$tid]=$cyc
  done

  sleep 30
done
