import { useEffect, useRef, useState } from 'react';
import { createChart, ColorType, IChartApi, ISeriesApi, SeriesMarker, Time } from 'lightweight-charts';
import type { Position, DecisionRecord } from '../types';

interface SymbolChartProps {
    symbol: string;
    traderId: string;
    positions?: Position[];
    decisions?: DecisionRecord[];
    onClose: () => void;
}

export function SymbolChartOverlay({ symbol, traderId, positions, decisions, onClose }: SymbolChartProps) {
    const chartContainerRef = useRef<HTMLDivElement>(null);
    const [chartData, setChartData] = useState<any[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const chartRef = useRef<IChartApi | null>(null);
    const seriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);

    // Fetch candle data
    useEffect(() => {
        let isMounted = true;
        const fetchCandles = async () => {
            setIsLoading(true);
            try {
                const res = await fetch(`/api/candles?trader_id=${traderId}&symbol=${symbol}`);
                if (!res.ok) throw new Error('Failed to fetch candles');
                const data = await res.json();

                if (isMounted && Array.isArray(data)) {
                    // map to lightweight-charts format: { time, open, high, low, close }
                    const formatted = data.map(kline => ({
                        time: (Math.floor(kline.OpenTime / 1000)) as Time,
                        open: kline.Open,
                        high: kline.High,
                        low: kline.Low,
                        close: kline.Close
                    }));

                    // Sort by time just in case
                    formatted.sort((a, b) => (a.time as number) - (b.time as number));

                    // Remove duplicates
                    const unique = formatted.filter((v, i, a) => a.findIndex(t => (t.time === v.time)) === i);

                    setChartData(unique);
                }
            } catch (err) {
                console.error("Error fetching candles:", err);
            } finally {
                if (isMounted) setIsLoading(false);
            }
        };

        fetchCandles();

        // Refresh every minute
        const interval = setInterval(fetchCandles, 60000);
        return () => {
            isMounted = false;
            clearInterval(interval);
        };
    }, [symbol, traderId]);

    // Setup chart chartRef
    useEffect(() => {
        if (!chartContainerRef.current || chartData.length === 0) return;

        const chart = createChart(chartContainerRef.current, {
            layout: {
                background: { type: ColorType.Solid, color: '#161A1E' },
                textColor: '#848E9C',
            },
            grid: {
                vertLines: { color: '#2B3139' },
                horzLines: { color: '#2B3139' },
            },
            width: chartContainerRef.current.clientWidth,
            height: 400,
            timeScale: {
                timeVisible: true,
                secondsVisible: false,
            },
        });

        const candleSeries = (chart as any).addCandlestickSeries({
            upColor: '#0ECB81',
            downColor: '#F6465D',
            borderVisible: false,
            wickUpColor: '#0ECB81',
            wickDownColor: '#F6465D',
        });

        candleSeries.setData(chartData);

        chartRef.current = chart;
        seriesRef.current = candleSeries;

        const handleResize = () => {
            if (chartContainerRef.current) {
                chart.applyOptions({ width: chartContainerRef.current.clientWidth });
            }
        };

        window.addEventListener('resize', handleResize);

        // Fit content
        chart.timeScale().fitContent();

        return () => {
            window.removeEventListener('resize', handleResize);
            chart.remove();
        };
    }, [chartData]); // Recreate chart if data changes heavily

    // Add markers and price lines
    useEffect(() => {
        if (!seriesRef.current || chartData.length === 0) return;

        // 1. Plot Decisions as markers
        const markers: SeriesMarker<Time>[] = [];

        (decisions || []).forEach(record => {
            const time = Math.floor(new Date(record.timestamp).getTime() / 1000) as Time;
            // Check if within chart bounds roughly
            if ((time as number) < (chartData[0].time as number)) return;

            (record.decisions || []).forEach(action => {
                if (action.symbol !== symbol || !action.success) return;

                if (action.action.includes('long') && action.action.includes('open')) {
                    markers.push({ time, position: 'belowBar', color: '#0ECB81', shape: 'arrowUp', text: `Long@${action.price.toFixed(2)}` });
                } else if (action.action.includes('short') && action.action.includes('open')) {
                    markers.push({ time, position: 'aboveBar', color: '#F6465D', shape: 'arrowDown', text: `Short@${action.price.toFixed(2)}` });
                } else if (action.action.includes('close')) {
                    markers.push({ time, position: 'aboveBar', color: '#F0B90B', shape: 'circle', text: 'Close' });
                }
            });
        });

        // Sort markers by time
        markers.sort((a, b) => (a.time as number) - (b.time as number));
        // @ts-ignore
        seriesRef.current.setMarkers(markers);

        // 2. Plot Active Position SL and TP lines
        const pos = positions?.find(p => p.symbol === symbol);
        if (pos && pos.quantity > 0) {
            const activePos = pos as any;
            // Draw entry
            if (activePos.entry_price > 0) {
                seriesRef.current.createPriceLine({
                    price: activePos.entry_price,
                    color: '#60a5fa',
                    lineWidth: 2,
                    lineStyle: 1, // Dotted
                    axisLabelVisible: true,
                    title: `Entry ${activePos.side}`,
                });
            }
            // Draw SL
            if (activePos.stop_loss && activePos.stop_loss > 0) {
                seriesRef.current.createPriceLine({
                    price: activePos.stop_loss,
                    color: '#F6465D',
                    lineWidth: 1,
                    lineStyle: 0,
                    axisLabelVisible: true,
                    title: 'SL',
                });
            }
            // Draw TP
            if (activePos.take_profit && activePos.take_profit > 0) {
                seriesRef.current.createPriceLine({
                    price: activePos.take_profit,
                    color: '#0ECB81',
                    lineWidth: 1,
                    lineStyle: 0,
                    axisLabelVisible: true,
                    title: 'TP',
                });
            }
        }

    }, [chartData, decisions, positions, symbol]);

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-fade-in">
            <div className="w-full max-w-5xl bg-[#161A1E] rounded-xl border border-[#2B3139] shadow-2xl overflow-hidden flex flex-col">
                <div className="flex items-center justify-between p-4 border-b border-[#2B3139] bg-[#1E2329]">
                    <div className="flex items-center gap-3">
                        <div className="w-8 h-8 rounded-full bg-[#F0B90B]/20 hidden sm:flex items-center justify-center text-[#F0B90B] font-bold">
                            {symbol.charAt(0)}
                        </div>
                        <div>
                            <h2 className="text-xl font-bold text-[#EAECEF]">{symbol} Chart</h2>
                            <div className="text-xs text-[#848E9C]">Real-time execution overlay & indicators</div>
                        </div>
                    </div>
                    <button
                        onClick={onClose}
                        className="w-8 h-8 rounded hover:bg-[#2B3139] flex items-center justify-center text-[#848E9C] hover:text-[#EAECEF] transition-colors"
                    >
                        
                    </button>
                </div>

                <div className="p-4 relative">
                    {isLoading && (
                        <div className="absolute inset-0 z-10 flex items-center justify-center bg-[#161A1E]/80">
                            <div className="flex flex-col items-center">
                                <div className="w-8 h-8 border-2 border-[#F0B90B] border-t-transparent rounded-full animate-spin mb-2"></div>
                                <span className="text-sm text-[#848E9C]">Loading Candles...</span>
                            </div>
                        </div>
                    )}
                    <div ref={chartContainerRef} className="w-full h-[400px] relative">
                        {/* Chart will be rendered here */}
                    </div>
                </div>
            </div>
        </div>
    );
}
