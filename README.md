# LagRadar

A Kafka consumer lag monitoring and automated root cause analysis (RCA) system built in Go. LagRadar provides partition-level sliding window analytics, multi-cluster management, and an event-driven RCA pipeline backed by Redis Streams.

Designed and built by Zhe Song.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    LagRadar Monitor                     │
│                                                         │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │  Collector   │─▶│  Evaluator   │─▶│  Prometheus   │  │
│  │  (per group) │  │  (sliding    │  │  Metrics      │  │
│  │              │  │   window)    │  │               │  │
│  └──────┬───────┘  └──────┬───────┘  └───────────────┘  │
│         │                 │                              │
│         │           ┌─────▼──────┐                       │
│         │           │ RCA Hook   │                       │
│         │           └─────┬──────┘                       │
│  ┌──────▼───────┐         │                              │
│  │ Cluster      │   ┌─────▼──────────┐                   │
│  │ Manager      │   │ Event Publisher │                   │
│  │ (multi-      │   │ (dedup +       │                   │
│  │  cluster)    │   │  retry)        │                   │
│  └──────────────┘   └─────┬──────────┘                   │
│                           │                              │
└───────────────────────────┼──────────────────────────────┘
                            │
                    ┌───────▼───────┐
                    │ Redis Streams │
                    └───────┬───────┘
                            │
                    ┌───────▼───────┐
                    │ RCA Consumer  │
                    │ (standalone)  │
                    └───────────────┘
```

### Core Components

**Sliding Window Evaluator** — Maintains per-partition offset windows and evaluates consumer health using consumption rate, lag change rate, and trend analysis. Classifies each partition as `ACTIVE`, `LAGGING`, `STALLED`, `STOPPED`, or `EMPTY` with corresponding health assessments.

**Multi-Cluster Manager** — Manages independent collectors per Kafka cluster with per-cluster or global configuration. Supports dynamic cluster addition/removal and concurrent group collection with configurable concurrency limits.

**RCA Event Pipeline** — Status transitions and anomaly conditions (rapid lag increase, consumer stall/stop, recovery, lag cleared) are published to Redis Streams with fingerprint-based deduplication and configurable retry. A standalone RCA consumer reads from the stream with consumer groups, enabling independent scaling and pluggable event handlers.

**Distributed Coordination** (backbone implemented) — Consistent hash sharding (`buraksezer/consistent` + `xxhash`), Redis-backed state store for instance registration, heartbeat, assignment persistence, and partition window storage. Designed for horizontal scaling across multiple LagRadar instances.

---

## Key Technical Decisions

- **Sliding window over point-in-time checks** — Trend detection requires historical context. Each partition maintains a configurable window of offset records, enabling consumption rate calculation and lag trend analysis via rolling averages.
- **Hook-based RCA integration** — The collector's evaluation hook decouples core metric collection from RCA event detection, allowing the RCA pipeline to be enabled/disabled without touching collector logic.
- **Redis Streams for event transport** — Provides persistent, replayable event delivery with consumer group semantics, avoiding tight coupling between the monitor and downstream processors.
- **Consistent hashing for group assignment** — Minimizes reassignment churn when instances join or leave, with configurable partition count, replication factor, and load balance factor.

---

## Metrics Exposed

LagRadar exposes Prometheus metrics at `/metrics`:

| Metric | Description |
|--------|-------------|
| `kafka_consumer_lag` | Current lag per partition |
| `kafka_consumer_status` | Consumer status (active/lagging/stalled/stopped/empty) |
| `kafka_consumer_health` | Health assessment (good/warning/critical) |
| `kafka_consumer_lag_trend` | Lag trend direction (stable/increasing/decreasing) |
| `kafka_consumer_consumption_rate` | Messages consumed per second |
| `kafka_consumer_lag_change_rate` | Lag change rate (msg/s) |
| `kafka_consumer_group_total_lag` | Total lag across all partitions for a group |
| `kafka_consumer_group_members` | Member count per consumer group |
| `kafka_lag_exporter_scrape_duration_seconds` | Collection duration per cluster |

---

## RCA Event Types

| Event | Trigger | Severity |
|-------|---------|----------|
| `consumer_stopped` | Active/lagging → stopped transition | Critical |
| `consumer_stalled` | Active → stalled transition | Warning |
| `rapid_lag_increase` | Lag growth > 2× configured threshold with lag > 1000 | Warning |
| `consumer_recovered` | Stopped/stalled → active transition | Info |
| `lag_cleared` | Lag drops to 0 from > 1000 | Info |

---

## Quick Start

```sh
# Build Docker images
make docker-build

# Start full stack (Kafka 3-node, Redis, Prometheus, Grafana, LagRadar, RCA consumer)
make compose-up

# Run end-to-end RCA pipeline test
python3 local_e2e_rca_test.py

# Access
# Prometheus metrics:  http://localhost:8080/metrics
# Grafana dashboard:   http://localhost:3000 (admin/admin)
# LagRadar API:        http://localhost:8080/api/v1/status
```

### API Endpoints

```
GET /health                                    # Liveness probe
GET /ready                                     # Readiness probe
GET /api/v1/clusters                           # List monitored clusters
GET /api/v1/clusters/{cluster}/status          # Cluster status
GET /api/v1/clusters/{cluster}/groups          # List groups in cluster
GET /api/v1/clusters/{cluster}/groups/{group}  # Group detail
GET /api/v1/status                             # All groups (aggregated)
GET /api/v1/groups                             # List all groups
GET /api/v1/config                             # Running configuration
```

---

## Configuration

See [`config.dev.yaml`](configs/config.dev.yaml) for a full example. Key settings:

```yaml
collector:
  window:
    size: 10                         # Records per partition window
    min_size: 5                      # Minimum records before evaluation
  check_interval: "10s"
  max_concurrency: 10
  thresholds:
    stalled_consumption_rate: 0.1    # msg/s below this = stalled
    rapid_lag_increase_rate: 20      # msg/s above this = rapid increase
    lag_trend_threshold: 0.1         # Relative change threshold for trend
    inactivity_timeout: "45s"        # No offset change = stopped
```

---

## Project Structure

```
cmd/
  monitor/main.go        # LagRadar monitor entrypoint
  rca/main.go            # RCA consumer entrypoint
internal/
  collector/             # Sliding window collector + evaluator
  cluster/               # Multi-cluster manager
  publisher/             # RCA event publisher + analyzer
  rca/                   # Event types, config, consumer
  metrics/               # Prometheus metric definitions
  api/                   # HTTP handlers
pkg/
  kafka/                 # Kafka admin/consumer client wrapper
distributed/             # Coordination configs, state store, sharding
```

---

## E2E Test Scenarios

The [`local_e2e_rca_test.py`](local_e2e_rca_test.py) script validates the full pipeline by simulating four scenarios against a local Kafka cluster:

1. **Consumer Stalled** — Producer at 200 msg/s, consumer at 10 msg/s → triggers `consumer_stalled`
2. **Rapid Lag Increase** — Normal production followed by 10× spike → triggers `rapid_lag_increase`
3. **Consumer Recovery** — Consumer stops for 60s, then resumes → triggers `consumer_stopped` then `consumer_recovered`
4. **Lag Cleared** — 5000 messages produced upfront, consumer clears backlog → triggers `lag_cleared`

---

## Tech Stack

Go 1.26, confluent-kafka-go, go-redis, Prometheus client, Docker, Redis Streams

---

*This is a personal portfolio project demonstrating distributed systems and SRE practices. Not intended for production deployment.*
