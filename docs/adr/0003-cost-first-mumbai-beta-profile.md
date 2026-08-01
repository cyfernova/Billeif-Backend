# ADR 0003: Cost-first Mumbai beta infrastructure profile

- Status: Accepted
- Date: 2026-07-30
- Scope: Initial Billeif AWS deployment in `ap-south-1`
- Implementation: Target prelaunch architecture; acceptance does not assert deployment

## Context

The initial Billeif deployment targets approximately 1,000 active users and
100 concurrent ordinary requests while keeping AWS spend below USD 125 per
month. Nothing is deployed and there is no production data, so the first
environment can adopt the intended network and database layout without an
in-place migration.

Managed high-availability components would raise the fixed monthly cost before
traffic demonstrates a need for them. The beta therefore accepts documented
single-component availability risks and pairs them with alarms, recovery
paths, restore drills, and objective exit gates.

Third-party provider charges are outside the USD 125 AWS ceiling. Regional
prices and the workload estimate must be recalculated immediately before
deployment.

## Decision

The launch Region is Mumbai, `ap-south-1`, with this beta profile:

- Two application-private subnets and two isolated database subnets span two
  Availability Zones.
- PostgreSQL starts Single-AZ on `db.t4g.micro`, with 20 GiB gp3 storage,
  autoscaling to 100 GiB, seven-day backups and point-in-time recovery,
  deletion protection, a final snapshot, encryption, and Database Insights
  Standard.
- The database accepts traffic only from the Billeif Lambda, migration, and
  optional Systems Manager tunnel security groups.
- Application and worker connection pools are deliberately small. Billeif
  connects directly to RDS at launch.
- A complete RDS Proxy option exists but is disabled until connection
  measurements justify its fixed cost.
- Private application egress uses one hardened ARM `t4g.micro` Amazon Linux
  NAT instance with encrypted storage, IMDSv2-only metadata, no SSH ingress,
  Systems Manager access, an Elastic IP, disabled source/destination checking,
  persistent forwarding rules, health supervision, status-check recovery, and
  alarms.
- A managed NAT mode remains available as an explicit switch.
- S3 and DynamoDB gateway endpoints avoid NAT traffic without an hourly
  endpoint charge. Paid SQS, Systems Manager, and Secrets Manager interface
  endpoints are deferred while traffic is low.
- Workloads that do not require PostgreSQL remain outside the VPC. A workload
  stays attached while it has a real database dependency.
- Standard-resolution alarms cover API errors, throttles, and duration; queue
  age and dead-letter queues; outbox age; database connections, CPU, memory,
  storage, and credits; NAT health and credits; and voice sessions when voice
  is enabled. They use two out of three evaluation periods and explicit
  missing-data behavior.
- A USD 125 monthly AWS Budget notifies at actual spend of 50, 80, and 100
  percent and forecast spend of 100 percent.

## Launch gate

Load increases through 20, 40, 60, 80, and 100 concurrent ordinary requests.
Billeif can launch only when the 100-concurrent stage demonstrates all of the
following:

- p95 response time is no more than 1.5 seconds;
- 5xx responses remain below 0.1 percent;
- there are no database connection errors or Lambda throttles;
- database connections remain below 70 percent of `max_connections`;
- no dead-letter queue contains a message; and
- queue age returns to baseline within five minutes after load stops.

The pricing estimate, verified sender domain, alarm recipient, backup
configuration, and restore procedure are also deployment prerequisites.

## Exit gates

These gates replace intuition with observable reasons to spend more:

| Change | Required evidence |
| --- | --- |
| Enable RDS Proxy | Connection headroom or connection churn fails the launch or operating target while database CPU and memory remain healthy |
| Upgrade PostgreSQL to `db.t4g.small` | CPU remains above 70 percent, free memory drops below 128 MiB, or CPU credits fall below 20; slow SQL is optimized first when query latency is the cause |
| Switch to managed NAT | Two egress incidents lasting more than five minutes occur within 30 days, NAT CPU or credits repeatedly saturate, or the availability target exceeds the accepted single-instance beta level |
| Enable Multi-AZ PostgreSQL | Billeif will promise availability above 99.5 percent, or a quarterly restore drill cannot meet RPO of 5 minutes and RTO of 4 hours |
| Replace the voice relay | The pilot will exceed five concurrent sessions or forecast more than USD 10 per month of voice-related AWS spend; the replacement is the ARM Fargate Go gateway defined by ADR 0002 |

## Consequences

- The beta has a materially lower fixed AWS cost and a clear path to increase
  resilience as evidence arrives.
- The NAT instance and Single-AZ database are explicit single points of
  failure. Alarms and recovery procedures reduce detection and recovery time
  but do not make them highly available.
- Database and egress upgrades are operational decisions governed by the exit
  gates, not automatic launch defaults.
- Isolated database subnets and narrow security-group relationships preserve
  network boundaries even in the cost-first profile.
- Quarterly restore drills, load tests, budget review, and pre-deployment
  price checks are part of operating the profile.

## Alternatives rejected for launch

### Managed NAT Gateway by default

Rejected for beta because its fixed cost is not justified by the accepted
availability target. It remains the defined exit path.

### RDS Proxy by default

Rejected because deliberately small pools and measured direct connections are
the lower-cost starting point. It is enabled when connection evidence, rather
than query or instance pressure, identifies the bottleneck.

### Multi-AZ PostgreSQL by default

Rejected for beta because the initial availability commitment and restore
objectives allow Single-AZ. It becomes mandatory at the stated service-level
or restore-drill gate.

### Paid interface endpoints by default

Rejected while low-volume endpoint traffic can use the NAT instance. Free S3
and DynamoDB gateway endpoints are retained.

## References

- [Enable private-resource egress with a NAT instance](https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html)
- [Gateway endpoints for Amazon S3 and DynamoDB](https://docs.aws.amazon.com/vpc/latest/privatelink/gateway-endpoints.html)
- [Plan where to use RDS Proxy](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy-planning.html)
- [Turn on Database Insights Standard mode](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_DatabaseInsights.TurningOnStandard.html)
- [Manage costs with AWS Budgets](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-managing-costs.html)
