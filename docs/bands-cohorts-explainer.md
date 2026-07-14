# Bands, Cohorts, and Continental Coverage

These three concepts interact during probe selection and are easy to conflate. This doc explains each one in isolation, then describes how they interact.

## One config, one set of probe groups

A probe appears in exactly one cohort. Every measurement that references that cohort — DNS, TLS, ping, traceroute — runs on the identical set of probes. `dns-canary/high-freq` and `tls-canary/high-freq` share the same 30 probes by design, so their results are directly comparable.

A single `atlasctl.yaml` owns one probe selection policy and one set of groups of probes or cohorts. If you need a fundamentally different grouping — different scoring priorities, different geographic focus, different probe compositions — create a new `atlasctl.yaml` in a separate directory with its own snapshot and state file. Each config is an independent unit of management.

## Bands: a scoring stability mechanism

Every probe gets a numeric score: 1 (base) plus additive weights from ASN, country, tag, and stability config. The raw score is then bucketed into one of four bands:

| Band | Score range (default thresholds) | Meaning |
|------|----------------------------------|---------|
| A    | 15 and above                     | Strong match on multiple criteria |
| B    | 8 to 14                          | Strong match on one criterion |
| C    | 3 to 7                           | Moderate match |
| D    | 1 to 2                           | Base score only |

Bands solve a stability problem. If you sorted probes by raw score alone, a single point of score change anywhere in the pool could cascade and reshuffle the entire selection. Instead, probes are sorted by `(band DESC, FNV-1a(probe_id) ASC)`. The FNV-1a hash is stable because probe IDs are permanent integers. A probe's sort position only changes when its score crosses a band threshold, which requires a real change to its ASN or tags on RIPE Atlas — a rare event.

Bands are a mechanism internal to the selection algorithm. They are not a user-facing concept except through the coverage report and the configurable thresholds.

## Cohorts: distinct measurements from a shared pool

A cohort is a named probe group. Each cohort has a target probe count, an interval, and a position in the draw-down order. Each `(measurement, cohort)` pair becomes one RIPE Atlas measurement with its own ID.

The core primitive cohorts provide is **separate RIPE Atlas measurement IDs from a single scored probe pool, with non-overlapping probe sets**. A probe selected for the first cohort is excluded from all subsequent cohorts. This means:

- Cohort 1 gets the best available probes by score.
- Cohort 2 gets the best available probes that cohort 1 did not take.
- Cohort 3 gets the best remaining after both prior cohorts.

The cohort names (`high-freq`, `mid-freq`, `low-freq`) used throughout this documentation and the examples in `atlasctl.yaml` reflect one common use of cohorts: different intervals per cohort for cost management. Measuring your best probes more frequently and your broader coverage less frequently is a reasonable pattern, and interval-per-cohort makes it straightforward to express.

**atlasctl does not enforce or consider intervals during selection.** The interval you set on a cohort is passed directly to the RIPE Atlas API when creating the measurement and has no effect on which probes are selected or in what order. Two cohorts with identical intervals are valid. The draw-down order is determined solely by the order cohorts are defined in config.

The other reason to use multiple cohorts is downstream signal separation. Separate measurement IDs are a dimension your observability stack can use to route, alert, and visualise differently. You might page on cohort 1 failures and only record cohort 2, or present them in separate dashboard panels. The probe composition difference between cohorts (cohort 1 got higher-scoring probes) is what makes that signal distinction meaningful — but the interval difference is not required for it.

If you only need one group of probes for one measurement, one cohort is all you need. Cohorts beyond the first are only worth adding when you have a concrete reason to want separate measurement IDs.

Cohorts are the output of probe selection. Bands are an internal sorting tool. They are related — higher-band probes tend to land in earlier cohorts — but they are not the same thing.

## What happens if you have more cohorts than bands (four)?

The algorithm has no dependency on the number of cohorts vs the number of bands. Cohorts five and beyond simply draw from whatever candidates remain after earlier cohorts have claimed their probes. In practice this means later cohorts pull progressively from Band D (base score, no meaningful weighting). If you have more cohorts than you have meaningfully scored probes to fill them, later cohorts will return fewer probes than requested. That is not an error — the coverage report will show it.

Having four or fewer cohorts is conventional, not required.

## Continental interleaving: the dominant coverage force

After scoring and band assignment, `interleaveContinents` reorders the full candidate list. This step is applied once to the entire pool before any cohort selection begins. The reordering works within each band:

```
Band A pass 1:  NA-A₁  EU-A₁  APAC-A₁  LATAM-A₁  MENA-A₁  SSA-A₁
Band A pass 2:  NA-A₂  EU-A₂  APAC-A₂  ...
Band B pass 1:  NA-B₁  EU-B₁  APAC-B₁  ...
...
```

The zone order is fixed: NA, EU, APAC, LATAM, MENA, SSA. Antarctica is not included; unmapped country codes fall to NA as a safe default.

Cohort selection then walks this reordered list from the front. The first N entries become the cohort's probe set. This means:

**Continental coverage is primarily determined by probe count per cohort, not scoring weights.**

No matter how you tune scoring, if `high-freq` has a count of 3, it can produce at most probes from 3 zones. Scoring controls which probe from each zone is picked — not whether a zone is represented at all.

