package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/supabase/atlascli/pkg/plan"
)

// printCreditEstimate writes a per-(measurement, round) credit burn table and
// the daily/weekly totals to w.
func printCreditEstimate(w io.Writer, est plan.CreditEstimate) {
	fmt.Fprintln(w, "\nCREDIT BURN (projected)")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tROUND\tTYPE\tPROBES\tINTERVAL\tPER DAY")
	for _, l := range est.Lines {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%ds\t%d\n",
			l.Key.Name, l.Key.Round, l.Type, l.ProbeCount, l.IntervalSecs, l.PerDay)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "Total: %d/day  %d/week\n", est.Daily, est.Weekly)
}

// printChangeset writes the changeset to w as an aligned table.
func printChangeset(w io.Writer, cs plan.Changeset) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tNAME\tROUND\tDETAILS")
	for _, ch := range cs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			ch.Kind, ch.Key.Name, ch.Key.Round, changeDetails(ch))
	}
	_ = tw.Flush()
}

// printWarnings writes drift warnings to w, if any.
func printWarnings(w io.Writer, warnings []plan.DriftWarning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w, "\nWARNINGS")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, warn := range warnings {
		fmt.Fprintf(tw, "  %s\t%s\t%s\tid=%d\t%s\n",
			warn.Kind, warn.Key.Name, warn.Key.Round, warn.MsmID, warn.Message)
	}
	_ = tw.Flush()
}

func changeDetails(ch plan.Change) string {
	switch ch.Kind {
	case plan.ChangeCreate:
		d := ch.Desired
		return fmt.Sprintf("target=%s type=%s interval=%d probes=%d",
			d.Target, d.Type, d.Interval, len(d.ProbeIDs))
	case plan.ChangeStop:
		return fmt.Sprintf("id=%d", ch.MsmID)
	case plan.ChangeAddProbes:
		return fmt.Sprintf("id=%d +%d probes", ch.MsmID, len(ch.ProbeIDs))
	case plan.ChangeRemoveProbes:
		return fmt.Sprintf("id=%d -%d probes", ch.MsmID, len(ch.ProbeIDs))
	case plan.ChangeNoOp:
		return fmt.Sprintf("id=%d", ch.MsmID)
	default:
		return ""
	}
}
