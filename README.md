# LagRadar

**LagRadar** is a modern, extensible toolkit for Kafka consumer lag monitoring and automated root cause analysis (RCA).

Designed and built by Zhe Song, this repository serves as a portfolio piece and demonstration of my approach to distributed systems, infrastructure-native workflows, and modern SRE practices.

> **NOTE:**  
> LagRadar is a personal engineering project for demo/reference only. Not intended as production OSS or an active community project.

---

## Key Features

- **Multi-cluster Support**
  - Monitor multiple Kafka clusters with unified metrics and API endpoints.
- **Consumer Group & Partition Insights** 
  - Deep-dive lag and health tracking across any number of consumer groups.
- **Kubernetes-ready** 
  - Stateless by design; health and readiness endpoints for K8s probes
- **Prometheus Integration** 
  - Exposes detailed metrics for Prometheus, Grafana, and observability stacks.
- **Legacy/Single Cluster Backward Compatibility** 
  - Seamlessly supports simple setups out of the box.
- **Extensible Core** 
  - Pluggable for Redis, RCA automation, custom notifications, and dashboards.
- **Self-documenting APIs** 
  - Clean REST endpoints, easy for both human and programmatic use.
- **Code and architecture clarity** 
  - Strong focus on code quality, diagnostics, and real-world reliability.

---

## Demo Use Cases

- Local development and PoC for distributed Kafka observability and RCA automation.
- Reference template for multi-cluster infra tools and SRE monitoring workflows.
- Interview/portfolio artifact—demonstrates system design, API quality, and modern infra best practices.


---

## Quick Start

> **Note:** For local/demo use only. Test with Docker Compose or K8s. Not for direct production deployment.

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

#### 3. Access endpoints & dashboards

- LagRadar API & Prometheus metrics: [http://localhost:8080/metrics](http://localhost:8080/metrics)
- Prometheus: [http://localhost:9090](http://localhost:9090)
- Grafana:    [http://localhost:3000](http://localhost:3000) (admin / admin by default)

#### 4. Custom configuration

- Edit `config.dev.yaml` for Kafka setup.
- See `prometheus/prometheus.yml` for scrape configs.

## API Overview


#### Core Endpoints

| Endpoint         | Description                        |
| ---------------- |------------------------------------|
| `/health`        | K8s liveness probe                 |
| `/ready`         | K8s readiness probe                |
| `/metrics`       | Prometheus metrics scrape endpoint |
| `/api/v1/config` | Show current  config               |

#### Cluster & Consumer Group APIs
| Endpoint                                    | Description                                |
| ------------------------------------------- | ------------------------------------------ |
| `/api/v1/clusters`                          | List all monitored clusters                |
| `/api/v1/clusters/{cluster}/status`         | Get status for all groups in the cluster   |
| `/api/v1/clusters/{cluster}/groups`         | List all consumer groups in the cluster    |
| `/api/v1/clusters/{cluster}/groups/{group}` | Get status for a specific group in cluster |

#### Legacy Single-Cluster
| Endpoint         | Description                            |
| ---------------- |----------------------------------------|
| `/api/v1/groups` | List all consumer groups (legacy mode) |
| `/api/v1/status` | Aggregated group status (legacy)       |

---

## Roadmap (Demo Only)

- ✅ Multi-cluster & legacy fallback
- ✅ Partition-level analytics, group health
- ✅ K8s & observability endpoints
- ✅ Prometheus & Grafana integration
- 🛠️ **[WIP]** Redis-powered RCA 
- 🛠️ **[WIP]** RCA trace/report examples
- 📝 Notification hooks (reference/demo)

---
## About This Project

Maintained by Zhe Song as a personal engineering demo for distributed systems and SRE practices. Not a production OSS—no guarantees, no long-term support.

Questions or feedback: contact: zhegithubcontact [at] gmail.com
