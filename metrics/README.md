# JSON-RPC Benchmark Grafana Integration

This directory contains the complete Grafana integration for the JSON-RPC benchmark system, including dashboards, alerting, and data source configurations.

## Components

### Data Sources

- **PostgreSQL**: Direct database access for detailed queries and table views
- **JSON-RPC Bench API**: SimpleJSON datasource pointing to the custom Grafana API endpoints
- **Prometheus**: For real-time metrics collection (optional)

### Dashboards

1. **jsonrpc-benchmark-enhanced.json**: Main performance monitoring dashboard
2. **baseline-comparison.json**: Baseline comparison and regression analysis
3. **jsonrpc-benchmark.json**: Legacy Prometheus-based dashboard

### Alerting

- **alerts.yml**: Alert rules for regression detection, error rate spikes, and system monitoring
- **contact-points.yml**: Modern Grafana alerting contact points (webhooks, email, Slack, Discord)
- **notification-policies.yml**: Alert routing and escalation policies
- **notifiers.yml**: Legacy notification channels (for backward compatibility)

## Quick Start

### 1. Set up environment variables

```bash
cp grafana.env.example .env
# Edit .env with your specific configuration
```

### 2. Deploy with Docker Compose

```bash
# Start the complete stack
docker-compose -f docker-compose.grafana.yml up -d

# Or start individual services
docker-compose -f docker-compose.grafana.yml up -d postgres
docker-compose -f docker-compose.grafana.yml up -d grafana
```

### 3. Access Grafana

- URL: <http://localhost:3000>
- Username: admin
- Password: admin (or your configured password)

### 4. Import Dashboards

Dashboards are automatically provisioned from the `dashboards/` directory.

## Configuration

### Environment Variables

#### Required Variables

```bash
# Database
POSTGRES_DB=jsonrpc_bench
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres

# Grafana Admin
GF_SECURITY_ADMIN_USER=admin
GF_SECURITY_ADMIN_PASSWORD=your-secure-password
```

#### Optional Notification Variables

```bash
# Slack Integration
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/your-webhook-url

# Discord Integration  
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/your-webhook-url

# Email Alerts
ALERT_EMAIL_ADDRESSES=alerts@your-domain.com,team@your-domain.com
GF_SMTP_ENABLED=true
GF_SMTP_HOST=smtp.your-domain.com:587
GF_SMTP_USER=alerts@your-domain.com
GF_SMTP_PASSWORD=your-email-password

# CI/CD Integration
WEBHOOK_CI_URL=https://your-ci-system.com/webhook
CI_WEBHOOK_TOKEN=your-ci-token

# GitHub Integration
GITHUB_WEBHOOK_URL=https://api.github.com/repos/your-org/your-repo/issues
GITHUB_TOKEN=your-github-token
```

### Data Source Configuration

#### PostgreSQL

```yaml
datasources:
  - name: PostgreSQL
    type: postgres
    url: postgres:5432
    database: jsonrpc_bench
    user: postgres
    jsonData:
      sslmode: "disable"
      maxOpenConns: 100
```

#### JSON-RPC Bench API

```yaml
datasources:
  - name: JSON-RPC Bench API
    type: simplejson
    url: http://runner:8080/api/grafana
    isDefault: true
```

## Dashboards

Two of these read Prometheus, two read PostgreSQL. Which one a panel uses
decides what it can show and when it has data.

| Dashboard | Source | Purpose |
|---|---|---|
| `benchmark-dashboard.json` | Prometheus (`bench_*`) | Live view of a run: latency, outcomes, JSON-RPC errors, load delivery. |
| `archive/k6-dashboard.json` | Prometheus (`k6_*`) | The k6-era dashboard, kept for reference. Nothing writes `k6_*` any more, so its panels render empty. Provisioned into an "archive" folder. |
| `jsonrpc-benchmark-enhanced.json` | Runner API over PostgreSQL | Historic runs across time. Needs `--historic`. |
| `baseline-comparison.json` | PostgreSQL | Current runs against a stored baseline. |

