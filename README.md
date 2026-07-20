# atlasctl

Declarative management of RIPE Atlas measurements for Supabase external edge telemetry.

atlasctl applies the same workflow as Terraform or Pulumi to RIPE Atlas: write a config file describing the desired state, review a plan showing what will change, then apply the minimal set of operations. It selects probes, manages measurement lifecycles, detects drift, and maintains a state file mapping config identities to live measurement IDs.

## Background

[RIPE Atlas](https://atlas.ripe.net) is a global network of roughly 12,000 hardware probes distributed across ISPs worldwide. Each probe runs active measurements (DNS, ping, TLS, traceroute) and reports results in real time. Supabase uses RIPE Atlas to detect failures that are invisible from our own infrastructure: DNS resolution problems at specific ISPs, TCP/TLS reachability issues from particular networks, and regional outages that Supabase's internal monitoring cannot see.

The challenge is operational. RIPE Atlas measurements are created individually, probe sets drift as probes connect and disconnect, and tracking which measurement IDs belong to which logical target requires bookkeeping. atlasctl handles all of that. The operator writes a config file and runs four commands. The tool does the rest.

## Cohorts

A cohort is a named probe tier within a measurement. Each (measurement, cohort) pair becomes one RIPE Atlas measurement with its own permanent ID. This pairing is the core resource identity throughout the workflow, config, state file, and drift detection.

Cohorts within a measurement are filled in definition order. A probe selected for an earlier cohort is excluded from later cohorts within the same measurement. Different measurements select from the full probe pool independently, so the same physical probe can appear in cohorts across separate measurements.

## Workflow

```
  atlasctl.yaml              probes/snapshot.json
       |                            |
       |   .-----------------------'
       v   v
   atlasctl refresh      fetch & cache all connected probes
       |
       v
   atlasctl select       score, rank, and assign probes to cohorts
       |                 (no API calls -- operates on local snapshot)
       v
  probe lists per cohort
       |
       v
   atlasctl plan  <----  state.yaml   (measurement ID mappings)
       |               + RIPE Atlas API (live drift check)
       v
   changeset (create / update probes / stop / noop)
       |
       v
   atlasctl apply ----> RIPE Atlas API  (create, update, stop)
       |
       v
   state.yaml (updated with new measurement IDs)
       |
       v
   state.yaml (measurement IDs) ---> downstream consumers
```

`select` and `plan` are read-only. Only `apply` touches the RIPE Atlas API in a way that costs credits or modifies live measurements.

## Subcommands

### refresh

Fetches the full probe snapshot from the RIPE Atlas API and writes it to `probes/snapshot.json`. Run periodically (weekly or on demand) to pick up changes in the probe pool.

```
$ atlasctl refresh

Fetching probes... 12,847 connected probes across 24 pages
Snapshot written to probes/snapshot.json (18.2 MB)
Previous snapshot was 6 days old.
```

`refresh` has no side effects beyond writing the snapshot file. It does not touch measurements or state.

### select

Runs the probe selection algorithm against the local snapshot and prints a coverage report. Optionally writes per-cohort GeoJSON files for visual review in geojson.io or kepler.gl.

```
$ atlasctl select

Cohort high-freq: 30 probes across 30 H3 cells, 14 ASNs, 8 countries
Cohort mid-freq:  60 probes across 52 H3 cells, 22 ASNs, 12 countries
Cohort low-freq:  100 probes across 71 H3 cells, 38 ASNs, 18 countries
Total: 190 unique probes
```

`select` makes no API calls. It is cheap to run repeatedly while iterating on scoring weights or city density overrides.

### plan

Compares the desired state (config measurements with selected probes) against the current state (state file plus live RIPE Atlas API). Prints a human-readable diff and flags drift. Does not mutate anything.

```
$ atlasctl plan

KIND    NAME              cohort      DETAILS
add     dns-canary        high-freq  id=12345678 +2 probes
remove  dns-canary        high-freq  id=12345678 -1 probes
noop    dns-canary        mid-freq   id=12345679
noop    dns-canary        low-freq   id=12345680
create  tls-canary        high-freq  target=canary.supabase.co type=tls interval=60 probes=30
stop    ping-old          low-freq   id=12345600
```

If the state file is absent (first run), `plan` treats all desired measurements as new creates.

`plan` also prints a projected credit burn based on the desired state: probe count, measurement type, and interval per (measurement, cohort) pair, rolled up to daily and weekly totals.

```
CREDIT BURN (projected)
NAME         ROUND      TYPE  PROBES  INTERVAL  PER DAY
dns-canary   high-freq  dns   30      60s       432000
tls-canary   high-freq  tls   30      60s       432000
dns-canary   mid-freq   dns   60      300s      172800
dns-canary   low-freq   dns   100     900s      96000
tls-canary   mid-freq   tls   60      300s      172800
ping-edge    low-freq   ping  100     900s      28800
Total: 1334400/day  9340800/week
```

Credit costs are fixed by the RIPE Atlas platform: DNS and TLS cost 10 credits per result, ping costs 3, and traceroute costs 30. The projected total reflects the desired state after selection, not what is currently running.

### apply

Executes the plan: creates new measurements, adds and removes probes on existing ones, and stops measurements no longer in config. Writes the updated state file after all changes complete. Prompts for confirmation before executing unless `--yes` is passed.

```
$ atlasctl apply

Creating tls-canary/high-freq...
  msm 12345681 created (30 probes, 60s interval)

Updating dns-canary/high-freq (msm 12345678)...
  added 2 probes, removed 1

Stopping ping-old/low-freq (msm 12345600)...
  stopped

State written to state.yaml
Applied: 1 created, 1 updated, 1 stopped.
```

`--dry-run` logs what would happen without making any API calls.

## Config

A single YAML file describes measurements and probe selection criteria.

### Minimal cohort configuration

The only required fields per cohort are `probe_count`, `max_probes_per_cell`, and `interval_seconds`.

```yaml
measurements:
  - name: dns-canary
    type: dns
    target: canary.supabase.co
    cohorts:
      - name: high-freq
        probe_count: 30
        max_probes_per_cell: 1
        interval_seconds: 60
```

With no scoring config, every probe scores equally. The selection algorithm cycles through six continental zones (NA, EU, APAC, LATAM, MENA, SSA) in round-robin order and caps each H3 geographic cell at `max_probes_per_cell` probes. No tuning required.

### Multiple cohorts per measurement

A measurement can have multiple cohorts. Each cohort becomes one RIPE Atlas measurement with its own ID. Cohorts are filled in definition order: a probe selected for an earlier cohort is excluded from later cohorts within the same measurement.

```yaml
measurements:
  - name: dns-canary
    type: dns
    target: canary.supabase.co
    cohorts:
      - name: high-freq
        probe_count: 30
        max_probes_per_cell: 1
        interval_seconds: 60
      - name: low-freq
        probe_count: 100
        max_probes_per_cell: 3
        interval_seconds: 900
```

`high-freq` gets the 30 best-available probes. `low-freq` gets the next 100 best remaining.

### Pinning and excluding specific probes

Use `include_probe_ids` to force specific probes into a cohort regardless of scoring or H3 cell limits. Use `exclude_probe_ids` to prevent specific probes from appearing in a cohort. Both fields are per cohort.

```yaml
cohorts:
  - name: high-freq
    probe_count: 30
    max_probes_per_cell: 1
    interval_seconds: 60
    include_probe_ids: [1001, 1002]   # always selected; bypass H3 cap
    exclude_probe_ids: [9999]          # never selected in this cohort
```

### Adding scoring

A `cfg` block on a cohort controls probe scoring. Higher-scoring probes are preferred within each continental zone. All scoring criteria are additive.

```yaml
cohorts:
  - name: high-freq
    probe_count: 30
    max_probes_per_cell: 1
    interval_seconds: 60
    cfg:
      asn:
        7018: 10   # AT&T
        7922: 8    # Comcast
      tags:
        office: 5
        fibre: 2
      countries:
        BR: 5
        US: 1
      stability:
        system-ipv4-stable-90d: 5
```

A probe matching ASN 7018, tagged `office`, in Brazil, with 90-day stability scores 1 (base) + 10 + 5 + 5 + 5 = 26.

### Named presets

When multiple cohorts or measurements share the same scoring config, define named presets under `cohort_configs` and reference them with `cfg_preset`.

```yaml
cohort_configs:
  standard:
    asn:
      7018: 10
      7922: 8
    tags:
      office: 5
      fibre: 2
    stability:
      system-ipv4-stable-90d: 5

measurements:
  - name: dns-canary
    type: dns
    target: canary.supabase.co
    cohorts:
      - name: high-freq
        probe_count: 30
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: standard

  - name: tls-canary
    type: tls
    target: canary.supabase.co
    cohorts:
      - name: high-freq
        probe_count: 30
        max_probes_per_cell: 1
        interval_seconds: 60
        cfg_preset: standard
```

If both `cfg_preset` and `cfg` appear on the same cohort, the inline `cfg` wins as a complete replacement. There is no field-level merging.

### Global settings

```yaml
# Hard exclusions: probes with any of these tags are never candidates.
exclude_tags:
  - broken
  - system-flakey-connection
  - system-flakey-power
  - system-ipv4-doesnt-work

# H3 hexagonal grid resolution for geographic diversity (1-15, default 3).
# Resolution 3 gives cells roughly the size of a state or province (~12,000 km²).
geo_diversity:
  h3_resolution: 3
```

For a complete reference of all configuration fields and the selection algorithm, see [docs/selection-reference.md](docs/selection-reference.md).

## Probe selection

See [docs/selection-reference.md](docs/selection-reference.md) for the complete config reference and algorithm walkthrough. See [docs/bands-cohorts-explainer.md](docs/bands-cohorts-explainer.md) for a detailed explanation of how scoring bands, continental interleaving, and cohorts interact.

Every probe starts with a base score of 1. Per-cohort scoring config adds to that score. Scores are bucketed into four stability bands (A through D). Within each band, probes sort by a deterministic FNV-1a hash of the probe ID, making assignments stable across snapshot refreshes. Before slots are filled, probes are interleaved across six continental zones in round-robin order within each band, preventing the US and Europe-heavy pool from dominating small cohorts. The selection loop then walks the interleaved list and enforces the per-cell H3 limit.

The primary lever for coverage is `probe_count`, not scoring weights. A cohort of 6 or more probes will cover all six continental zones. Scoring controls which probe from each zone is preferred, not whether a zone is represented at all.

## Resource model

A managed measurement is identified by `(measurement_name, cohort_name)`. This pair maps to exactly one RIPE Atlas measurement ID.

```
  config identity                 RIPE Atlas
  --------------                  ----------
  dns-canary / high-freq    -->   msm 12345678
  dns-canary / mid-freq     -->   msm 12345679
  dns-canary / low-freq     -->   msm 12345680
  tls-canary / high-freq    -->   msm 12345681
```

Structural attributes (target, measurement type, interval, address family) are immutable. Changing any of them stops the old measurement and creates a new one. The probe set is mutable: probes can be added and removed on a running measurement without recreating it. This is the common change path.

| Situation                                        | Action                 | Credits  |
|--------------------------------------------------|------------------------|----------|
| New `(name, cohort)` in config, no existing msm   | Create measurement     | Yes      |
| Existing measurement, probe list changed         | Add/remove probes      | No       |
| Existing measurement, structural attribute changed| Stop old, create new  | Yes      |
| Existing measurement, no changes                 | No-op                  | No       |
| Running measurement not in config                | Stop measurement       | No       |

Credit costs per result: DNS/TLS = 10, Ping = 3, Traceroute = 30. One-off measurements cost 2x periodic.

## State tracking

`state.yaml` maps config identities to live measurement IDs. It is written by `apply` and read by `plan`.

```yaml
measurements:
  dns-canary:
    high-freq:
      msm_id: 12345678
      target: canary.supabase.co
      type: dns
      interval: 60
      probe_ids: [1001, 2002, 3003]
  tls-canary:
    high-freq:
      msm_id: 12345681
      target: canary.supabase.co
      type: tls
      interval: 60
      probe_ids: [4004, 5005]
last_applied: 2026-07-07T14:30:00Z
probe_snapshot: probes/snapshot.json
```

Every measurement atlasctl creates includes a structured description tag:

```
[atlasctl:<measurement_name>:<cohort_name>]
```

This makes managed measurements discoverable via the RIPE Atlas API even if the state file is lost:

```bash
goat fm -my -status ong -descstarts "[atlasctl:"
```

### Drift detection

`plan` compares the state file against the live API and flags discrepancies as warnings:

**Orphan.** A measurement on the API with our description tag that is not in the state file. Cause: manual creation, state file lost, or an apply that wrote to the API but crashed before saving state.

**Ghost.** A measurement ID in the state file that no longer exists on the API. Cause: manually stopped, credits exhausted, or deleted via the RIPE Atlas UI.

Drift is reported as warnings, not errors. The operator decides how to resolve.

## Downstream pipeline

`state.yaml` records the measurement IDs of all active measurements. Those IDs are the hand-off point to whatever consumes results — see the [RIPE Atlas measurements API](https://atlas.ripe.net/docs/apis/rest-api-reference/measurements/) for what is available per measurement ID.

The authors use atlasctl together with [atlas_exporter](https://github.com/czerwonk/atlas_exporter) to expose measurement results as Prometheus metrics.

In addition to managed measurements, RIPE Atlas has thousands of ongoing public measurements created by researchers and network operators. These can be consumed at no credit cost and are useful for incident correlation. If a managed canary fails from AT&T probes at the same time a public google.com measurement fails from the same probes, the failure is almost certainly network-level.

## Constraints

- No HTTP measurements. RIPE Atlas supports ping, DNS, TLS, traceroute, and NTP only.
- Minimum measurement interval: 60 seconds per probe.
- All RIPE Atlas measurements are publicly queryable by design.

## Package architecture

Domain logic lives in importable packages with no dependency on the CLI layer. This makes the packages usable by an external adapter (Terraform provider, Pulumi provider, or direct import) without pulling in cobra or any CLI concerns.

```
pkg/
  config/     config loading and validation
  snapshot/   probe cache: fetch, persist, load
  selection/  probe scoring, band assignment, H3 diversity, cohort selection
  atlasapi/   RIPE Atlas API wrapping
  plan/       state file, desired-vs-current diff, drift detection, apply

cmd/
  atlasctl/     CLI; thin wrappers over pkg/
```

The `goat` library (github.com/robert-kisteleki/goat) is the only dependency on the RIPE Atlas API. It is confined to `internal/goatadapter`. All `pkg/` packages are tested with fakes and have no goat import.

## Required API key permissions

Generate an API key at [atlas.ripe.net/keys/](https://atlas.ripe.net/keys/) and enable the following permissions:

| Permission | Purpose |
|------------|---------|
| List your measurements | Read measurement state during plan and refresh |
| Show information about your probes | Resolve probe metadata during probe selection |
| Schedule a new measurement | Create measurements on apply |

Additional permissions may be required as provider capabilities expand.


## Requirements

- Go 1.22 or later
- `RIPE_ATLAS_API_KEY` environment variable (or `--api-key` flag) for `refresh`, `plan`, and `apply`
- A RIPE Atlas account with sufficient credits for the measurements you intend to create

## Getting started

```bash
# Build the binary.
go build -o atlasctl ./cmd/atlasctl

# Fetch the probe snapshot (run weekly or on demand).
atlasctl refresh --snapshot probes/snapshot.json

# Review the probe selection against your config.
atlasctl select --config atlasctl.yaml --snapshot probes/snapshot.json

# See what would change (no API mutations).
atlasctl plan --config atlasctl.yaml --snapshot probes/snapshot.json --state state.yaml

# Apply the changes.
atlasctl apply --config atlasctl.yaml --snapshot probes/snapshot.json --state state.yaml --yes
```
