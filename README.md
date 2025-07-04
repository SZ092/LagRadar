# LagRadar

**LagRadar** is an open-source, self-hosted observability tool for monitoring Kafka consumer lag and group health, with a focus on
reliability, extensibility, and professional SRE workflows.

> *Monitor Kafka consumer lag like a radar. Spot anomalies, trigger alerts, and gain insight into the health of your streaming data pipelines.*

🚧 This project is an MVP and actively evolving. Some features are still WIP and there may be flaky behavior—please use with caution and report any issues you find.

---

## Features

- **Real-time Kafka consumer group lag tracking**
- **Lag trend analysis based on sliding window evaluation**
- **Prometheus metrics export** for integration with Grafana, alerting, and long-term monitoring
- **RESTful API** for consumer group and partition health status
- **Health/Ready endpoints** for Kubernetes/cloud-native operations
- **Easy deployment:** Single binary, no external dependencies required for core monitoring
- **Extensible:** Designed for future integration with Redis, custom alerting, and automated RCA

---
## Architecture
                                        +------------------------+
                                        |        Grafana         |
                                        |  (dashboard & alerts)  |
                                        +-----------+------------+
                                                    |
                        +---------------------------v--------------------------+
                        |                  Prometheus (scrapes)                |
                        +---------------------------+--------------------------+
                                                    |
                                   +----------------v----------------+
                                   |         LagRadar API             |
                                   |  - /metrics (Prometheus)         |
                                   |  - /api/v1/status                |
                                   |  - /api/v1/status/{groupId}      |
                                   |  - /api/v1/groups                |
                                   |  - /api/v1/config                |
                                   +----------------+----------------+
                                                    |
                                +-------------------v-------------------+
                                |         Sliding Window Engine         |
                                |   +-------------------------------+   |
                                |   |   Lag/Offset Time Series      |   |
                                |   |   Trend/Anomaly Detection     |   |
                                |   +-------------------------------+   |
                                |           |            |              |
                                |   +-------v--------+   |              |
                                |   | Status Eval    |   |              |
                                |   | (Healthy/      |   |              |
                                |   |  Stalled/Spike)|   |              |
                                |   +-------+--------+   |              |
                                |           |            |              |
                                |   (Future) RCA Engine  |              |
                                +------------------------+--------------+
                                                    |
                                        +-----------v-----------+
                                        |   Kafka Cluster(s)    |
                                        +----------------------+

---

## Quick Start

### One-click Local Dev Environment

LagRadar provides a fully automated local test environment with Kafka, Zookeeper, Prometheus, and Grafana, using `docker-compose`.

#### 1. Clone the repo

```sh
git clone https://github.com/{user}/lagradar.git
cd lagradar
```

#### 2. Build and start the stack

You can use either Makefile commands or docker-compose directly.

**With Makefile (recommended):**

```sh
make help                 # Show help messages - for Makefile commands
make build                # Build the application binary
make compose-up           # Start Kafka, Zookeeper, Prometheus, Grafana, LagRadar
```

#### 3. Open dashboards and endpoints

- LagRadar API & Prometheus metrics: [http://localhost:8080/metrics](http://localhost:8080/metrics)
- Prometheus UI: [http://localhost:9090](http://localhost:9090)
- Grafana UI: [http://localhost:3000](http://localhost:3000) (username: admin / password: admin by default)

Grafana will auto-load example dashboards from `grafana/provisioning/dashboards/` if present.

#### 4. Custom configuration

- Edit `config.dev.yaml` (recommended for local development/testing only) for LagRadar Kafka connection and tuning.
- See `prometheus/prometheus.yml` for scrape configs.

### 5. View Prometheus metrics

- Visit `http://localhost:8080/metrics` 
- Add this endpoint as a scrape target in your Prometheus config.

### 6. Explore API endpoints

This service exposes a set of HTTP/REST APIs for cluster monitoring, consumer group status, Prometheus scraping, and health checks. Both legacy single-cluster and modern multi-cluster use cases are supported.

#### Core Endpoints

| Endpoint         | Description                           |
| ---------------- | ------------------------------------- |
| `/health`        | Health check (K8s liveness probe)     |
| `/ready`         | Readiness check (K8s readiness probe) |
| `/metrics`       | Prometheus metrics scrape endpoint    |
| `/api/v1/config` | Show current Collector config         |

#### Cluster & Consumer Group APIs
| Endpoint                                    | Description                                |
| ------------------------------------------- | ------------------------------------------ |
| `/api/v1/clusters`                          | List all monitored clusters                |
| `/api/v1/clusters/{cluster}/status`         | Get status for all groups in the cluster   |
| `/api/v1/clusters/{cluster}/groups`         | List all consumer groups in the cluster    |
| `/api/v1/clusters/{cluster}/groups/{group}` | Get status for a specific group in cluster |

#### Legacy Single-Cluster (Backward Compatibility)
| Endpoint         | Description                             |
| ---------------- | --------------------------------------- |
| `/api/v1/groups` | List all monitored groups (legacy mode) |
| `/api/v1/status` | Get aggregated status for all groups    |


---

## Grafana Integration

- Use the provided dashboards and datasources under `grafana/` for quick visualization.
- Example panels: Total lag, per-group lag, health trends, alert rules.

---

## Roadmap

- [ ] **Multi-cluster Monitoring:**  
  Support monitoring multiple Kafka clusters from a single LagRadar instance.  
  Useful for large organizations and multi-tenant scenarios.

- [ ] **Automated Root Cause Analysis (RCA):**  
  Built-in RCA engine for lag and consumer health anomalies, with event-driven workflows.  
  (See `pkg/redis/README.md` for the proposal.)

- [ ] **Pluggable Grafana Dashboards/Panels:**  
  Official and community-maintained dashboards, custom panel plugin framework.

- [ ] **Production-grade Benchmark Tests:**  
  End-to-end benchmarking suite for throughput, latency, and scalability validation.

- [ ] **Pluggable Alerting & Notification:**  
  Integration with Grafana alerts, email, Slack, webhook, and custom notification backends.

- [ ] **API Security and Access Control:**  
  Token-based API authentication, Prometheus metrics endpoint protection.

---

## License
[Apache-2.0](LICENSE)