**[METRICS.md](METRICS.md) is the reference for every `bench_*` series**: its
labels, its units, what it means, and how each old `k6_*` name maps onto it.
Read it before writing a panel — the trend families are cumulative from the
start of a run, which is not what a Prometheus user assumes.

### Main Dashboard (jsonrpc-benchmark-enhanced.json)

- **Overall Latency Metrics**: Average, P95, P99 latency trends
- **Client Comparison**: Performance metrics by client (Geth, Nethermind, Besu, Erigon)
- **Error Rate Monitoring**: Error rate trends and alerts
- **Throughput Analysis**: Requests per second by client
- **Detailed Results Table**: Tabular view of recent benchmark results
- **Regression Detection**: Automated detection of performance regressions

### Baseline Comparison Dashboard

- **Performance vs Baseline**: Compare current performance against established baselines
- **Deviation Analysis**: Percentage deviation from baseline metrics
- **Historical Trends**: Long-term performance trend analysis
- **Regression Summary**: Table of detected regressions with severity levels

## Alerting

### Alert Rules

#### High Latency Regression

- **Condition**: Average latency increased by >50% compared to previous 10 minutes
- **Duration**: 2 minutes
- **Severity**: Major

#### High Error Rate

- **Condition**: Error rate >5% for 5 minutes
- **Duration**: 1 minute  
- **Severity**: Critical

#### Throughput Drop

- **Condition**: Throughput dropped by >30% compared to previous 25 minutes
- **Duration**: 3 minutes
- **Severity**: Major

#### No Benchmark Data

- **Condition**: No data received for 15 minutes
- **Duration**: 5 minutes
- **Severity**: Major

### Notification Routing

1. **Critical Alerts**:
   - Immediate CI/CD webhook
   - Slack notification
   - Email to team
   - GitHub issue creation

2. **Major Alerts**:
   - Slack notification within 2 minutes
   - Email within 5 minutes
   - CI/CD webhook within 1 minute

3. **Monitoring Alerts**:
   - Discord notification
   - Less frequent escalation

## API Integration

### Grafana API Endpoints

The system provides SimpleJSON datasource compatible endpoints:

- `GET /api/grafana/` - Test connection
- `POST /api/grafana/search` - Metric discovery
- `POST /api/grafana/query` - Time series data
- `POST /api/grafana/annotations` - Annotations (regressions, baselines, deployments)
- `GET /api/grafana/metrics` - Metrics metadata

### Supported Metrics

- `{test_name}.{client}.avg_latency`
- `{test_name}.{client}.p95_latency`
- `{test_name}.{client}.p99_latency`
- `{test_name}.{client}.error_rate`
- `{test_name}.{client}.throughput`

### Aggregation Functions

- `rate({metric})` - Rate of change
- `delta({metric})` - Difference from previous value
- `count({metric})` - Count of data points

## Database Schema

### Tables

- **historic_runs**: Benchmark execution results
- **baselines**: Performance baselines for comparison
- **regressions**: Detected performance regressions
- **performance_alerts**: System alerts and notifications
- **test_configurations**: Test configuration metadata

### Key Indexes

- Composite indexes on (test_name, timestamp)
- GIN indexes on JSONB columns for performance
- Individual indexes on frequently queried columns

## Maintenance

### Database Cleanup

```sql
-- Clean up old data (keeps baselines)
SELECT cleanup_old_data(90); -- Keep 90 days of data
```

### Backup Recommendations

- Regular PostgreSQL backups of the `jsonrpc_bench` database
- Grafana dashboard backups (JSON exports)
- Configuration backups (provisioning files)

### Monitoring Health

- Grafana health endpoint: `http://localhost:3000/api/health`
- PostgreSQL connection monitoring
- Alert delivery verification

## Known issues

These are defects found while replacing the load engine, in parts of the stack
that work was deliberately not touching. They are recorded here so they are
found by whoever picks them up rather than rediscovered.

Runs now record what their error rate counted, and both comparison paths check
it: `CompareRuns` returns a refusal instead of regressions, and
`CompareToBaseline` returns an error. A run that recorded nothing is reported as
unverified rather than refused, since an absent value is not proof of a
mismatch. The remaining gap is that `CompareToSequential` and
`CompareToRollingAverage` aggregate several runs and do not yet check that the
window is internally consistent.

