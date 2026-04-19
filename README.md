# Swarmex Operator DB

Database health monitoring and automatic failover for Docker Swarm.

Part of [Swarmex](https://github.com/ccvass/swarmex) — enterprise-grade orchestration for Docker Swarm.

## What It Does

Monitors database containers via TCP health checks and triggers automatic failover when the primary becomes unreachable. Ensures minimum ready replicas are maintained before promoting a standby.

## Labels

```yaml
deploy:
  labels:
    swarmex.operator.enabled: "true"     # Enable DB operator
    swarmex.operator.type: "postgresql"  # Database type (postgresql, mysql, redis)
    swarmex.operator.port: "5432"        # Port for TCP health check
    swarmex.operator.min-ready: "1"      # Minimum ready replicas before failover
```

## How It Works

1. Discovers database services with operator labels.
2. Performs periodic TCP checks against the configured port.
3. Tracks consecutive failures per instance.
4. Triggers failover by promoting a standby when the primary fails.
5. Ensures min-ready replicas are healthy before any failover action.

## Quick Start

```bash
docker service update \
  --label-add swarmex.operator.enabled=true \
  --label-add swarmex.operator.type=postgresql \
  --label-add swarmex.operator.port=5432 \
  my-postgres
```

## Verified

PostgreSQL failover triggered correctly when the primary container was killed.

## License

Apache-2.0
