package selection

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	h3 "github.com/ThingsIXFoundation/h3-light"

	"github.com/supabase/atlasctl/pkg/config"
)

// CoverageReport summarises the geographic and network diversity of a set of
// selected probe rounds. It is intended for operator review before applying.
type CoverageReport struct {
	// ProbesByRound is the number of selected probes per round name, in order.
	ProbesByRound []RoundCount `json:"probes_by_round"`
	// TotalProbes is the sum of probes across all rounds.
	TotalProbes int `json:"total_probes"`
	// UniqueH3Cells is the number of distinct H3 cells occupied at resolutions
	// 2, 3, and 4. Higher resolution = smaller cells = finer diversity signal.
	UniqueH3Cells map[int]int `json:"unique_h3_cells"`
	// Countries counts selected probes by ISO 3166-1 alpha-2 country code.
	Countries map[string]int `json:"countries"`
	// ASNs counts selected probes by IPv4 ASN.
	ASNs map[uint32]int `json:"asns"`
	// Bands counts selected probes by score band.
	// Key is the band letter (A/B/C/D) for JSON-friendliness.
	Bands map[string]int `json:"bands"`
	// Scores holds the min, median, and max raw score across all selected probes.
	// A narrow spread (min ≈ max) indicates that scoring is not discriminating the
	// probe pool and selection has degraded to continental round-robin.
	Scores ScoreStats `json:"scores"`
}

// ScoreStats summarises the distribution of raw probe scores across all
// selected probes. Used to detect degenerate scoring configurations.
type ScoreStats struct {
	Min    int `json:"min"`
	Median int `json:"median"`
	Max    int `json:"max"`
}

// RoundCount pairs a round name with its probe count.
type RoundCount struct {
	Round string `json:"round"`
	Count int    `json:"count"`
}

// h3Resolutions are the three fixed resolutions always reported.
var h3Resolutions = []int{2, 3, 4}

// Report computes a CoverageReport from a set of selected rounds.
// scoring is used to assign score bands to each probe.
//
// Note: the function signature adds a scoring parameter beyond what the plan
// sketch shows for Report(rounds []SelectedRound), because band distribution
// cannot be derived from probe data alone — it requires the config weights.
func Report(rounds []SelectedRound, scoring config.ScoringConfig) CoverageReport {
	cells := map[int]map[h3.Cell]struct{}{}
	for _, res := range h3Resolutions {
		cells[res] = make(map[h3.Cell]struct{})
	}

	countries := make(map[string]int)
	asns := make(map[uint32]int)
	bands := make(map[string]int)
	byRound := make([]RoundCount, 0, len(rounds))
	var allScores []int
	total := 0

	for _, r := range rounds {
		byRound = append(byRound, RoundCount{Round: r.Round.Name, Count: len(r.Probes)})
		total += len(r.Probes)

		for _, p := range r.Probes {
			countries[p.CountryCode]++
			if p.ASN4 != 0 {
				asns[p.ASN4]++
			}

			score := Score(p, scoring)
			bands[AssignBand(score, scoring.BandThresholds.Effective()).String()]++
			allScores = append(allScores, score)

			for _, res := range h3Resolutions {
				cells[res][h3.LatLonToCell(p.Lat, p.Lon, res)] = struct{}{}
			}
		}
	}

	uniqueCells := make(map[int]int, len(h3Resolutions))
	for _, res := range h3Resolutions {
		uniqueCells[res] = len(cells[res])
	}

	return CoverageReport{
		ProbesByRound: byRound,
		TotalProbes:   total,
		UniqueH3Cells: uniqueCells,
		Countries:     countries,
		ASNs:          asns,
		Bands:         bands,
		Scores:        computeScoreStats(allScores),
	}
}

// computeScoreStats returns min, median, and max from a slice of scores.
// Returns zero values when the slice is empty.
func computeScoreStats(scores []int) ScoreStats {
	if len(scores) == 0 {
		return ScoreStats{}
	}
	sorted := make([]int, len(scores))
	copy(sorted, scores)
	sort.Ints(sorted)
	median := sorted[len(sorted)/2]
	return ScoreStats{
		Min:    sorted[0],
		Median: median,
		Max:    sorted[len(sorted)-1],
	}
}

// JSON returns the report serialised as indented JSON.
func (r CoverageReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Format returns a human-readable text summary of the report.
func (r CoverageReport) Format() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Coverage Report\n")
	fmt.Fprintf(&b, "Total probes: %d\n", r.TotalProbes)

	fmt.Fprintf(&b, "\nProbes by round:\n")
	for _, rc := range r.ProbesByRound {
		fmt.Fprintf(&b, "  %s: %d\n", rc.Round, rc.Count)
	}

	fmt.Fprintf(&b, "\nUnique H3 cells:\n")
	for _, res := range h3Resolutions {
		fmt.Fprintf(&b, "  resolution %d: %d\n", res, r.UniqueH3Cells[res])
	}

	fmt.Fprintf(&b, "\nCountries:\n")
	for _, cc := range sortedKeys(r.Countries) {
		fmt.Fprintf(&b, "  %s: %d\n", cc, r.Countries[cc])
	}

	fmt.Fprintf(&b, "\nASNs:\n")
	asns := make([]uint32, 0, len(r.ASNs))
	for asn := range r.ASNs {
		asns = append(asns, asn)
	}
	sort.Slice(asns, func(i, j int) bool { return asns[i] < asns[j] })
	for _, asn := range asns {
		fmt.Fprintf(&b, "  %d: %d\n", asn, r.ASNs[asn])
	}

	fmt.Fprintf(&b, "\nScore bands:\n")
	for _, letter := range []string{"A", "B", "C", "D"} {
		if n, ok := r.Bands[letter]; ok {
			fmt.Fprintf(&b, "  %s: %d\n", letter, n)
		}
	}

	fmt.Fprintf(&b, "\nScore distribution:\n")
	fmt.Fprintf(&b, "  min: %d  median: %d  max: %d\n", r.Scores.Min, r.Scores.Median, r.Scores.Max)

	return b.String()
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
