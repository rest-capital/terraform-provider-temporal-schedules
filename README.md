# Terraform Provider: Temporal Schedules

A Terraform provider for managing [Temporal](https://temporal.io) Schedules as infrastructure-as-code.

Connection details (`address`, `namespace`, `api_key`) are configured **per-resource**, not at the provider level. This allows a single Terraform configuration to manage schedules across multiple Temporal clusters and namespaces.

## Provider Configuration

The provider block requires no attributes:

```hcl
terraform {
  required_providers {
    temporalschedules = {
      source = "rest-capital/temporal-schedules"
    }
  }
}

provider "temporalschedules" {}
```

## Resource: `temporalschedules_schedule`

### Example

```hcl
resource "temporalschedules_schedule" "daily_report" {
  namespace = "default"
  address   = "temporal.example.com:7233"
  api_key   = var.temporal_api_key
  name      = "daily-report"

  overlap_policy = "skip"
  is_paused      = false
  catchup_window = "365d"

  action {
    workflow_type = "ReportWorkflow"
    task_queue    = "report-queue"
    input_payload = jsonencode({ format = "pdf" })
  }

  spec {
    timezone = "America/New_York"

    cron_expressions = ["0 9 * * *"]

    calendar {
      hour       = "9"
      minute     = "0"
      day_of_week = "1-5"
      comment    = "Weekday mornings"
    }

    interval {
      every  = "4h"
      offset = "5m"
    }

    jitter   = "30s"
    start_at = "2025-01-01T00:00:00Z"
    end_at   = "2026-12-31T23:59:59Z"
  }

  memo = {
    team    = "platform"
    runbook = "https://wiki.example.com/daily-report"
  }
}
```

### Root Attributes

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `namespace` | String | Yes | | Temporal namespace. |
| `address` | String | Yes | | Temporal gRPC endpoint address. |
| `api_key` | String | No | | Temporal API key (sensitive). Required for Temporal Cloud. |
| `name` | String | Yes | | Schedule identifier. |
| `overlap_policy` | String | No | `"skip"` | What happens when a schedule fires while a previous run is still executing. One of: `skip`, `buffer_one`, `buffer_all`, `cancel_other`, `terminate_other`, `allow_all`. |
| `is_paused` | Boolean | No | `false` | Whether the schedule is paused. |
| `catchup_window` | String | No | | Catchup window duration (e.g. `365d`, `1h`). |
| `memo` | Map of String | No | | Arbitrary key-value metadata attached to the schedule. |

### `action` Block (Required)

| Attribute | Type | Required | Description |
|---|---|---|---|
| `workflow_type` | String | Yes | Workflow type name to execute. |
| `task_queue` | String | Yes | Task queue to dispatch the workflow to. |
| `workflow_id` | String | No | Workflow execution ID prefix. Temporal appends a timestamp suffix to this value. Defaults to the schedule name. Maximum length: 979 characters. |
| `input_payload` | String | No | JSON-encoded workflow input. |

### `spec` Block (Required)

| Attribute | Type | Required | Description |
|---|---|---|---|
| `cron_expressions` | List of String | No | Cron expressions (e.g. `["0 9 * * *"]`). |
| `timezone` | String | No | IANA timezone name (e.g. `America/New_York`). |
| `jitter` | String | No | Random jitter added to each trigger time. |
| `start_at` | String | No | Earliest trigger time (RFC 3339). |
| `end_at` | String | No | Latest trigger time (RFC 3339). |

### `calendar` Block (Optional, repeatable, inside `spec`)

| Attribute | Type | Description |
|---|---|---|
| `year` | String | Year range. |
| `month` | String | Month range (1-12). |
| `day_of_month` | String | Day of month range (1-31). |
| `day_of_week` | String | Day of week range (0-6, 0 = Sunday). |
| `hour` | String | Hour range (0-23). |
| `minute` | String | Minute range (0-59). |
| `second` | String | Second range (0-59). |
| `comment` | String | Description of this calendar spec. |

### `interval` Block (Optional, repeatable, inside `spec`)

| Attribute | Type | Required | Description |
|---|---|---|---|
| `every` | String | Yes | Repeat interval (e.g. `4h`, `30m`). |
| `offset` | String | No | Offset from the interval boundary. |

## Duration Format

Duration attributes (`catchup_window`, `jitter`, `interval.every`, `interval.offset`) accept:

- Go duration strings: `1h`, `30m`, `15s`, `1h30m45s`
- Day shorthand: `1d`, `7d`, `365d`

## Behavior Notes

### Retry on Authentication Errors

All Temporal API calls (Create, Read, Update, Delete) automatically retry on `Unauthenticated` and `PermissionDenied` gRPC errors with backoff for up to 2 minutes. This handles API key eventual consistency when using Temporal Cloud.

### Externally Deleted Schedules

If a schedule is deleted outside of Terraform (e.g. via the Temporal UI or CLI):

- **Read** removes the schedule from Terraform state, allowing Terraform to recreate it on the next `apply`.
- **Delete** succeeds silently instead of returning an error.

## Import

Existing schedules can be imported using the format `namespace/schedule-name`:

```bash
terraform import temporalschedules_schedule.example default/my-schedule
```

The `address` (and optionally `api_key`) must be set in the resource configuration before importing, as they cannot be read from the Temporal server.

## Building

```bash
make build     # compile the provider
make install   # install into $GOPATH/bin
make fmt       # format Go source
make lint      # run golangci-lint
```

### Local Development Override

To test a locally-built provider, add a dev override to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "rest-capital/temporal-schedules" = "/path/to/go/bin"
  }
  direct {}
}
```

Then run `make install` and use Terraform normally (skip `terraform init`).

## Testing

```bash
make test      # unit tests
make testacc   # acceptance tests (starts an embedded Temporal dev server)
```

Acceptance tests require no external dependencies. They start a Temporal dev server in-process using the Temporal SDK test suite.

## Releasing

Push a tag to trigger a GitHub Actions workflow that builds and publishes the release with GoReleaser:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Binaries are built for `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`.
