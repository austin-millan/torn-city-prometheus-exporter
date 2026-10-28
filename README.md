# torn-city-prometheus-exporter

Prometheus exporter for a Torn account, built on [go-torn-client](https://gitlab.com/torn-city/go-torn-client).

## Setup

```bash
cp .env.example .env
# set TORN_API_KEY in .env
```

## Run

```bash
task run
curl -s localhost:9101/metrics | head
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `TORN_API_KEY` | required | Torn API key |
| `TORN_EXPORTER_ADDR` | `:9101` | Listen address |
| `TORN_EXPORTER_INTERVAL` | `60s` | Background scrape interval |
| `TORN_USER_AGENT` | `gitlab.com/torn-city/torn-city-prometheus-exporter` | HTTP User-Agent |
| `TORN_COMMENT` | `gitlab.com/torn-city/torn-city-prometheus-exporter` | Torn API comment |

Metrics include bars, money/points, cooldowns, travel, notifications, refills, battle stats, networth, job, status, and scrape health (`torn_scrape_*`).
