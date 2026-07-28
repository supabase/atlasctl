# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added

- `ProbeSource` interface (`FileProbeSource`, `CachedProbeSource`) for probe list acquisition ([#33](https://github.com/supabase/atlasctl/pull/33))
- `MsmSpec` gains `HourlyCredits` and `DailyCredits` fields populated by `DesiredState`, so providers can read per-cohort credit burn without calling `EstimateCredits` ([#34](https://github.com/supabase/atlasctl/pull/34))
- `http` measurement type supported end-to-end; RIPE Atlas restricts scheduling to approved accounts, see docs for details
- `exclude_tags` moved from global config to `CohortCfg`; tag exclusions now travel with presets and can differ per cohort
- `h3_resolution` moved from global `geo_diversity` config to `CohortCfg`; cell granularity can now differ per cohort; global `geo_diversity` block removed

### Changed

- Namespace changes in `atlasctl.yaml` now trigger stop+create like other structural attributes ([#33](https://github.com/supabase/atlasctl/pull/33))
- `LiveDiff` is codec-aware; orphan and ghost checks use the configured namespace instead of the hardcoded default ([#33](https://github.com/supabase/atlasctl/pull/33))
