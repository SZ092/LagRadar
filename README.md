# LagRadar

**LagRadar** is a personal engineering project and proof-of-concept for advanced Kafka consumer lag observability, multi-cluster monitoring, and automated diagnostics.  
Designed and built by Zhe Song, this repository serves as a portfolio piece and demonstration of my approach to distributed systems, infrastructure-native workflows, and modern SRE practices.

> **NOTE:**  
> LagRadar is not intended as a production OSS or community project. All features and APIs are designed for technical showcase and engineering demonstration.  
> This repository is maintained solely for personal branding and professional reference.

---

## Key Features

- **Multi-cluster Support:** Monitor multiple Kafka clusters with unified metrics and API endpoints.
- **Consumer Group & Partition Insights:** Track lag, health, and offsets across any number of consumer groups—even those sharing the same partition set.
- **Kubernetes-ready:** Designed for stateless deployment, with built-in `/health` and `/ready` endpoints for k8s probes.
- **Prometheus Integration:** Exposes detailed metrics compatible with Prometheus, Grafana, and external observability tools.
- **Legacy/Single Cluster Backward Compatibility:** Seamlessly supports simple legacy setups out of the box.
- **Extensible Core:** Structure ready for integrating Redis, semi-automated RCA, custom notification flows, or dashboard plugins.
- **Self-documenting APIs:** Clean RESTful endpoints for both human and programmatic consumption.
- **Technical Depth:** Focus on code quality, system health modeling, extensible diagnostics, and real-world reliability patterns.

---

## Demo Use Cases

- Local development and PoC for distributed Kafka observability and RCA automation.
- Template/reference for multi-cluster infra tools, metrics exporters, and SRE monitoring workflows.
- Engineering interview “trace artifact”—demonstrates system design, API structuring, code quality, and practical best practices.


---

## Quick Start

> **Note:** LagRadar is for demo/engineering showcase only. 
> Local testing via Docker Compose or K8s is recommended; not intended for direct production use.

### One-click Local Dev Environment

LagRadar provides a fully automated local test environment with Kafka, Zookeeper, Prometheus, and Grafana, using `docker-compose`.

#### 1. Clone the repo

```sh
git clone https://github.com/{yourusername}/lagradar.git
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

- Edit `config.dev.yaml` (local development/testing only) for LagRadar Kafka connection and tuning.
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

## Technical Roadmap (For Demo)

- ✅ Multi-cluster configuration with seamless legacy fallback
- ✅ Partition-level consumer lag analytics & group health evaluation
- ✅ Kubernetes-ready deployment & observability endpoints
- ✅ Prometheus & Grafana integration

- 🛠️ **[In Progress]** Redis-based RCA pipeline & semi-automatic diagnosis
- 🛠️ **[In Progress]** Example root-cause analysis (RCA) trace/report
- 📝 Reference notification hooks (e.g., Slack, webhook)

---
## About This Project
- Maintained by Zhe Song as a personal demonstration of distributed systems design, infra workflows, and advanced SRE practices.  
- Not a production OSS—no guarantees, no long-term maintenance. For demo, interview, and learning purposes only.

For technical questions, collaboration, or feedback: contact: zhegithubcontact [at] gmail.com
