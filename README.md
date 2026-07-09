# atlasctl

Declarative management of RIPE Atlas measurements for Supabase external edge telemetry.

atlasctl applies the same workflow as Terraform or Pulumi to RIPE Atlas: write a config file describing the desired state, review a plan showing what will change, then apply the minimal set of operations. It selects probes, manages measurement lifecycles, detects drift, and maintains a state file mapping config identities to live measurement IDs.

## Background

[RIPE Atlas](https://atlas.ripe.net) is a global network of roughly 12,000 hardware probes distributed across ISPs worldwide. Each probe runs active measurements (DNS, ping, TLS, traceroute) and reports results in real time. Supabase uses RIPE Atlas to detect failures that are invisible from our own infrastructure: DNS resolution problems at specific ISPs, TCP/TLS reachability issues from particular networks, and regional outages that Supabase's internal monitoring cannot see.

The challenge is operational. RIPE Atlas measurements are created individually, probe sets drift as probes connect and disconnect, and tracking which measurement IDs belong to which logical target requires bookkeeping. atlasctl handles all of that. The operator writes a config file and runs four commands. The tool does the rest.

## Workflow

```
  atlasctl.yaml              probes/snapshot.json
       |                            |
       |   .-----------------------'
       v   v
   atlasctl refresh      fetch & cache all connected probes
       |
       v
   atlasctl select       score, rank, and assign probes to rounds
       |                 (no API calls -- operates on local snapshot)
       v
  probe lists per round
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
   atlas_exporter ---> Prometheus ---> Victoria Metrics ---> Grafana
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

Runs the probe selection algorithm against the local snapshot and prints a coverage report. Optionally writes per-round GeoJSON files for visual review in geojson.io or kepler.gl.

```
$ atlasctl select

Round high-freq: 30 probes across 30 H3 cells, 14 ASNs, 8 countries
Round mid-freq:  60 probes across 52 H3 cells, 22 ASNs, 12 countries
Round low-freq:  100 probes across 71 H3 cells, 38 ASNs, 18 countries
Total: 190 unique probes
```

`select` makes no API calls. It is cheap to run repeatedly while iterating on scoring weights or city density overrides.

### plan

Compares the desired state (config measurements with selected probes) against the current state (state file plus live RIPE Atlas API). Prints a human-readable diff and flags drift. Does not mutate anything.

```
$ atlasctl plan

KIND    NAME              ROUND      DETAILS
add     dns-canary        high-freq  id=12345678 +2 probes
remove  dns-canary        high-freq  id=12345678 -1 probes
noop    dns-canary        mid-freq   id=12345679
noop    dns-canary        low-freq   id=12345680
create  tls-canary        high-freq  target=canary.supabase.co type=tls interval=60 probes=30
stop    ping-old          low-freq   id=12345600
```

If the state file is absent (first run), `plan` treats all desired measurements as new creates.

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

A single YAML file describes rounds, measurements, and probe selection criteria.

```yaml
# Rounds define frequency tiers. Each (measurement, round) pair becomes one
# RIPE Atlas measurement.
rounds:
  - name: high-freq
    count: 30
    interval_seconds: 60
    max_probes_per_cell: 1

  - name: mid-freq
    count: 60
    interval_seconds: 300
    max_probes_per_cell: 2

  - name: low-freq
    count: 100
    interval_seconds: 900
    max_probes_per_cell: 3

# Measurements define what to measure and which rounds to apply it to.
measurements:
  - name: dns-canary
    type: dns
    target: canary.supabase.co
    rounds: [high-freq, mid-freq, low-freq]

  - name: tls-canary
    type: tls
    target: canary.supabase.co
    rounds: [high-freq, mid-freq]

  - name: ping-edge
    type: ping
    target: 162.159.36.1
    rounds: [low-freq]

# Scoring controls which probes are preferred. All sections are optional.
scoring:
  asn:
    7018: 10    # AT&T
    7922: 8     # Comcast
    28573: 8    # Claro Brazil (incident history, sparse region)
    5650: 6     # Frontier
  tags:
    office: 5
    datacentre: 4
    lte: 3
    fibre: 2
    cable: 2
    home: 1
  countries:
    BR: 5       # sparse coverage, high incident relevance
    HN: 8       # very sparse, boost hard
    US: 1
    DE: 2
  stability:
    system-ipv4-stable-90d: 5
    system-ipv4-stable-30d: 3

# Hard exclusions. These probes are never candidates.
exclude_tags:
  - broken
  - system-flakey-connection
  - system-flakey-power
  - system-ipv4-doesnt-work

# H3 hexagonal grid resolution for geographic diversity (1-15, default 3).
# Resolution 3 gives cells roughly the size of a state or province (~12,000 km2).
geo_diversity:
  h3_resolution: 3

# City overrides relax the per-cell probe limit in specific areas.
# Coordinates are always explicit (no runtime geocoding).
cities:
  - name: Ashburn
    lat: 39.04
    lon: -77.49
    radius_km: 40
    density_coefficient: 2.0
  - name: Sao Paulo
    lat: -23.55
    lon: -46.63
    radius_km: 60
    density_coefficient: 3.0
  - name: Frankfurt
    lat: 50.11
    lon: 8.68
    density_coefficient: 0.7 # Frankfurt has too many probes
  - name: Bellingham
    lat: 48.7519
    lon: -122.4787
    radius_km: 40
    score: 12 # raise score
  - name: Berlin
    lat: 52.520008
    lon: 13.404954
    radius_km: 70  # berlin is huge
    score: -12 # downweight berlin

```

## Probe selection

Probe selection is the central design problem. The RIPE Atlas probe pool has ~12,000 connected probes. atlasctl needs to pick a small subset that covers the right networks and geographies, assign them to frequency tiers, and keep those assignments stable as probes connect and disconnect over time.

### Scoring

Every probe starts with a base score of 1. Matching scoring criteria add to the score. All criteria are additive. A probe matching multiple criteria accumulates the sum.

```
  base:                      1
  asn (7018, AT&T):        +10
  tag (office):             +5
  tag (fibre):              +2
  stability (90d):          +5
  country (US):             +1
                           ---
  total:                    24
```

### Bands and deterministic assignment

Pure score-sorted selection is fragile: one probe disconnecting can cascade across all three rounds. atlasctl avoids this by discretising scores into four bands, then using a deterministic hash of the probe ID as the tiebreaker within each band.

```
  Sort key: (band DESC, FNV-1a(probe_id) ASC)

  Band A  score >= 15   High priority, multiple strong criteria
  Band B  score 8-14    Single strong criterion
  Band C  score 3-7     Moderate match
  Band D  score 1-2     Weak or base match only
```

A probe's band changes only when its score crosses a threshold (requiring a tag or ASN change on the probe itself, which is rare). Within a band, `FNV-1a(probe_id)` is perfectly stable because probe IDs are permanent integers. When a probe disconnects, only its slot opens. The next probe in hash order fills it. No cascade beyond that one slot.

### Round assignment

Rounds are filled in order of decreasing frequency. A probe selected for `high-freq` is excluded from all subsequent rounds. Each round walks the sorted candidate list (after prior-round probes are removed) and fills slots until the round's `count` is reached.

```
  high-freq  30 probes   first responders, maximum geographic spread
  mid-freq   60 probes   regional coverage, fills ASN and tag diversity
  low-freq   100 probes  depth, denser coverage in priority areas
```

### Geographic diversity

Two mechanisms work together to spread probes across the globe: continental interleaving and H3 cell limits.

#### Continental interleaving

The RIPE Atlas probe pool is heavily concentrated in the US and Europe. Left to pure score ordering, a round of 40 probes can easily fill all slots from those two regions, leaving Asia-Pacific, Latin America, and Africa unrepresented regardless of how scoring weights are tuned.

After scoring, probes are grouped into six continental zones:

| Zone  | Countries                                          |
|-------|----------------------------------------------------|
| NA    | United States, Canada                              |
| EU    | Europe (including Russia and the Caucasus)         |
| APAC  | Asia-Pacific and Oceania                           |
| LATAM | Latin America and the Caribbean                    |
| MENA  | Middle East and North Africa                       |
| SSA   | Sub-Saharan Africa                                 |

Within each band tier, the selection walk interleaves zones in round-robin order before any zone gets a second pick. The effect is easiest to see with an example. Suppose a round has these Band B candidates:

```
  NA:    [probe 1, probe 2, probe 3, probe 4, probe 5]
  EU:    [probe 6, probe 7, probe 8]
  APAC:  [probe 9]
  LATAM: [probe 10, probe 11]
  MENA:  (none in Band B)
  SSA:   (none in Band B)

  Interleaved order:
    pass 1:  NA-1,  EU-6,  APAC-9, LATAM-10
    pass 2:  NA-2,  EU-7,          LATAM-11
    pass 3:  NA-3,  EU-8
    pass 4:  NA-4
    pass 5:  NA-5
```

The H3 cell filter then applies to this reordered list. Selecting 8 probes from the example above yields 3 from NA, 3 from EU, 1 from APAC, and 1 from LATAM, rather than 5 from NA and 3 from EU.

Band priority is fully preserved: Band A probes from any zone are exhausted before Band B probes from any zone enter the walk. Within a zone, probes are ordered by the same `(band DESC, FNV-1a(probe_id) ASC)` key as before, so within-zone assignment is stable across snapshot refreshes.

#### H3 cell limits

Each probe is mapped to an H3 hexagonal cell. The `max_probes_per_cell` limit per round prevents geographic clustering within a zone. City density overrides allow a higher limit in specific metros.

| Resolution | Avg cell area  | Useful for              |
|------------|----------------|-------------------------|
| 2          | ~86,000 km2    | Large country region    |
| 3          | ~12,000 km2    | State/province (default)|
| 4          | ~1,770 km2     | Metro area              |
| 5          | ~253 km2       | City                    |

### Detection cadence

| Event scope       | Source                    | Effective cadence                                    |
|-------------------|---------------------------|------------------------------------------------------|
| Global outage     | Round 1 (30 probes)       | ~60s, multiple probes fail simultaneously            |
| Regional (US, EU) | Rounds 1+2 (90 probes)    | 60s from round 1 probes in region, 5m from round 2  |
| ISP-specific      | Rounds 1+2+3 (190 probes) | Depends on probe count in that ASN                  |
| Single city       | Mostly rounds 2+3         | 5-15 min                                             |

## Resource model

A managed measurement is identified by `(measurement_name, round_name)`. This pair maps to exactly one RIPE Atlas measurement ID.

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
| New `(name, round)` in config, no existing msm   | Create measurement     | Yes      |
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
[atlasctl:<measurement_name>:<round_name>]
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

Measurement results flow from RIPE Atlas into the Supabase observability stack via [atlas_exporter](https://github.com/czerwonk/atlas_exporter), a Prometheus exporter that subscribes to the RIPE Atlas streaming WebSocket.

```
  RIPE Atlas Streaming API (wss://atlas-stream.ripe.net/stream/)
          |
          v
    atlas_exporter    (subscribes using measurement IDs from state.yaml)
          |
          v
    Prometheus scrape
          |
          v
    Victoria Metrics  (long-term storage)
          |
          v
    Grafana           (dashboards, alerting)
```

In addition to our own measurements, RIPE Atlas has thousands of ongoing public measurements created by researchers and network operators. Subscribing to public measurements against Google DNS, Cloudflare, and major CDN endpoints provides incident correlation signals: if our canary fails from AT&T probes at the same time a public google.com measurement fails from the same probes, the failure is almost certainly network-level, not Supabase-specific.

## Constraints

- IPv4 only. Supabase edge does not currently support IPv6.
- No HTTP measurements. RIPE Atlas supports ping, DNS, TLS, traceroute, and NTP only.
- Minimum measurement interval: 60 seconds per probe.
- All RIPE Atlas measurements are publicly queryable by design.

## Package architecture

Domain logic lives in importable packages with no dependency on the CLI layer. This makes the packages usable by an external adapter (Terraform provider, Pulumi provider, or direct import) without pulling in cobra or any CLI concerns.

```
pkg/
  config/     config loading and validation
  snapshot/   probe cache: fetch, persist, load
  selection/  probe scoring, band assignment, H3 diversity, round selection
  plan/       state file, desired-vs-current diff, drift detection, apply

internal/
  goatadapter/  adapts the goat library to pkg/ interfaces

cmd/
  atlasctl/     CLI; thin wrappers over pkg/
```

The `goat` library (github.com/robert-kisteleki/goat) is the only dependency on the RIPE Atlas API. It is confined to `internal/goatadapter`. All `pkg/` packages are tested with fakes and have no goat import.

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
