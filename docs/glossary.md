# Glossary

Terms used throughout atlasctl documentation, CLI output, config files, and state files.

**apply** — executes the changeset: creates, updates, and stops measurements on the RIPE Atlas API, then writes state.yaml with the results.

**band** — stability tier (A/B/C/D) derived from a probe's score. Controls sort order within the candidate list using a deterministic FNV-1a hash of the probe ID as tiebreaker within each tier. A probe's band changes only when its score crosses a threshold, which requires a tag or ASN change on the probe itself.

**changeset** — the output of `plan`: a list of create, update (add/remove probes), stop, and noop operations needed to reconcile desired state with current state.

**cohort** — per-measurement tier with its own probe count, interval, and ordering config. Cohorts within a measurement are filled in definition order; a probe selected for one cohort is excluded from all subsequent cohorts within that same measurement. Different measurements select from the full probe pool independently, so the same physical probe can appear in cohorts across separate measurements. Every (measurement, cohort) pair maps to one RIPE Atlas measurement ID.

**continental zone** — one of six fixed geographic regions used during selection to interleave probes across continents before cohort slots are filled. Fixed order: NA, EU, APAC, LATAM, MENA, SSA.

**credit** — RIPE Atlas platform currency consumed per measurement result. Costs: DNS and TLS 10 per result, ping 3, traceroute 30. One-off measurements cost 2x the periodic rate.

**description tag** — a structured string embedded in each RIPE Atlas measurement description by atlasctl (`[atlasctl:<name>:<cohort>]`). Marks a measurement as managed and allows recovery if state.yaml is lost. The namespace is configurable via `namespace` in config.

**drift** — discrepancy between state.yaml and the live RIPE Atlas API. Two kinds: orphan (a live tagged measurement absent from state) and ghost (a state record whose measurement ID is no longer live). Reported as warnings by `plan`.

**measurement** (atlasctl config) — a named intent to observe a target using a specific type (dns, tls, ping, traceroute). Combined with a cohort, it produces one RIPE Atlas measurement.

**measurement** (RIPE Atlas) — a live platform object with a permanent integer ID that runs active measurements from a set of probes at a fixed interval. The physical resource atlasctl creates and manages.

**plan** — read-only command that computes and displays the changeset without touching the API. Also performs a live drift check against the RIPE Atlas API.

**probe** — a physical RIPE Atlas hardware device with a permanent integer ID, ASN, country code, coordinates, and a set of tags. The unit of selection. probe IDs are permanent; probes connect and disconnect but their IDs never change.

**probe tag** — a label applied to a probe by RIPE Atlas or the probe operator describing its connectivity type or environment (e.g. `office`, `cable`, `lte`, `system-ipv4-stable-90d`). Distinct from description tag. Full taxonomy: `probe_tags.txt`.

**report** — coverage summary produced by `atlasctl select`: H3 cell counts at resolutions 2/3/4, country and ASN histograms, band distribution, and score statistics (min/median/max). Used to evaluate probe selection quality before applying.

**score** — numeric rank assigned to a probe by the scoring algorithm. Base score is 1. Matching scoring criteria (ASN, country, tag, stability) add to it. All criteria are additive. Score determines which band a probe falls into.

**selection** — the full process run by `atlasctl select`: build a filtered probe pool from the snapshot, then for each measurement independently score every probe using that measurement's cohort config, assign bands, apply H3 cell limits, interleave by continental zone within each band, and fill the measurement's cohorts from the resulting ordered list. Each measurement runs its own independent selection; the same physical probe can be selected for multiple measurements.

**snapshot** — a cached JSON dump of all connected RIPE Atlas probes, written by `atlasctl refresh`. Selection reads the snapshot and makes no network calls. Refresh weekly or on demand to pick up changes in the probe pool.

**state** — `state.yaml`. Maps (measurement, cohort) identities to live RIPE Atlas measurement IDs, probe lists, and structural attributes (target, type, interval, address family). Written by `apply`, read by `plan`.

**zone** — see *continental zone*.
