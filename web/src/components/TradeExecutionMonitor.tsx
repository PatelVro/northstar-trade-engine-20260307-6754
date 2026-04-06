import { useMemo } from 'react';
import type { Position, DecisionRecord } from '../types';

export function TradeExecutionMonitor({ positions, decisions, currency = 'USD' }: { positions?: Position[], decisions?: DecisionRecord[], currency?: string }) {

    const recentExecutions = useMemo(() => {
        return (decisions || [])
            .flatMap(d => (d.decisions || []).map(action => ({
                ...action,
                cycle: d.cycle_number,
                timestamp: d.timestamp,
                logs: d.execution_log || []
            })))
            .filter(a => a.action.includes('open') || a.action.includes('close'))
            .slice(0, 10); // show last 10 actionable decisions
    }, [decisions]);

    return (
        <div className="binance-card p-6 mt-6 animate-slide-in" style={{ border: '1px solid rgba(96, 165, 250, 0.2)' }}>
            <div className="flex items-center gap-3 mb-5 pb-4 border-b" style={{ borderColor: '#2B3139' }}>
                <div className="w-10 h-10 rounded-xl flex items-center justify-center text-xl" style={{ border: '1px solid rgba(96, 165, 250, 0.4)', background: 'rgba(96, 165, 250, 0.1)' }}></div>
                <div>
                    <h2 className="text-xl font-bold text-[#EAECEF]">Trade Execution Monitor</h2>
                    <div className="text-xs text-[#848E9C]">Real-time Order Lifecycle Tracking</div>
                </div>
            </div>

            <div className="space-y-4">
                {recentExecutions.length === 0 ? (
                    <div className="text-center py-8 text-[#848E9C] text-sm">No recent trade executions</div>
                ) : (
                    recentExecutions.map((exec, i) => {
                        // Find active position to show realtime PnL
                        const isLong = exec.action.includes('long');
                        const activePos = positions?.find(p => p.symbol === exec.symbol && p.side === (isLong ? 'long' : 'short'));

                        // Graphical Stepper Steps
                        const steps = [
                            { label: 'Order Submitted', done: true },
                            { label: 'Filled', done: exec.success },
                            { label: 'Position Opened', done: exec.success },
                            { label: 'Stop Loss Set', done: exec.success && exec.logs.some(l => l.toLowerCase().includes('stop loss')) },
                            { label: 'Take Profit Set', done: exec.success && exec.logs.some(l => l.toLowerCase().includes('take profit')) }
                        ];

                        return (
                            <div key={i} className="flex flex-col gap-3 p-4 rounded bg-[#1E2329] border border-[#2B3139] transition-all hover:border-[#60a5fa] hover:shadow-[0_0_15px_rgba(96,165,250,0.1)]">

                                {/* Header Row */}
                                <div className="flex justify-between items-center">
                                    <div className="flex items-center gap-3">
                                        <span className="font-bold text-lg text-[#EAECEF]">{exec.symbol}</span>
                                        <span className={`px-2 py-1 rounded text-xs font-bold uppercase tracking-wider ${exec.action.includes('open') ? 'bg-[#0ECB81]/10 text-[#0ECB81]' : 'bg-[#F0B90B]/10 text-[#F0B90B]'}`}>
                                            {exec.action.replace('_', ' ')}
                                        </span>
                                        {exec.leverage > 0 && (
                                            <span className="px-2 py-1 bg-[#1E2329] border border-[#2B3139] text-[#848E9C] rounded text-xs">
                                                {exec.leverage}x
                                            </span>
                                        )}
                                    </div>
                                    <div className="text-xs text-[#848E9C]">
                                        <span className="hidden sm:inline">{new Date(exec.timestamp).toLocaleString()}  </span>
                                        Cycle #{exec.cycle}
                                    </div>
                                </div>

                                {/* Details Grid */}
                                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 bg-[#0B0E11] p-3 rounded mt-1">
                                    <div>
                                        <div className="text-[10px] text-[#848E9C] uppercase tracking-wider mb-1">Status</div>
                                        {exec.success ? (
                                            <div className="font-bold text-[#0ECB81] flex items-center gap-1"> FILLED</div>
                                        ) : (
                                            <div className="font-bold text-[#F6465D] flex items-center gap-1"> FAILED</div>
                                        )}
                                    </div>
                                    {exec.price > 0 && (
                                        <div>
                                            <div className="text-[10px] text-[#848E9C] uppercase tracking-wider mb-1">Entry Price</div>
                                            <div className="font-mono text-[#EAECEF]">{exec.price.toFixed(4)}</div>
                                        </div>
                                    )}
                                    {exec.quantity > 0 && (
                                        <div>
                                            <div className="text-[10px] text-[#848E9C] uppercase tracking-wider mb-1">Size</div>
                                            <div className="font-mono text-[#EAECEF]">{exec.quantity.toFixed(4)}</div>
                                        </div>
                                    )}
                                    {activePos && (
                                        <div>
                                            <div className="text-[10px] text-[#848E9C] uppercase tracking-wider mb-1">Current PnL</div>
                                            <div className={`font-mono font-bold ${activePos.unrealized_pnl >= 0 ? 'text-[#0ECB81]' : 'text-[#F6465D]'}`}>
                                                {activePos.unrealized_pnl >= 0 ? '+' : ''}{activePos.unrealized_pnl.toFixed(2)} {currency}
                                                <span className="text-xs ml-1 opacity-80">({activePos.unrealized_pnl_pct.toFixed(2)}%)</span>
                                            </div>
                                        </div>
                                    )}
                                </div>

                                {/* Graphical Stepper */}
                                <div className="relative pt-4 pb-2 px-2 overflow-x-auto">
                                    <div className="flex justify-between items-center relative min-w-[400px]">
                                        {/* Connecting Line */}
                                        <div className="absolute top-[11px] left-0 w-full h-[2px] bg-[#2B3139] -z-10"></div>
                                        <div className="absolute top-[11px] left-0 h-[2px] bg-[#60a5fa] transition-all duration-1000 -z-10"
                                            style={{ width: `${(steps.filter(s => s.done).length - 1) * (100 / (steps.length - 1))}%` }}></div>

                                        {steps.map((step, idx) => (
                                            <div key={idx} className="flex flex-col items-center gap-2 group">
                                                <div className={`w-6 h-6 rounded-full flex items-center justify-center text-xs transition-colors duration-300 ${step.done
                                                    ? 'bg-[#60a5fa] text-[#0B0E11] shadow-[0_0_10px_rgba(96,165,250,0.5)]'
                                                    : 'bg-[#1E2329] text-[#848E9C] border border-[#2B3139]'
                                                    }`}>
                                                    {step.done ? '' : (idx + 1)}
                                                </div>
                                                <span className={`text-[10px] uppercase font-semibold whitespace-nowrap ${step.done ? 'text-[#60a5fa]' : 'text-[#5E6673]'}`}>
                                                    {step.label}
                                                </span>
                                            </div>
                                        ))}
                                    </div>
                                </div>

                                {/* Error message attachment if failed */}
                                {!exec.success && exec.error && (
                                    <div className="mt-2 text-xs text-[#F6465D] bg-[rgba(246,70,93,0.1)] p-2 rounded">
                                        Error Log: {exec.error}
                                    </div>
                                )}
                            </div>
                        );
                    })
                )}
            </div>
        </div>
    );
}
