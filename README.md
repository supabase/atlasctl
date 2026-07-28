# atlasctl

Declarative management of RIPE Atlas measurements for Supabase external edge telemetry.

## Background

[RIPE Atlas](https://atlas.ripe.net) is a global network of roughly 12,000 to 14,000 hardware probes distributed across ISPs worldwide. Each probe runs active measurements (DNS, ping, TLS, traceroute, HTTP) and reports results in real time. Supabase uses RIPE Atlas to detect failures invisible from our own infrastructure: DNS resolution problems at specific ISPs, TCP/TLS reachability issues from particular networks, and regional outages that internal monitoring cannot see.

The operational challenge starts with probe selection. If you want a measurement with 50 to 200 probes, you have thousands of candidates. Picking them naively, or accepting whatever the API returns, tends to produce sets that cluster in Western Europe and North America, repeat the same ASNs, and include probes that go offline regularly. A useful measurement needs geographic spread across continental zones, ASN diversity so no single carrier dominates, and probes with a track record of stability.

atlasctl addresses this with a scoring-based autoselection algorithm. Every probe starts with a base score and you add weighted criteria on top: preferred ASNs, stability tags, country bonuses. The selection loop interleaves probes across six continental zones before filling slots, and a per-cell H3 geographic cap prevents any single city from consuming a disproportionate share of the cohort. You steer the algorithm through config. The default behavior, with no scoring config at all, still produces a geographically diverse set.

Beyond selection, atlasctl wraps the full RIPE Atlas measurement lifecycle with a Terraform-style workflow: a config file describing desired state, a plan step that shows what will change and what it will cost in credits, and an apply step that executes only the minimal set of API operations. A state file tracks which measurement IDs belong to which logical targets, and drift detection flags when the live API diverges from recorded state.

## Cohorts

A cohort is a named probe tier within a measurement. A single logical measurement can have multiple cohorts, each running at a different frequency or targeting a different slice of the probe pool. For example, a `high-freq` cohort might run 30 probes every 60 seconds for tight alerting, while a `low-freq` cohort runs 100 probes every 15 minutes for broad coverage at lower credit cost.

Each cohort is filled in definition order using its own scoring config. The first cohort gets the highest-scoring available probes. Later cohorts draw from whatever remains after earlier cohorts have claimed their probes. This means you can concentrate certain network paths or stability characteristics at the top tier and broaden coverage at lower tiers, without any probe appearing twice in the same measurement.

Each (measurement, cohort) pair becomes one RIPE Atlas measurement with its own permanent ID. This pair is the core resource identity throughout the workflow, config, state file, and drift detection. Different measurements select from the full probe pool independently, so the same physical probe can appear in cohorts across separate measurements.

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

`select` and `plan` are read-only. `select` runs probe selection and stops. `plan` runs the same selection and continues to diff against state and the live API. Only `apply` touches the RIPE Atlas API in a way that costs credits or modifies live measurements.

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

Runs the probe selection algorithm against the local snapshot and prints a coverage summary per cohort. Use it while iterating on scoring weights, probe counts, or H3 cell caps to preview what the selection would look like before running a full plan.

```
$ atlasctl select

Cohort high-freq: 30 probes across 30 H3 cells, 14 ASNs, 8 countries
Cohort mid-freq:  60 probes across 52 H3 cells, 22 ASNs, 12 countries
Cohort low-freq:  100 probes across 71 H3 cells, 38 ASNs, 18 countries
Total: 190 unique probes
```

`select` is a subset of what `plan` does. It runs the same probe selection algorithm and stops there. `plan` takes those same probe lists and continues to diff them against the state file and the live API. `select` makes no API calls and has no dependencies beyond the local snapshot, so it is cheap to run repeatedly.

### plan

Runs probe selection (the same algorithm as `select`), then compares the resulting desired state against the state file and the live RIPE Atlas API. Prints a human-readable diff and flags drift. Does not mutate anything.

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

Use `include_probe_ids` to force specific probes into a cohort. Pinned probes are selected unconditionally: they bypass H3 cell limits, `exclude_probe_ids`, tag exclusions, and inter-cohort exclusions. The only reason a pinned probe is skipped is if it is absent from the snapshot. Use `exclude_probe_ids` to prevent specific probes from appearing during scored selection. Both fields are per cohort.

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
    h3_resolution: 3   # state/province granularity (default — explicit here for clarity)
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

### Tag exclusions

Tag-based probe exclusion is per-cohort via `exclude_tags` in a `CohortCfg` block. Probes carrying any listed tag are skipped during scored selection for that cohort. Probes pinned via `include_probe_ids` are not affected.

Because exclusions are part of `CohortCfg`, they travel with named presets:

```yaml
cohort_configs:
  standard:
    exclude_tags:
      - broken
      - system-flakey-connection
      - starlink
    asn:
      7018: 10
    stability:
      system-ipv4-stable-90d: 5

  satellite:
    # No exclude_tags — starlink probes are candidates here.
    asn:
      SpaceX: 20
```

Any cohort using `cfg_preset: standard` excludes those tags. A cohort using `cfg_preset: satellite` or its own inline `cfg` block is unaffected. This makes it possible to exclude a provider from most cohorts while targeting it explicitly in another.

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

Credit costs per result: DNS/TLS/HTTP = 10, Ping = 3, Traceroute = 30. One-off measurements cost 2x periodic.

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

## Terraform and Pulumi providers

If your infrastructure is already managed with Terraform or Pulumi, atlasctl is a good way to get started. Use it interactively to work out your probe selection parameters, scoring config, and cohort structure. The `select` command gives fast feedback while you tune weights and counts, and `plan` shows exactly what the API would see before you commit to anything.

Once the config is stable, the same concepts map directly into declarative infrastructure code. Everything expressible in `atlasctl.yaml` can be expressed in HCL or Pulumi code and tracked in your existing state:

- [supabase/terraform-provider-ripe-atlas](https://github.com/supabase/terraform-provider-ripe-atlas)
- [supabase/pulumi-atlas](https://github.com/supabase/pulumi-atlas)

The providers use the same namespace, cohort, and probe selection model. Measurements created through either provider carry the same description tag format and are visible to drift detection in atlasctl if you share a namespace.

## Constraints

- HTTP measurements (plain HTTP 1.x only) are supported by atlasctl and the RIPE Atlas platform, but RIPE Atlas restricts HTTP measurement creation to researchers and other approved account tiers. Most API keys will receive a 403 when trying to schedule one. Use `tls` to test HTTPS reachability. The `http` type is available in config for accounts where the platform grants it.
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
make build

# Fetch the probe snapshot (run weekly or on demand).
./atlasctl refresh --snapshot probes/snapshot.json

# Review the probe selection against your config.
./atlasctl select --config atlasctl.yaml --snapshot probes/snapshot.json

# See what would change (no API mutations).
./atlasctl plan --config atlasctl.yaml --snapshot probes/snapshot.json --state state.yaml

# Apply the changes.
./atlasctl apply --config atlasctl.yaml --snapshot probes/snapshot.json --state state.yaml --yes
```