- **The simplejson datasource points at the wrong port.** `datasource.yml` sets
  `http://runner:8080/api/grafana` while the runner's API binds `:8081`, so the
  targets in `jsonrpc-benchmark-enhanced.json` cannot resolve as provisioned.

- **The provisioned alert rules cannot fire.** They query bare targets such as
  `avg_latency`, but the API's `parseMetricTarget` requires at least three
  dot-separated segments (`test_name.client.metric`) and returns nil for
  anything shorter. All four rules are affected.

- **`full_results` is computed on every run and never stored.** `SaveRun`
  marshals the whole result into the field, but `InsertRun`'s column list omits
  it and `GetRun`'s `SELECT` does not read it back. So `CompareRuns` always
  finds it empty and falls through to the aggregate summary, which means
  `diffClientMetrics` — the per-client, per-method regression diff — never runs
  against stored data. The same drift affects `metadata`. Adding both to the
  INSERT and SELECT would switch that path on; it is left alone here because
  turning it on changes what the API reports.

- **`GetRun` fabricates p99 and max latency.** They are not stored, so it sets
  both to the p95 it does have, with a comment saying so. A regression detector
  comparing "p99" is therefore comparing p95, and `TotalErrors` is re-derived
  from the success rate rather than read. Any threshold written against p99 in
  the historic path is not measuring p99.

- **The dashboard UI still reads `environment.k6Version`.** That field is gone
  from the Go type; the engine and its version are now recorded in the run
  manifest (`manifest.json`, and `manifest` in `results.json`) alongside the
  error-rate semantics. The UI needs a one-line change to read
  `manifest.engine_version` instead. It renders blank until then, which it
  already did — the old JSON tag was `k6_version` and never matched.

## Troubleshooting

### Common Issues

#### Data Source Connection Failed

1. Verify PostgreSQL is running and accessible
2. Check database credentials in environment variables
3. Ensure network connectivity between containers

#### No Data in Dashboards

1. Verify the runner service is populating data
2. Check SimpleJSON datasource configuration
3. Validate API endpoints are responding

#### Alerts Not Firing

1. Check alert rule conditions and thresholds
2. Verify notification channel configuration
3. Review Grafana alerting logs

#### Performance Issues

1. Review database query performance
2. Check resource allocation for containers
3. Optimize dashboard refresh intervals

### Logs

- Grafana logs: `docker logs jsonrpc-bench-grafana`
- PostgreSQL logs: `docker logs jsonrpc-bench-postgres`
- Runner API logs: `docker logs jsonrpc-bench-runner`

## Security Considerations

- Change default admin credentials
- Use strong database passwords
- Secure webhook URLs and tokens
- Enable HTTPS in production
- Restrict network access appropriately
- Regular security updates for container images

## Development

### Adding New Metrics

1. Extend the Grafana API in `runner/api/grafana_api.go`
2. Update dashboard queries to include new metrics
3. Add appropriate alert rules if needed

### Custom Dashboard Development

1. Create dashboards in Grafana UI
2. Export as JSON
3. Place in `dashboards/` directory for provisioning
4. Update variable queries as needed

### Testing Alerts

```bash
# Trigger test alerts
curl -X POST http://localhost:8080/api/test/alert \
  -H "Content-Type: application/json" \
  -d '{"type": "latency_regression", "severity": "major"}'
```

## Production Deployment

### Prerequisites

- Docker and Docker Compose
- Persistent volume storage
- Reverse proxy (nginx/traefik) for HTTPS
- External PostgreSQL database (recommended)
- SMTP server for email notifications

### Environment Specific Configuration

- Update all webhook URLs for production systems
- Configure proper email settings
- Set appropriate data retention policies
- Enable authentication/authorization
- Configure backup strategies
- Set up monitoring for the monitoring system

### Scaling Considerations

- PostgreSQL read replicas for query performance
- Grafana clustering for high availability
- Load balancing for API endpoints
- Separate alerting infrastructure if needed
