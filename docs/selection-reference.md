# Selection Reference

atlasctl selects probes from the RIPE Atlas probe pool and assigns them to cohorts. This document is a complete reference for probe selection configuration and the algorithm that drives it.

For a conceptual walkthrough of how bands, cohorts, and continental coverage interact, including how to diagnose weak scoring configurations, see [bands-cohorts-explainer.md](bands-cohorts-explainer.md).

## Configuration reference

### Top-level fields

| Field | Type | Default | Description |
|---|---|---|---|
| `snapshot` | string | | Path to the probe snapshot file written by `atlasctl refresh` |
| `state` | string | | Path to the state file read and written by `plan` and `apply` |
| `namespace` | string | `atlasctl` | Namespace embedded in RIPE Atlas measurement descriptions and tags. Used to scope managed measurements and filter by ownership during drift detection |
| `cohort_configs` | map | | Named `CohortCfg` presets for reuse across cohorts. Referenced via `cfg_preset` |
| `measurements` | list | | The measurements atlasctl will manage |

### Measurement fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique identifier. Combined with cohort name to form the RIPE Atlas description tag |
| `type` | string | yes | `dns`, `tls`, `ping`, `traceroute`, or `http` |
| `target` | string | yes | Hostname or IP address to measure |
| `af` | int | no | Address family: 4 or 6. Defaults to the RIPE Atlas platform default |
| `query_type` | string | no | DNS query type. DNS measurements only. Legacy alias for `dns.query_type` |
| `http_method` | string | no | HTTP method: `GET`, `HEAD` (default), or `POST`. HTTP measurements only |
| `http_path` | string | no | URL path. HTTP measurements only. Defaults to `/` |
| `http_port` | int | no | TCP port. HTTP measurements only. Defaults to `80` |
| `http_version` | string | no | HTTP version: `1.0` or `1.1`. HTTP measurements only |
| `common` | block | no | Options shared by every measurement type. See [Measurement options](#measurement-options) |
| `ping`, `traceroute`, `dns`, `tls`, `http` | block | no | Options for one measurement type. A block is only valid on a measurement of the same `type` |
| `cohorts` | list | yes | The cohort tiers for this measurement |

**Note on HTTP access.** RIPE Atlas restricts `http` measurement creation to researchers and other approved account tiers. Most API keys will receive a 403 from the RIPE Atlas API when apply runs. The `http` type is fully supported by atlasctl and the goat library for accounts where the platform grants it. Use `tls` to test HTTPS reachability without this restriction.

### Measurement options

Every option is optional. An option that is not set is left out of the RIPE Atlas request, so the platform default applies. Changing any option on a running measurement stops it and creates a new one, because the options cannot be edited in place.

```yaml
measurements:
  - name: api-dns
    type: dns
    target: api.example.com
    common:
      spread: 30
      resolve_on_probe: true
    dns:
      query_type: AAAA
      set_rd_bit: true
      retry: 2
    cohorts:
      - name: default
        probe_count: 50
        max_probes_per_cell: 2
        interval_seconds: 300
```

#### `common` (all types)

| Field | Type | Description |
|---|---|---|
| `resolve_on_probe` | bool | Resolve the target on each probe instead of once centrally |
| `spread` | int | Seconds over which probe start times are spread within one interval |
| `skip_dns_check` | bool | Skip the platform's DNS check of the target at creation time |
| `target_update_hours` | int | Re-resolve the target every N hours |
| `auto_topup` | bool | Replace probes that go offline. Implied by the next two fields |
| `auto_topup_prb_days_off` | int | Days offline before a probe is replaced |
| `auto_topup_prb_similarity` | float | Similarity of the replacement probe, between 0 and 1 |
| `start_time` | string | RFC 3339 start time |
| `stop_time` | string | RFC 3339 stop time. Must be after `start_time` |
| `bill_to` | string | Charge credits to another RIPE Atlas account instead of your own. That account must have added you to its allowed billing list |

A measurement that reaches its `stop_time` ends on its own. The next `apply` sees it missing and tries to create it again with a stop time that has already passed, which is unlikely to be accepted. Remove `stop_time` or the measurement from the config when it ends.

#### `ping`

| Field | Type | Description |
|---|---|---|
| `packets` | int | Packets per result. Platform default 3 |
| `packet_interval` | int | Milliseconds between the packets of one result |
| `include_probe_id` | bool | Put the probe ID (as ASCII digits) in the packet payload |

#### `traceroute`

| Field | Type | Description |
|---|---|---|
| `protocol` | string | `ICMP`, `UDP`, or `TCP`. atlasctl defaults to `UDP`, the platform default is `ICMP` |
| `response_timeout` | int | Milliseconds to wait for a reply to each packet. Platform default 4000 |
| `packets` | int | Packets sent per hop. Platform default 3 |
| `paris` | int | Number of distinct flow identifiers to cycle through, so that load balanced paths are explored consistently (Paris traceroute). Platform default 16 |
| `first_hop` | int | TTL of the first hop probed. Platform default 1 |
| `max_hops` | int | TTL of the last hop probed. Platform default 32 |
| `destination_option_size` | int | Size in bytes of an IPv6 destination options extension header added to each packet. Platform default 0 (none) |
| `hop_by_hop_option_size` | int | Size in bytes of an IPv6 hop by hop options extension header added to each packet. Platform default 0 (none) |
| `dont_fragment` | bool | Set the don't fragment bit on outgoing packets. Platform default false |

#### `dns`

The target is sent as the query argument and the probe's own resolver is always used.

| Field | Type | Description |
|---|---|---|
| `query_type` | string | Record type such as `A` (default), `AAAA`, `NS`, `MX`, `TXT`. Takes priority over the flat `query_type` key, and the two must agree if both are set |
| `query_class` | string | `IN` (default) or `CHAOS` |
| `protocol` | string | `UDP` (default) or `TCP` |
| `use_macros` | bool | Expand `$p` (probe ID), `$r` (random string) and `$t` (timestamp) in the target, for example to build unique names that bypass resolver caches |
| `set_nsid_bit` | bool | Ask the server to identify itself with the EDNS NSID option (RFC 5001), commonly used to see which anycast instance answered |
| `include_qbuf` | bool | Include the query packet sent by the probe in each result |
| `prepend_probe_id` | bool | Prefix the queried name with the probe ID, giving a unique name per probe |
| `set_rd_bit` | bool | Set the recursion desired bit |
| `set_do_bit` | bool | Set the DNSSEC OK bit |
| `udp_payload_size` | int | Maximum UDP response size in bytes advertised through EDNS0. Platform default 512 |
| `retry` | int | Number of times a query is retried after a failure. Platform default 0 |
| `timeout` | int | Milliseconds to wait for a response before the query counts as failed. Platform default 5000 |

#### `tls`

| Field | Type | Description |
|---|---|---|
| `port` | int | TCP port. Platform default 443 |
| `hostname` | string | Server name sent as SNI. Useful when the target is an IP address |

#### `http`

These extend the flat `http_*` keys above.

| Field | Type | Description |
|---|---|---|
| `query_string` | string | Query string appended to the request path (without the leading `?`) |
| `header_bytes` | int | Maximum number of response header bytes to record in the result |
| `extended_timing` | bool | Report additional timing detail in each result (for example time to resolve and time to first byte) |
| `more_extended_timing` | bool | Report even more timing detail than `extended_timing` |

#### Platform fields not yet supported

The goat library atlasctl uses cannot send these, so they are not available: `is_public`, packet `size` for ping, traceroute and NTP, traceroute `port`, `duplicate_timeout`, `traffic_class` and `gap_limit`, DNS `tls`, `port`, `cookies`, `ttl`, `set_ad_bit`, `set_cd_bit`, `default_client_subnet` and `include_abuf`, and HTTP `user_agent`, `max_bytes_read`, `host` and `https`. `is_oneoff` is also not offered, because a finished one off measurement would be recreated on every apply.

Credit estimates use a fixed cost per result for each type and do not account for options such as `packets` that change the real cost.

### Cohort fields

Each entry in a measurement's `cohorts` list:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Unique within the measurement. Part of the RIPE Atlas measurement identity |
| `probe_count` | int | yes | Target number of probes to select |
| `max_probes_per_cell` | int | yes | Maximum probes allowed from the same H3 cell |
| `interval_seconds` | int | yes | Measurement interval passed to the RIPE Atlas API. Minimum: 60 |
| `include_probe_ids` | list of int | no | Probe IDs that are always selected first, regardless of score, H3 cap, `exclude_probe_ids`, `cfg.exclude_tags`, or inter-cohort exclusions. The only reason a pinned probe is skipped is if it is absent from the snapshot |
| `exclude_probe_ids` | list of int | no | Probe IDs to skip during scored selection. Has no effect on probes listed in `include_probe_ids` |
| `cfg_preset` | string | no | Name of a preset defined in `cohort_configs`. Used unless `cfg` is also present |
| `cfg` | CohortCfg | no | Inline ordering config. Takes priority over `cfg_preset` if both are specified |

A cohort with no `cfg` or `cfg_preset` uses base scoring (all probes score 1). Continental interleaving and H3 limits are still active.

### CohortCfg fields

The body of a `cfg` block or a named preset in `cohort_configs`:

| Field | Type | Description |
|---|---|---|
| `exclude_tags` | list of string | Probes carrying any of these tags are skipped during scored fill for this cohort. Has no effect on probes pinned via `include_probe_ids`. See [Tag exclusions](#tag-exclusions) |
| `asn` | map of int to int | Per-ASN score weights. A probe whose ASN matches receives the specified weight added to its score |
| `tags` | map of string to int | Per-tag score weights. Weights for all matching tags are summed |
| `countries` | map of string to int | Per-country score weights. Two-letter ISO 3166-1 alpha-2 codes |
| `stability` | map of string to int | Per-stability-tag weights. Common keys: `system-ipv4-stable-90d`, `system-ipv4-stable-30d` |
| `band_thresholds.a` | int | Minimum score for Band A. Default: 15 |
| `band_thresholds.b` | int | Minimum score for Band B. Default: 8 |
| `band_thresholds.c` | int | Minimum score for Band C. Default: 3 |
| `cities` | list of CityConfig | City-specific score bonuses and H3 density overrides |
| `h3_resolution` | int | H3 hexagonal cell resolution for this cohort (1-15). Default 3 when omitted or zero. See [H3 cell reference](#h3-cell-reference) |
| `disable_continental_shuffle` | bool | If true, skips continental interleaving. Probes sort by (band, hash) only |

### CityConfig fields

Each entry in `cfg.cities`:

| Field | Type | Description |
|---|---|---|
| `name` | string | Label for the city (used in reports) |
| `lat` | float | Latitude of the city center |
| `lon` | float | Longitude of the city center |
| `radius_km` | float | Radius in km around the center. Probes within this radius are affected |
| `score` | int | Score bonus added to probes within the radius. Can be negative to downweight |
| `density_coefficient` | float | Multiplier applied to `max_probes_per_cell` for H3 cells within the radius. Values above 1.0 allow more probes per cell; values below 1.0 restrict them |

City coordinates are always explicit. atlasctl does not perform runtime geocoding.

## Selection algorithm

Selection runs independently for each measurement. Every measurement scores and selects from the full probe pool on its own. The same physical probe can appear in cohorts across different measurements.

### Step 1: Build the probe pool

The full set of connected probes from the snapshot is loaded into a single immutable pool shared across all measurements. Per-cohort tag exclusions (`CohortCfg.exclude_tags`) are applied later, inside the selection loop for each cohort, not here.

### Step 2: Score probes (per cohort)

For each cohort, every probe in the pool receives a numeric score:

```
score = 1 (base)
      + asn_weight          (if probe's ASN is in cfg.asn)
      + sum(tag_weights)    (for each matching tag in cfg.tags)
      + country_weight      (if probe's country code is in cfg.countries)
      + stability_weight    (for each matching key in cfg.stability)
      + city_score_bonus    (if probe is within any city's radius in cfg.cities)
```

All criteria are additive. Every probe scores at least 1. There is no cap.

Example:

```
  base:                          1
  asn 7018 (AT&T):             +10
  tag "office":                 +5
  tag "fibre":                  +2
  stability "ipv4-stable-90d":  +5
  country BR:                   +5
                               ---
  total:                        28
```

### Step 3: Assign bands

Scores are bucketed into four bands using `cfg.band_thresholds` (defaults: A >= 15, B >= 8, C >= 3, D = 1-2):

| Band | Score range (default thresholds) | Meaning |
|---|---|---|
| A | 15 and above | Strong match on multiple criteria |
| B | 8 to 14 | Strong match on one criterion |
| C | 3 to 7 | Moderate match |
| D | 1 to 2 | Base score only |

Bands are a stability mechanism: a probe's sort position only shifts when its score crosses a threshold. See [bands-cohorts-explainer.md](bands-cohorts-explainer.md) for the full explanation of how this prevents cascade reassignment.

### Step 4: Order probes (per cohort)

Probes are sorted by `(band DESC, FNV-1a(probe_id) ASC)`. The FNV-1a hash of the permanent integer probe ID is a stable tiebreaker: its value does not change between runs. When a probe disconnects, only its slot opens. The next probe in hash order fills it with no cascading effect.

Unless `disable_continental_shuffle` is set on the cohort's `cfg`, the sorted list is reordered using continental interleaving. Within each band tier, probes are interleaved across six zones in round-robin:

```
NA, EU, APAC, LATAM, MENA, SSA
```

This prevents the US and Europe-heavy probe pool from filling all slots in a small cohort. Example output with Band B candidates:

```
  NA:    [probe 1, probe 2, probe 3, probe 4, probe 5]
  EU:    [probe 6, probe 7, probe 8]
  APAC:  [probe 9]
  LATAM: [probe 10, probe 11]
  MENA:  (none in Band B)
  SSA:   (none in Band B)

  Interleaved:
    pass 1:  NA-1,  EU-6,  APAC-9, LATAM-10
    pass 2:  NA-2,  EU-7,          LATAM-11
    pass 3:  NA-3,  EU-8
    pass 4:  NA-4
    pass 5:  NA-5
```

The orderer caches its result per `(probe pool, CohortCfg)` pair. Cohorts within the same measurement that share identical `CohortCfg` values reuse the cached result without rescoring.

### Step 5: Fill cohort slots

The selection loop processes each cohort in definition order, maintaining a shared inter-cohort exclusion set across cohorts within the same measurement.

For each cohort:

1. Build the effective exclusion set: union of the inter-cohort excluded set and the cohort's `exclude_probe_ids`.
2. Derive per-cell density coefficients from `cfg.cities` using each city's `density_coefficient` and `radius_km`.
3. Process `include_probe_ids` first. For each ID present in the snapshot: add it to the selected list unconditionally. Pinned probes bypass the H3 cell cap, `exclude_probe_ids`, `cfg.exclude_tags`, and inter-cohort exclusions. Their IDs are added to the exclusion set to prevent double-selection during the fill step.
4. Walk the orderer's list to fill remaining slots. For each probe: skip if excluded; skip if it matches `cfg.exclude_tags`; skip if the probe's H3 cell is at capacity; otherwise add to selected, mark the cell occupied, add the probe to the exclusion set.
5. Add all selected probe IDs to the inter-cohort exclusion set for subsequent cohorts within this measurement.

H3 cell occupancy and city density coefficients reset per cohort. A cell at capacity in `high-freq` starts empty again for `low-freq`.

## H3 cell reference

H3 is Uber's hexagonal hierarchical spatial index. Each probe maps to a hexagonal cell at the configured resolution. `max_probes_per_cell` caps how many probes from the same cell can be selected.

| Resolution | Avg cell area | Useful for |
|---|---|---|
| 2 | ~86,000 km² | Large country region |
| 3 | ~12,000 km² | State or province (default) |
| 4 | ~1,770 km² | Metro area |
| 5 | ~253 km² | City |

`h3_resolution` is per cohort in `CohortCfg` (default 3 when omitted or zero). Different cohorts within the same measurement can use different resolutions. The `density_coefficient` in a city's config scales the effective cap for cells within that city's radius. A coefficient of 2.0 allows twice as many probes per cell. A coefficient of 0.5 halves it.

## Continental zones

| Zone | Countries |
|---|---|
| NA | United States, Canada |
| EU | Europe (including Russia and the Caucasus) |
| APAC | Asia-Pacific and Oceania |
| LATAM | Latin America and the Caribbean |
| MENA | Middle East and North Africa |
| SSA | Sub-Saharan Africa |

The zone order is fixed: NA, EU, APAC, LATAM, MENA, SSA. Antarctica is not included. Probes with unmapped country codes fall to NA as a safe default.

See [bands-cohorts-explainer.md](bands-cohorts-explainer.md) for minimum probe count guidance per coverage goal.

## Tag exclusions

`CohortCfg.exclude_tags` is a list of probe tags. Any probe carrying at least one of those tags is skipped during scored fill for that cohort. Probes pinned via `include_probe_ids` are not affected — pinned IDs take precedence over all exclusions.

Because `exclude_tags` is part of `CohortCfg`, it travels with named presets. The typical pattern is to put universally-unwanted tags in a shared preset and leave them out of any cohort that intentionally targets those probes:

```yaml
cohort_configs:
  standard:
    exclude_tags:
      - broken
      - system-flakey-connection
      - starlink         # satellite probes — poor geo signal for most cohorts

  satellite:
    # No exclude_tags. Starlink probes are candidates here.
    asn:
      SpaceX: 20
```

Both cohorts draw from the same full probe pool. The `standard` preset filters at selection time; the `satellite` preset does not. No global exclusion is needed.

## Named presets

Presets avoid repeating the same `cfg` block across many cohorts.

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
    cities:
      - name: Ashburn
        lat: 39.04
        lon: -77.49
        radius_km: 40
        density_coefficient: 2.0
        score: 10

  latam-focus:
    disable_continental_shuffle: true
    countries:
      BR: 15
      AR: 10
      CL: 8
    cities:
      - name: Sao Paulo
        lat: -23.55
        lon: -46.63
        radius_km: 60
        density_coefficient: 3.0
        score: 20
```

Reference a preset with `cfg_preset: latam-focus`. If the cohort also has an inline `cfg` block, the inline block wins entirely. There is no field-level merging between a preset and an inline block.

This lets a specific cohort diverge from the shared preset without requiring a second named preset for a minor variation.

## Detection cadence

Detection speed depends on how many probes cover the affected region and at what interval they run.

| Event scope | Coverage source | Notes |
|---|---|---|
| Global outage | All cohorts | As fast as your highest-frequency cohort's interval |
| Regional (continent-level) | Cohorts with probe_count >= 6 | Zones with no probes cannot detect regional failures |
| ISP-specific | Cohorts with that ASN weighted | Depends on how many matching probes were selected |
| Single city | City-boosted cohorts | Depends on city config, radius, and probe density in area |

A cohort with `probe_count` below 6 structurally cannot cover all zones. Increasing the count is always more effective than adjusting scoring weights for coverage goals.

## Diagnosing weak scoring

If the coverage report shows most probes in the same band, scoring is not differentiating the pool. See [bands-cohorts-explainer.md](bands-cohorts-explainer.md) for causes, diagnostic signals in the coverage report, and how to fix a degenerate config.
