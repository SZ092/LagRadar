# LagRadar

**LagRadar** is an open-source, self-hosted observability tool for monitoring Kafka consumer lag and group health, with a focus on
reliability, extensibility, and professional SRE workflows.

> *Monitor Kafka consumer lag like a radar. Spot anomalies, trigger alerts, and gain insight into the health of your streaming data pipelines.*

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

- Edit `config.yaml` for LagRadar Kafka connection and tuning.
- See `prometheus/prometheus.yml` for scrape configs.

### 5. View Prometheus metrics

- Visit `http://localhost:8080/metrics` 
- Add this endpoint as a scrape target in your Prometheus config.

### 6. Explore API endpoints
- `GET /api/v1/groups` 
  - List all monitored consumer group IDs
- `GET /api/v1/status` 
  - All group health status
- `GET /api/v1/status/{group}` 
  - Status for a specific group
- `GET /api/v1/config` 
  - Get current Config of Collector
---

## Example API Usage

```sh
# List group IDs for all active consumer groups that being monitored 
curl -s http://localhost:8080/api/v1/groups

# Get status for all consumer groups
curl -s http://localhost:8080/api/v1/status

# Get status for specific consumer group
curl -s http://localhost:8080/api/v1/status/my-consumer-group
```

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