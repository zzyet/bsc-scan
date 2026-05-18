-- Seed free BSC RPC endpoints
INSERT INTO endpoints (url, rate_limit_per_minute, daily_limit, max_consecutive_failures, max_total_failures, backoff_initial, backoff_max)
VALUES
    ('https://bsc-dataseed.binance.org', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed1.binance.org', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed2.binance.org', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed3.binance.org', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed4.binance.org', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed1.defibit.io', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed2.defibit.io', 25, 100000, 5, 100, 60, 600),
    ('https://rpc.ankr.com/bsc', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed1.ninicoin.io', 25, 100000, 5, 100, 60, 600),
    ('https://bsc-dataseed2.ninicoin.io', 25, 100000, 5, 100, 60, 600)
ON CONFLICT DO NOTHING;

-- Seed USDT contract (BSC BEP-20)
INSERT INTO monitored_contracts (address, name, active)
VALUES ('0x55d398326f99059fF775485246999027B3197955', 'USDT (BSC)', true)
ON CONFLICT DO NOTHING;
