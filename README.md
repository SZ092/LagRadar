# LagRadar

**LagRadar** is a modern, extensible toolkit for Kafka consumer lag monitoring and automated root cause analysis (RCA).

Designed and built by Zhe Song, this repository serves as a portfolio piece and demonstration of my approach to distributed systems, infrastructure-native workflows, and modern SRE practices.

> **NOTE:**  
> LagRadar is a personal engineering project for demo/reference only. Not intended as production OSS or an active community project.

---

## Latest Updates

###  Modular RCA Consumer
Standalone RCA pipeline: lag events flow through Redis Streams to a dedicated RCA consumer, enabling future extension for alerting and integrations.

###  End-to-End Test Coverage
Provided an automated e2e python script simulating full group lifecycle and lag anomalies, validating the RCA pipeline from Kafka through Redis Streams to the RCA consumer.
All major lag scenarios (stall, spike, recovery, lag clear) are covered and verified in the pipeline (see [local_e2e_rca_test.py](local_e2e_rca_test.py)).

### Enhanced Local Deployment
Refactored Makefile and Docker Compose setup for fast, reproducible local dev/test—run the entire monitoring + RCA pipeline with a single command.

### Distributed Core
Redis-powered sharding, state management, and instance coordination for horizontal scalability and failover.

---

## Architecture Highlight
###  Distributed, Extensible by Design

- **Distributed Coordination & Sharding**
  - Consistent hashing and Redis-backed state store for scalable group assignment, instance health, and windowed analytics.
  - Lag events published to Redis Streams for fully decoupled, replayable event workflow; enables future modular integrations (alerting, storage, RCA plugins).
- **Production-inspired Local Testability**
  - End-to-end test script simulates group lifecycle, lag anomaly injection, and auto-verifies RCA pipeline detection/processing in a single run.
- **Cloud-Native Observability**
  - Exposes detailed Prometheus metrics for RCA events, anomaly detection, and consumer health. Natively supports integration with Grafana and other monitoring stacks.

> Designed for demo/reference only — not for direct production deployment.
---


## Quick Start

### One-click Local Dev Environment

LagRadar provides a fully automated local test environment with Kafka, Zookeeper, Prometheus, and Grafana, using `docker-compose`.

#### 1. Clone the repo

```sh
git clone https://github.com/{yourusername}/LagRadar.git
cd LagRadar
```

#### 2. Build and start the stack

**With Makefile (recommended):**

```sh
# Build Docker images
make docker-build

# [Optional] Rebuild images without cache and start all services  
make compose-rebuild

# Start all services (Kafka, Zookeeper, Redis, LagRadar, RCA, Prometheus, Grafana)
make compose-up         

# Run E2E RCA pipeline test
python3 local_e2e_rca_test.py

# Sample Output - Some event boundaries (e.g. “recovered” vs “lag cleared”) may overlap in test results, reflecting real-world 
# signal ambiguity and scenario simulation limitations.
2025-08-05 14:54:21,721 - __main__ - INFO - 🚨 RCA Event Detected: consumer_stalled
2025-08-05 14:54:21,721 - __main__ - INFO -    Group: e2e-consumer-stalled-group-1754419994
2025-08-05 14:54:21,721 - __main__ - INFO -    Topic: e2e-consumer-stalled-1754419994[0]
2025-08-05 14:54:21,721 - __main__ - INFO -    Severity: warning
2025-08-05 14:54:21,721 - __main__ - INFO -    Message: Consumer stalled for 40s
2025-08-05 14:54:21,721 - __main__ - INFO -    Current Lag: 1800
2025-08-05 14:54:21,721 - __main__ - INFO -    ID: 1754420061716-0
2025-08-05 14:54:24,425 - __main__ - INFO - Window completeness for e2e-consumer-stalled-group-1754419994: 70%
2025-08-05 14:54:34,438 - __main__ - INFO - Window completeness for e2e-consumer-stalled-group-1754419994: 80%
2025-08-05 14:54:34,438 - __main__ - INFO - Waiting additional 30s for LagRadar to process...
2025-08-05 14:55:04,432 - __main__ - INFO - Scenario consumer-stalled completed successfully
2025-08-05 14:55:04,432 - __main__ - INFO - Stopping scenario: consumer-stalled
2025-08-05 14:55:34,442 - __main__ - INFO - Final status: {
  "cluster": "default",
  "group": "e2e-consumer-stalled-group-1754419994",
  "status": {
    "GroupID": "e2e-consumer-stalled-group-1754419994",
    "OverallHealth": "CRITICAL",
    "TotalLag": 1800,
    "PartitionCount": 1,
    "ActivePartitions": 0,
    "StoppedPartitions": 1,
    "StalledPartitions": 0,
    "MemberCount": 0,
    "LastUpdateTime": "2025-08-05T18:55:31.691556088Z",
    "CriticalPartitions": [
      {
        "GroupID": "e2e-consumer-stalled-group-1754419994",
        "Topic": "e2e-consumer-stalled-1754419994",
        "Partition": 0,
        "CurrentOffset": 200,
        "HighWatermark": 2000,
        "CurrentLag": 1800,
        "LastUpdateTime": "2025-08-05T18:55:31.693280629Z",
        "Status": "STOPPED",
        "LagTrend": "STABLE",
        "Health": "CRITICAL",
        "ConsumptionRate": 0,
        "LagChangeRate": 0,
        "WindowCompleteness": 100,
        "Message": "Consumer stopped for 1m30s with lag 1800",
        "IsActive": false,
        "TimeSinceLastMove": 90002741667
      }
    ],
    "WarningPartitions": null,
    "HealthyPartitions": null
  }
}

```

#### 3. Access endpoints & dashboards

- View Prometheus metrics at:   [http://localhost:8080/metrics](http://localhost:8080/metrics)
- Default Grafana Dashboard at:    [http://localhost:3000](http://localhost:3000)

#### 4. Custom configuration

- For config details, see `config.dev.yaml` and inline comments in [Makefile](Makefile)

---
## Key Features

- Multi-cluster, partition-level analytics
- Multi-group, multi-topic monitoring
- Standalone, event-driven RCA consumer (pluggable and extensible)
- Distributed sharding and coordination (Redis-powered)
- Full local dev/test workflow (Docker Compose)
---

## Implemented (Demo Only)

- ✅ Distributed state/sharding backbone
- ✅ Pluggable RCA pipeline (Redis Streams)
- ✅ Standalone RCA consumer with test harness
- ✅ Local end-to-end test script

## Out of Scope
These items were considered during the design phase but are intentionally left out of scope for this demo:
- Grafana alerting integration demo
- Notification/report modules (extensible)

---
## About This Project
> As of Aug 2025, LagRadar is feature-frozen, with no new functionality planned

Maintained by Zhe Song as a personal engineering demo for distributed systems and SRE practices. Not a production OSS—no guarantees, no long-term support.

Questions or feedback: please contact: zhegithubcontact [at] gmail.com or raise an Issue
