import { useMemo } from 'react';
import type { DecisionRecord } from '../types';

export function StrategyComplianceMonitor({ records }: { records?: DecisionRecord[] }) {
    const complianceData = useMemo(() => {
        if (!records || records.length === 0) return [];

        // Look at the latest cycles
        return records.slice(0, 15).flatMap(record => {
            let expectedDecisions: any = [];
            try {
                if (!record.decision_json) return [];
                expectedDecisions = JSON.parse(record.decision_json);
                if (expectedDecisions.decisions) {
                    expectedDecisions = expectedDecisions.decisions;
                } else if (!Array.isArray(expectedDecisions)) {
                    expectedDecisions = [expectedDecisions];
                }
            } catch (e) {
                return [];
            }

            const executedActions = record.decisions || [];

            // Filter out 'hold' and 'wait' to just see actionable intents,
            // OR show cases where it actually executed something
            const intents = expectedDecisions.filter((d: any) =>
                d && d.action && typeof d.action === 'string' &&
                !['hold', 'wait', 'none'].includes(d.action.toLowerCase())
            );

            const results = intents.map((intent: any) => {
                const actualAction = executedActions.find(a => a.symbol === intent.symbol);
                const match = actualAction && actualAction.action === intent.action && actualAction.success;

                return {
                    cycle: record.cycle_number,
                    symbol: intent.symbol,
                    expectedAction: intent.action,
                    actualAction: actualAction ? (actualAction.success ? actualAction.action : `Failed: ${actualAction.error}`) : 'No Trade Executed',
                    matched: !!match,
                    confidence: intent.confidence || 0,
                    reasoning: intent.reasoning || ''
                };
            });

            return results;
        });
    }, [records]);

    if (!complianceData || complianceData.length === 0) {
        return null;
    }

    return (
        <div className="binance-card p-6 mt-6 animate-slide-in" style={{ border: '1px solid rgba(14, 203, 129, 0.2)' }}>
            <div className="flex items-center gap-3 mb-5 pb-4 border-b" style={{ borderColor: '#2B3139' }}>
                <div className="w-10 h-10 rounded-xl flex items-center justify-center text-xl" style={{ border: '1px solid rgba(14, 203, 129, 0.4)', background: 'rgba(14, 203, 129, 0.1)' }}></div>
                <div>
                    <h2 className="text-xl font-bold text-[#EAECEF]">Strategy Compliance Monitor</h2>
                    <div className="text-xs text-[#848E9C]">Verifying AI Autonomous Execution & Rules Adherence</div>
                </div>
            </div>

            <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead className="text-left border-b border-[#2B3139]">
                        <tr>
                            <th className="pb-3 text-[#848E9C]">Cycle</th>
                            <th className="pb-3 text-[#848E9C]">Symbol</th>
                            <th className="pb-3 text-[#848E9C]">Expected Strategy (AI Intent)</th>
                            <th className="pb-3 text-[#848E9C]">Actual Execution</th>
                            <th className="pb-3 text-[#848E9C]">Match</th>
                            <th className="pb-3 text-[#848E9C] w-1/3">Reasoning Summary</th>
                        </tr>
                    </thead>
                    <tbody>
                        {complianceData.map((row, i) => (
                            <tr key={i} className="border-b border-[#2B3139] hover:bg-[#1E2329] transition-colors last:border-0">
                                <td className="py-3 font-mono text-[#848E9C]">#{row.cycle}</td>
                                <td className="py-3 font-bold text-[#EAECEF]">{row.symbol}</td>
                                <td className="py-3 font-mono text-[#F0B90B] uppercase text-xs">{row.expectedAction}</td>
                                <td className={`py-3 font-mono uppercase text-xs ${row.matched ? 'text-[#0ECB81]' : (row.actualAction === 'No Trade Executed' ? 'text-[#848E9C]' : 'text-[#F6465D]')}`}>
                                    {row.actualAction}
                                </td>
                                <td className="py-3">
                                    {row.matched ? <span className="text-[#0ECB81]"></span> : <span className="text-[#F6465D]"></span>}
                                </td>
                                <td className="py-3 text-xs text-[#848E9C] truncate max-w-xs cursor-help" title={row.reasoning}>
                                    {row.reasoning}
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
