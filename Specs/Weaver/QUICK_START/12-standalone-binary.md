# Weaver: Standalone Binary Quick Start (5 Minutes)

> **Read Time:** 5 minutes  
> **Deployment Time:** 5 minutes  
> **Audience:** DevOps, Bare Metal, Small Deployments  
> **Previous:** [11-docker-compose.md](./11-docker-compose.md) | **Next:** [13-verify-deployment.md](./13-verify-deployment.md)

---

## System Diagram

```
┌───────────────────────────────┐
│  Single Host                  │
├───────────────────────────────┤
│                               │
│  ./weaver --config cfg.yaml   │
│  ├─ :5051 (gRPC)              │
│  ├─ :8080 (HTTP)              │
│  └─ :9090 (Metrics)           │
│                               │
│  ← connects to →              │
│                               │
│  etcd-t2:2379 (remote)        │
│  fm-replicas (remote)         │
│                               │
└───────────────────────────────┘
```

---

## 5-Minute Setup

### **Step 1: Build or Download Weaver** (2 min)

**Option A: Build from Source**

```bash
# Clone repo
git clone https://github.com/dashfabric/weaver.git
cd weaver

# Build
make build

# Binary at: ./bin/weaver
```

**Option B: Use Pre-Built Binary**

```bash
# Download pre-built binary
curl -L https://releases.weaver.io/v1.0.0/weaver-linux-amd64 -o weaver
chmod +x weaver
```

---

### **Step 2: Create Configuration** (1 min)

```bash
cat > weaver-config.yaml <<'EOF'
gateway:
  name: "fm-gateway-standalone"
  mode: "primary_aware"

discovery:
  type: "t2_etcd"
  config:
    endpoint: "http://etcd-t2.example.com:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"

health:
  type: "http"
  interval: 10s
  config:
    endpoint: "/api/v1/health"

listeners:
  grpc:
    enabled: true
    port: 5051
  http:
    enabled: true
    port: 8080
  metrics:
    enabled: true
    port: 9090

load_balancers:
  - name: "default"
    type: "least_connections"

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
  retry:
    enabled: true
    max_attempts: 3
    backoff_strategy: "exponential"
    initial_backoff: 10ms
  queuing:
    enabled: true
    per_replica_depth: 1000

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
EOF
```

---

### **Step 3: Run Weaver** (1 min)

```bash
# Start Weaver
./weaver --config weaver-config.yaml
```

**Expected Output:**
```
[INFO] Weaver gateway started
[INFO] Configuration loaded from: weaver-config.yaml
[INFO] Discovery: T2 etcd endpoint connected
[INFO] Listeners: gRPC :5051, HTTP :8080, Metrics :9090
[INFO] Discovered 3 replicas: [fm-1, fm-2, fm-3]
[INFO] Gateway ready to accept requests
```

---

### **Step 4: Verify in Another Terminal** (1 min)

```bash
# Check replicas
curl http://localhost:8080/debug/replicas | jq .

# Check metrics
curl http://localhost:9090/metrics | grep fm_gw_replica_status

# Test gRPC
grpcurl -plaintext localhost:5051 list
```

---

## Configuration Options

### **For FM System (Primary-Aware)**

```yaml
gateway_mode: "primary_aware"
load_balancer: "primary_aware"
```

### **For CB System (Peer-Equivalent)**

```yaml
gateway_mode: "peer_equivalent"
load_balancer: "least_connections"
```

### **For Custom System**

```yaml
discovery:
  type: "consul"
  config:
    endpoint: "http://consul:8500"
    service_name: "my-service"

load_balancers:
  - name: "default"
    type: "consistent_hash"
```

---

## Systemd Service (Optional)

To run Weaver as a systemd service:

```bash
sudo tee /etc/systemd/system/weaver.service > /dev/null <<'EOF'
[Unit]
Description=Weaver Gateway
After=network.target

[Service]
Type=simple
User=weaver
WorkingDirectory=/opt/weaver
ExecStart=/opt/weaver/bin/weaver --config /etc/weaver/weaver-config.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable weaver
sudo systemctl start weaver

# Check status
sudo systemctl status weaver
```

---

## Common Tasks

### **View Logs**

```bash
# Tail logs
tail -f weaver.log

# Filter by level
grep "ERROR" weaver.log
grep "WARN" weaver.log
```

### **Update Configuration**

Edit `weaver-config.yaml` and Weaver will auto-reload within 1 second:

```bash
# Edit config
nano weaver-config.yaml

# Weaver detects change and reloads (no restart!)
# Check logs to confirm: [INFO] Configuration reloaded
```

### **Stop Weaver**

```bash
# Graceful shutdown (current requests complete)
Ctrl+C

# Or send signal
kill -SIGTERM $(pidof weaver)
```

---

## Troubleshooting

| Issue | Fix |
|-------|-----|
| **Connection refused on 5051** | Weaver not started; check logs |
| **No replicas discovered** | Check etcd endpoint and key pattern |
| **Port 5051 already in use** | Change port in config or kill other process |
| **High memory usage** | Check request queue depth; reduce with `per_replica_depth` |

---

## Performance Tuning

### **For High Throughput (100k+ req/s)**

```yaml
listeners:
  grpc:
    max_connections: 50000
    buffer_size: 1000

queuing:
  per_replica_depth: 10000

reliability:
  circuit_breaker:
    failure_threshold: 10  # Higher threshold for bursty workloads
```

### **For Low Latency**

```yaml
reliability:
  timeout:
    global: 10s          # Lower timeout
    per_replica: 5s
    connect: 1s

  retry:
    max_attempts: 1      # Fewer retries
    initial_backoff: 1ms
```

---

## Next Steps

- **Verify Deployment** → [13-verify-deployment.md](./13-verify-deployment.md)
- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)

---

**Navigation:**
- [← Previous](./11-docker-compose.md)
- [Index](../INDEX.md)
- [Next →](./13-verify-deployment.md)