## Minimum probe count per cohort for continental coverage

Six zones are active. A cohort with fewer than 6 probes structurally cannot cover all zones, regardless of scoring configuration. A cohort with 6 or more probes will cover all zones that have at least one eligible probe after hard exclusions and H3 cell limits.

Practical guidance:

| Probe count | Continental coverage |
|-------------|----------------------|
| < 6         | Structurally incomplete. Some zones will be absent. |
| 6 to 11     | One probe per zone at best. Thin but globally distributed. |
| 12 to 17    | Up to two probes per zone. Meaningful redundancy begins. |
| 18+         | Solid global coverage; additional slots fill by score priority within each zone. |

A cohort with a single probe always selects a North American probe (NA is first in `zoneOrder` and dominates Band A and B due to probe pool density). This is a useful thing to know when debugging unexpected selection output.

If your `high-freq` cohort needs to detect a regional outage independently — without relying on `mid-freq` or `low-freq` to provide coverage in that region — it should have a count of at least 6, and ideally 12 or more.

## Bands and probe count interact at the coverage report

The coverage report from `atlasctl select` shows, per cohort:
- H3 cell counts at multiple resolutions
- Country and ASN histograms
- Band distribution

If a cohort shows heavy band concentration (e.g., 90% Band A) and the probe pool is large, that is expected and healthy. If it shows heavy zone concentration (e.g., 70% NA + EU), the cohort's probe count is likely too small relative to zone diversity in the pool. Increase the probe count, not the scoring weights — scoring cannot overcome a structural slot shortage.

## When scoring degrades to continental round-robin

Scoring can appear configured while providing no real discrimination. The failure mode is band collapse: most or all selected probes land in the same band, leaving the hash tiebreaker (and therefore continental interleaving) as the only ordering force. When this happens, carefully tuned ASN and tag weights are doing nothing.

### How it happens

**Weights too small to cross a band boundary.** A weight of 1 added to a base score of 1 gives score=2, still Band D. The weight exists in config but has no effect.

**Too many things weighted too similarly.** Assigning 15 different ASNs a weight of 12 each pushes all matching probes to Band B (score ~13). Within Band B they sort by hash. None of those 15 ASNs is preferred over another — the discrimination you intended is gone.

**Tag weights that are too universal.** Weighting `home`, `cable`, `dsl`, and `lte` at moderate values covers the majority of the probe pool. If 80% of probes match at least one of those tags and all score in the same range, the band histogram will show a single dominant band, and within that band selection is hash-ordered.

**No scoring config.** Every probe scores 1. All Band D. Pure continental round-robin, pure hash within each zone.

### What to look for in the coverage report

`atlasctl select` reports band counts and score distribution (min/max/median) per cohort. Two signals indicate a degenerate config:

**Band concentration.** If one band accounts for more than roughly 70-80% of selected probes, scoring is providing limited discrimination. Band D concentration is the clearest sign — it means most probes have no meaningful weights at all.

**Narrow score spread.** If the min and max scores in a cohort are close together (say, all probes score between 3 and 6), the band boundaries aren't doing useful work. A healthy config produces a spread where Band A probes score noticeably higher than Band C probes — concrete evidence that scoring is differentiating the pool.

The score spread is most informative when compared against the band thresholds. A spread of 3 to 24 with thresholds at C=3, B=8, A=15 means all four bands are active. A spread of 1 to 4 with the same thresholds means almost everything is Band D or C, and the A/B thresholds are effectively unused.

### Fixing a degenerate config

The goal is meaningful spread across at least two or three bands. A few approaches:

**Increase weights on your highest-priority criteria.** If AT&T (ASN 7018) is your most important network, give it a weight of 10-15. Other ASNs you care about but less: 5-8. This creates a real Band A vs Band B separation.

**Weight fewer things.** A config that weights 20 ASNs and 10 tags gives most probes a boost. A config that weights 3-5 ASNs and 2-3 tags creates sharper discrimination.

**Check the actual score of your priority probes.** The band histogram tells you where selected probes landed, but not whether your priority probes made it into the selection at all. If you care about AT&T probes and the report shows 0 from ASN 7018, either none were selected (H3 cell capacity) or none were in the snapshot.

### Degenerate scoring is not always wrong

If your requirements are purely geographic — you want continental spread with no network preference — then a minimal or empty scoring config is correct. The continental shuffle will do what you want. The problem is when scoring is configured with the expectation of network discrimination but the weights are too weak or too broad to deliver it.

## Summary

| Concept | Purpose | Configurable? |
|---------|---------|---------------|
| Score | Ranks probes by relevance | Yes — via `scoring:` in config |
| Band | Stability bucket for sort ordering | Yes — via `band_thresholds:` |
| Continental zone | Ensures geographic spread | No — fixed six-zone scheme |
| Cohort | Named, exclusive probe group; each (measurement, cohort) pair produces one RIPE Atlas measurement ID | Yes — via `cohorts:` in config |

The operational knobs that matter most:

1. **Cohort `probe_count`** — controls how many zones and how much per-zone depth you get. This is the primary lever for coverage.
2. **Scoring weights** — controls which probe from each zone is preferred. Tune after coverage is satisfactory.
3. **Band thresholds** — rarely need adjustment. The defaults work well for most probe pools.
