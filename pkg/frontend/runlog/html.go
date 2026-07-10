package runlog

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	readmodelrunlog "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/readmodel/runlog"
	frontui "github.com/roivaz/ARO-HCP-CIHealth/pkg/frontend/ui"
	sourceoptions "github.com/roivaz/ARO-HCP-CIHealth/pkg/source/options"
	"github.com/roivaz/ARO-HCP-CIHealth/pkg/source/prowartifacts"
	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"
)

type PageOptions struct {
	Chrome              frontui.ReportChromeOptions
	Query               readmodelrunlog.RunLogDayQuery
	FailurePatternsHref string
}

func RenderHTML(
	data readmodelrunlog.RunLogDayData,
	options PageOptions,
) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString("<html lang=\"en\">\n")
	b.WriteString("<head>\n")
	b.WriteString("  <meta charset=\"utf-8\" />\n")
	b.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n")
	b.WriteString("  <title>CIHealth Run Log</title>\n")
	b.WriteString(frontui.ThemeInitScriptTag())
	b.WriteString("  <style>\n")
	b.WriteString("    body { font-family: Arial, sans-serif; margin: 0; color: #1f2937; }\n")
	b.WriteString("    h1 { margin-bottom: 6px; }\n")
	b.WriteString("    h2 { margin-top: 22px; margin-bottom: 8px; }\n")
	b.WriteString("    .meta { color: #4b5563; margin-bottom: 8px; }\n")
	b.WriteString("    .cards { display: flex; flex-wrap: wrap; gap: 10px; margin: 14px 0 18px; }\n")
	b.WriteString("    .card { border: 1px solid #e5e7eb; border-radius: 8px; background: #f9fafb; padding: 10px 12px; min-width: 180px; }\n")
	b.WriteString("    .label { font-size: 12px; color: #6b7280; margin-bottom: 3px; }\n")
	b.WriteString("    .value { font-size: 20px; font-weight: 700; }\n")
	b.WriteString("    .section { border: 1px solid #e5e7eb; border-radius: 8px; padding: 12px; margin: 14px 0; }\n")
	b.WriteString("    .section-note { color: #4b5563; font-size: 12px; margin-top: -4px; margin-bottom: 8px; }\n")
	b.WriteString("    .muted { color: #6b7280; }\n")
	b.WriteString("    .page-actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 12px 0 18px; }\n")
	b.WriteString("    .action-btn { display: inline-flex; align-items: center; justify-content: center; border-radius: 999px; padding: 8px 14px; font-size: 13px; font-weight: 600; text-decoration: none; }\n")
	b.WriteString("    .action-primary { border: 1px solid #111827; background: #111827; color: #ffffff; }\n")
	b.WriteString("    .action-primary:hover { background: #1f2937; }\n")
	b.WriteString("    .action-secondary { border: 1px solid #d1d5db; background: #ffffff; color: #1f2937; }\n")
	b.WriteString("    .action-secondary:hover { background: #f3f4f6; }\n")
	b.WriteString("    .run-search { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin: 6px 0 16px; }\n")
	b.WriteString("    .run-search-input { flex: 1 1 360px; min-width: 240px; max-width: 680px; padding: 8px 14px; border: 1px solid #d1d5db; border-radius: 999px; font-size: 13px; color: #1f2937; background: #ffffff; }\n")
	b.WriteString("    .run-search-input:focus { outline: none; border-color: #2563eb; box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.15); }\n")
	b.WriteString("    .run-search-status { color: #6b7280; font-size: 12px; white-space: nowrap; }\n")
	b.WriteString("    .run-search-empty { color: #6b7280; font-style: italic; margin: 4px 0 12px; }\n")
	b.WriteString("    tr.run-row.is-hidden, section.section.is-hidden { display: none; }\n")
	b.WriteString("    .runs-table { width: 100%; border-collapse: collapse; font-size: 12px; margin: 8px 0 12px; }\n")
	b.WriteString("    .runs-table th, .runs-table td { border: 1px solid #e5e7eb; padding: 8px 9px; text-align: left; vertical-align: top; }\n")
	b.WriteString("    .runs-table th { background: #f3f4f6; color: #374151; font-weight: 700; }\n")
	b.WriteString("    .runs-table td.result-col, .runs-table td.time-col, .runs-table td.pr-col, .runs-table td.failed-tests-col { white-space: nowrap; }\n")
	b.WriteString("    .status-badge { display: inline-flex; align-items: center; justify-content: center; border-radius: 999px; padding: 2px 8px; font-size: 11px; font-weight: 700; border: 1px solid transparent; }\n")
	b.WriteString("    .status-failed { background: #fee2e2; border-color: #fecaca; color: #991b1b; }\n")
	b.WriteString("    .status-passed { background: #dcfce7; border-color: #bbf7d0; color: #166534; }\n")
	b.WriteString("    .job-submeta, .phrase-submeta, .detail-meta { color: #6b7280; font-size: 11px; margin-top: 4px; }\n")
	b.WriteString("    .run-flags, .failure-flags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }\n")
	b.WriteString("    .mini-badge { display: inline-flex; align-items: center; justify-content: center; border-radius: 999px; padding: 2px 7px; font-size: 10px; font-weight: 700; background: #eff6ff; color: #1e40af; border: 1px solid #bfdbfe; }\n")
	b.WriteString("    .signal-icon { display: inline-flex; align-items: center; justify-content: center; margin-right: 4px; font-weight: 700; }\n")
	b.WriteString("    .signal-regression { color: #dc2626; }\n")
	b.WriteString("    .signal-flake { color: #b45309; }\n")
	b.WriteString("    .signal-new { color: #7c3aed; }\n")
	b.WriteString("    .detail-list { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; }\n")
	b.WriteString("    .lane-group { margin-top: 0; }\n")
	b.WriteString("    .detail-list > .lane-group { margin-top: 0; }\n")
	b.WriteString("    .lane-group > summary { text-transform: uppercase; letter-spacing: 0.02em; font-size: 11px; }\n")
	b.WriteString("    .lane-detail-list { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; }\n")
	b.WriteString("    .detail-item { border: 1px solid #e5e7eb; border-radius: 8px; background: #f9fafb; padding: 8px 10px; }\n")
	b.WriteString("    .detail-title { font-weight: 700; }\n")
	b.WriteString("    .job-link { font-weight: 700; }\n")
	b.WriteString("    .page-content details { margin-top: 8px; }\n")
	b.WriteString("    details summary { cursor: pointer; color: #1d4ed8; font-weight: 600; }\n")
	b.WriteString("    .raw-failure-toggle > summary { display: inline-flex; align-items: center; justify-content: center; border: 1px solid #d1d5db; border-radius: 999px; padding: 4px 10px; font-size: 11px; font-weight: 600; color: #1f2937; background: #ffffff; }\n")
	b.WriteString("    .raw-failure-toggle[open] > summary { background: #f3f4f6; }\n")
	b.WriteString("    pre { white-space: pre-wrap; word-break: break-word; background: #111827; color: #f9fafb; padding: 8px; border-radius: 6px; font-size: 11px; margin: 8px 0 0; }\n")
	b.WriteString(frontui.ReportChromeCSS())
	b.WriteString(frontui.ThemeCSS())
	b.WriteString("    :root[data-theme=\"dark\"] .meta, :root[data-theme=\"dark\"] .muted, :root[data-theme=\"dark\"] .label, :root[data-theme=\"dark\"] .section-note, :root[data-theme=\"dark\"] .job-submeta, :root[data-theme=\"dark\"] .phrase-submeta, :root[data-theme=\"dark\"] .detail-meta { color: #94a3b8; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .card, :root[data-theme=\"dark\"] .section, :root[data-theme=\"dark\"] .detail-item { background: #111827; border-color: #334155; color: #e2e8f0; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .runs-table th { background: #1f2937; color: #e2e8f0; border-color: #334155; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .runs-table td { border-color: #334155; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .action-primary { background: #2563eb; border-color: #2563eb; color: #e2e8f0; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .action-primary:hover { background: #1d4ed8; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .action-secondary { background: #1f2937; border-color: #334155; color: #e2e8f0; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .action-secondary:hover { background: #0f172a; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .run-search-input { background: #1f2937; border-color: #334155; color: #e2e8f0; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .run-search-input:focus { border-color: #2563eb; box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.25); }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .run-search-status, :root[data-theme=\"dark\"] .run-search-empty { color: #94a3b8; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] details summary { color: #93c5fd; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .mini-badge { background: #1e293b; border-color: #334155; color: #93c5fd; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .raw-failure-toggle > summary { background: #1f2937; border-color: #334155; color: #e2e8f0; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] .raw-failure-toggle[open] > summary { background: #0f172a; }\n")
	b.WriteString("    :root[data-theme=\"dark\"] pre { background: #020617; color: #e2e8f0; border: 1px solid #334155; }\n")
	b.WriteString("  </style>\n")
	b.WriteString("</head>\n")
	b.WriteString("<body>\n")
	b.WriteString(frontui.ReportChromeHTML(options.Chrome))
	b.WriteString("<main class=\"page-content\">\n")
	b.WriteString(runLogDayActionsHTML(options))
	b.WriteString(runLogDaySearchBoxHTML())

	for _, environment := range data.Environments {
		b.WriteString(fmt.Sprintf("  <section id=\"runs-%s\" class=\"section\">\n", html.EscapeString(strings.TrimSpace(environment.Environment))))
		b.WriteString(fmt.Sprintf("    <h2>Environment: %s</h2>\n", html.EscapeString(strings.ToUpper(strings.TrimSpace(environment.Environment)))))
		if len(environment.Runs) == 0 {
			b.WriteString("    <p class=\"muted\">No runs were recorded for this environment on the selected day.</p>\n")
			b.WriteString("  </section>\n")
			continue
		}
		b.WriteString("    <table class=\"runs-table\">\n")
		b.WriteString("      <thead><tr><th class=\"tz-header\">Time (UTC)</th><th>Job</th><th>Failed at</th><th>Result</th><th>PR</th><th>Failed tests</th><th>Details</th></tr></thead>\n")
		b.WriteString("      <tbody>\n")
		for _, row := range environment.Runs {
			b.WriteString(runLogDayRunRowHTML(row))
		}
		b.WriteString("      </tbody>\n")
		b.WriteString("    </table>\n")
		b.WriteString("  </section>\n")
	}

	b.WriteString("</main>\n")
	b.WriteString(frontui.ThemeToggleScriptTag())
	b.WriteString(frontui.TimezoneToggleScriptTag())
	b.WriteString(runLogDaySearchScriptTag())
	b.WriteString("</body>\n")
	b.WriteString("</html>\n")
	return b.String()
}

func runLogDayActionsHTML(options PageOptions) string {
	var b strings.Builder
	b.WriteString("  <div class=\"page-actions\">\n")
	if href := strings.TrimSpace(options.FailurePatternsHref); href != "" {
		b.WriteString(fmt.Sprintf(
			"    <a class=\"action-btn action-primary\" href=\"%s\">&#8599; Open Failure patterns for this day</a>\n",
			html.EscapeString(href),
		))
	}
	b.WriteString("  </div>\n")
	return b.String()
}

// runLogDaySearchBoxHTML renders the free-text filter box. Filtering is applied
// client-side against each run row's text (including collapsed raw failure
// logs), so operators can narrow the view to runs whose failures match a phrase
// such as "timeout during CreateHCPClusterAndWait" or
// "alert [svc] KubeNodeUnreachable fired".
func runLogDaySearchBoxHTML() string {
	var b strings.Builder
	b.WriteString("  <div class=\"run-search\">\n")
	b.WriteString("    <input type=\"search\" id=\"run-log-search\" class=\"run-search-input\" ")
	b.WriteString("placeholder=\"Filter runs by failure text (e.g. timeout during CreateHCPClusterAndWait)\" ")
	b.WriteString("autocomplete=\"off\" spellcheck=\"false\" aria-label=\"Filter runs by failure text\" />\n")
	b.WriteString("    <span class=\"run-search-status\" id=\"run-log-search-status\" role=\"status\" aria-live=\"polite\"></span>\n")
	b.WriteString("  </div>\n")
	b.WriteString("  <p class=\"run-search-empty\" id=\"run-log-search-empty\" hidden>No runs match your search.</p>\n")
	return b.String()
}

// runLogDaySearchScriptTag renders the client-side filtering behaviour for the
// run-log search box. It hides run rows whose text does not contain the query,
// auto-expands the categories/raw sections that hold the match so it is visible
// in context, hides environment sections that end up empty, and keeps the query
// in the URL (?q=) so a filtered view is shareable and survives reload.
func runLogDaySearchScriptTag() string {
	return strings.TrimSpace(`
<script>
(function () {
  var input = document.getElementById("run-log-search");
  if (!input) { return; }
  var status = document.getElementById("run-log-search-status");
  var emptyMsg = document.getElementById("run-log-search-empty");
  var rows = Array.prototype.slice.call(document.querySelectorAll("tr.run-row"));
  var sections = Array.prototype.slice.call(document.querySelectorAll("section.section"));

  function clearSearchOpens() {
    var opened = document.querySelectorAll("[data-search-open]");
    for (var i = 0; i < opened.length; i++) {
      opened[i].open = false;
      opened[i].removeAttribute("data-search-open");
    }
  }

  function expandMatches(row, q) {
    // Only auto-expand the lane-group category expanders so a match hidden in
    // a collapsed category becomes visible; never force-open the raw-failure
    // toggles, so the full failure log stays collapsed until the user asks.
    var dets = row.querySelectorAll("details.lane-group");
    for (var i = 0; i < dets.length; i++) {
      var d = dets[i];
      if (d.open) { continue; }
      if ((d.textContent || "").toLowerCase().indexOf(q) !== -1) {
        d.open = true;
        d.setAttribute("data-search-open", "");
      }
    }
  }

  function apply(rawQuery) {
    var q = (rawQuery || "").trim().toLowerCase();
    clearSearchOpens();
    var visible = 0;
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var match = q === "" || (row.textContent || "").toLowerCase().indexOf(q) !== -1;
      if (match) {
        row.classList.remove("is-hidden");
        visible++;
        if (q !== "") { expandMatches(row, q); }
      } else {
        row.classList.add("is-hidden");
      }
    }
    for (var s = 0; s < sections.length; s++) {
      var sec = sections[s];
      var secRows = sec.querySelectorAll("tr.run-row");
      if (secRows.length === 0) {
        sec.classList.toggle("is-hidden", q !== "");
        continue;
      }
      var anyVisible = false;
      for (var r = 0; r < secRows.length; r++) {
        if (!secRows[r].classList.contains("is-hidden")) { anyVisible = true; break; }
      }
      sec.classList.toggle("is-hidden", !anyVisible);
    }
    if (status) {
      status.textContent = q === "" ? "" : ("Showing " + visible + " of " + rows.length + " runs");
    }
    if (emptyMsg) {
      emptyMsg.hidden = !(q !== "" && visible === 0);
    }
    syncURL(rawQuery);
  }

  function syncURL(rawQuery) {
    if (!window.history || !window.history.replaceState) { return; }
    try {
      var url = new URL(window.location.href);
      var v = (rawQuery || "").trim();
      if (v === "") { url.searchParams.delete("q"); } else { url.searchParams.set("q", v); }
      window.history.replaceState(null, "", url.toString());
    } catch (err) {}
  }

  function initialQuery() {
    try {
      return new URL(window.location.href).searchParams.get("q") || "";
    } catch (err) { return ""; }
  }

  var initial = initialQuery();
  if (initial) { input.value = initial; }
  var timer = null;
  input.addEventListener("input", function () {
    if (timer) { clearTimeout(timer); }
    timer = setTimeout(function () { apply(input.value); }, 120);
  });
  apply(input.value);
})();
</script>
`) + "\n"
}

func runLogDayCardHTML(label string, value string) string {
	return fmt.Sprintf(
		"    <div class=\"card\"><div class=\"label\">%s</div><div class=\"value\">%s</div></div>\n",
		html.EscapeString(strings.TrimSpace(label)),
		html.EscapeString(strings.TrimSpace(value)),
	)
}

func runLogDayRunRowHTML(row readmodelrunlog.JobHistoryRunRow) string {
	var b strings.Builder
	b.WriteString("        <tr class=\"run-row\">\n")
	b.WriteString(fmt.Sprintf("          <td class=\"time-col\">%s</td>\n", runLogDayRunTimeHTML(row.Run.OccurredAt)))
	b.WriteString("          <td>")
	b.WriteString(runLogDayJobHTML(row.Run))
	if flagsHTML := runLogDayRunFlagsHTML(row.Run); flagsHTML != "" {
		b.WriteString(flagsHTML)
	}
	b.WriteString(fmt.Sprintf("<div class=\"job-submeta\">%s</div>", html.EscapeString(runLogDayRunSubmeta(row.Run))))
	b.WriteString("</td>\n")
	b.WriteString(fmt.Sprintf("          <td>%s</td>\n", html.EscapeString(runLogDayLaneSummary(row))))
	b.WriteString(fmt.Sprintf("          <td class=\"result-col\">%s</td>\n", runLogDayResultBadgeHTML(row.Run)))
	b.WriteString(fmt.Sprintf("          <td class=\"pr-col\">%s</td>\n", runLogDayPRHTML(row)))
	b.WriteString(fmt.Sprintf("          <td class=\"failed-tests-col\">%s</td>\n", html.EscapeString(runLogDayFailedTestsLabel(row))))
	b.WriteString("          <td>")
	detailsHTML := runLogDayFailureDetailsHTML(row)
	if detailsHTML == "" {
		// Only show the single-line summary when there are no per-category
		// expanders; otherwise it just duplicates the category breakdown
		// (e.g. "Multiple failures (10)").
		b.WriteString(html.EscapeString(runLogDayPrimaryPhrase(row)))
		if submeta := runLogDayPrimaryPhraseSubmeta(row); submeta != "" {
			b.WriteString(fmt.Sprintf("<div class=\"phrase-submeta\">%s</div>", html.EscapeString(submeta)))
		}
	}
	if detailsHTML != "" {
		b.WriteString(detailsHTML)
	}
	b.WriteString("</td>\n")
	b.WriteString("        </tr>\n")
	return b.String()
}

func runLogDayRunTime(occurredAt string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(occurredAt))
	if err != nil {
		return strings.TrimSpace(occurredAt)
	}
	return parsed.UTC().Format("15:04:05 UTC")
}

func runLogDayRunTimeHTML(occurredAt string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(occurredAt))
	if err != nil {
		return html.EscapeString(strings.TrimSpace(occurredAt))
	}
	return frontui.TimestampHTML(parsed, "15:04:05", frontui.TzFmtTime)
}

func runLogDayJobLabel(run storecontracts.RunRecord) string {
	label := strings.TrimSpace(run.JobName)
	if label != "" {
		return label
	}
	return "unknown job"
}

func runLogDayJobHTML(run storecontracts.RunRecord) string {
	label := runLogDayJobLabel(run)
	if href := strings.TrimSpace(run.RunURL); href != "" {
		return fmt.Sprintf("<a class=\"job-link\" href=\"%s\">%s</a>", html.EscapeString(href), html.EscapeString(label))
	}
	return fmt.Sprintf("<span class=\"job-link\">%s</span>", html.EscapeString(label))
}

func runLogDayRunFlagsHTML(run storecontracts.RunRecord) string {
	// Tide batch runs are marked with a single "batch" badge instead of the
	// post-good / merged-PR badges. Batch runs test PRs that each already passed
	// e2e, so they are counted as post-good in metrics, but surfaced distinctly
	// here so they are easy to identify.
	if prowartifacts.IsBatchRunURL(run.RunURL) {
		return "<div class=\"run-flags\"><span class=\"mini-badge\">batch</span></div>"
	}
	flags := make([]string, 0, 2)
	if run.PostGoodCommit {
		flags = append(flags, "post-good")
	}
	if run.MergedPR {
		flags = append(flags, "merged PR")
	}
	if len(flags) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<div class=\"run-flags\">")
	for _, flag := range flags {
		b.WriteString(fmt.Sprintf("<span class=\"mini-badge\">%s</span>", html.EscapeString(flag)))
	}
	b.WriteString("</div>")
	return b.String()
}

func runLogDayRunSubmeta(run storecontracts.RunRecord) string {
	parts := make([]string, 0, 2)
	if short := runLogDayShortSHA(run.PRSHA); short != "" {
		parts = append(parts, "head "+short)
	}
	if short := runLogDayShortSHA(run.FinalMergedSHA); short != "" {
		parts = append(parts, "merge "+short)
	}
	if len(parts) == 0 {
		return "No additional run metadata captured"
	}
	return strings.Join(parts, " · ")
}

func runLogDayResultBadgeHTML(run storecontracts.RunRecord) string {
	label := "passed"
	className := "status-badge status-passed"
	if run.Failed {
		label = "failed"
		className = "status-badge status-failed"
	}
	return fmt.Sprintf("<span class=\"%s\">%s</span>", className, html.EscapeString(label))
}

func runLogDayPRHTML(row readmodelrunlog.JobHistoryRunRow) string {
	run := row.Run
	if run.PRNumber <= 0 {
		return "<span class=\"muted\">n/a</span>"
	}
	label := fmt.Sprintf("#%d", run.PRNumber)
	if state := runLogDayPRStateLabel(run); state != "" {
		label += " (" + state + ")"
	}
	content := html.EscapeString(label)
	if href := runLogDayGitHubPRURL(run.PRNumber); href != "" {
		content = fmt.Sprintf("<a href=\"%s\">%s</a>", html.EscapeString(href), content)
	}
	if icons := runLogDaySignalIconsHTML(row); icons != "" {
		return icons + content
	}
	return content
}

func runLogDayPRStateLabel(run storecontracts.RunRecord) string {
	if run.PRNumber <= 0 {
		return ""
	}
	if run.MergedPR {
		return "merged"
	}
	state := strings.ToLower(strings.TrimSpace(run.PRState))
	switch state {
	case "open", "closed", "merged":
		return state
	default:
		return ""
	}
}

func runLogDaySignalIconsHTML(row readmodelrunlog.JobHistoryRunRow) string {
	if !row.Run.Failed || row.SemanticRollups.ClusteredRows == 0 {
		return ""
	}
	hasRegression, regressionReasons := runLogDayBestRegression(row)
	hasNew := runLogDayHasNewPattern(row)

	var icons strings.Builder
	if hasRegression {
		tooltip := "Likely regression — " + strings.Join(regressionReasons, "; ")
		icons.WriteString(fmt.Sprintf(
			"<span class=\"signal-icon signal-regression\" title=\"%s\" aria-label=\"%s\">⚠</span>",
			html.EscapeString(tooltip),
			html.EscapeString(tooltip),
		))
	}
	if hasNew && !hasRegression {
		icons.WriteString("<span class=\"signal-icon signal-new\" title=\"New failure pattern — no prior history\" aria-label=\"New failure pattern\">★</span>")
	}
	return icons.String()
}

func runLogDayBestRegression(row readmodelrunlog.JobHistoryRunRow) (bool, []string) {
	if row.BadPRScore <= 0 {
		return false, nil
	}
	return true, append([]string(nil), row.BadPRReasons...)
}

func runLogDayHasNewPattern(row readmodelrunlog.JobHistoryRunRow) bool {
	for _, f := range row.FailureRows {
		if strings.TrimSpace(f.SemanticAttachment.Status) == "clustered" && f.PriorWeeksPresent == 0 {
			return true
		}
	}
	return false
}

func runLogDayPrimaryPhrase(row readmodelrunlog.JobHistoryRunRow) string {
	if len(row.FailureRows) == 0 {
		if row.Run.Failed {
			return "Failure details unavailable"
		}
		return "n/a"
	}

	phrases := runLogDaySemanticPhrases(row)
	switch strings.TrimSpace(row.SemanticRollups.AttachmentSummary) {
	case "single_clustered":
		if len(phrases) > 0 {
			return phrases[0]
		}
	case "multiple_clustered", "mixed":
		return fmt.Sprintf("Multiple failures (%d)", row.FailedTestCount)
	case "unmatched_only":
		if len(row.FailureRows) == 1 {
			if text := strings.TrimSpace(row.FailureRows[0].FailureText); text != "" {
				return runLogDayPreviewText(text, 140)
			}
		}
		return fmt.Sprintf("Multiple failures (%d)", row.FailedTestCount)
	}
	if len(phrases) > 0 {
		return phrases[0]
	}
	return fmt.Sprintf("%d failure rows", len(row.FailureRows))
}

func runLogDayPrimaryPhraseSubmeta(row readmodelrunlog.JobHistoryRunRow) string {
	if len(row.FailureRows) == 0 {
		if row.Run.Failed {
			return "Failure details are not available for this run yet."
		}
		return ""
	}
	return ""
}

func runLogDayFailureDetailsHTML(row readmodelrunlog.JobHistoryRunRow) string {
	if len(row.FailureRows) == 0 {
		return ""
	}
	if runLogDayAllFailuresNonArtifactBacked(row.FailureRows) {
		return ""
	}

	groups, order := runLogDayGroupFailuresByLane(row.FailureRows)

	var b strings.Builder
	b.WriteString("<div class=\"detail-list\">")
	for _, lane := range order {
		failures := groups[lane]
		// Expand a category by default only when it holds a single failure so
		// the pattern is visible at a glance; keep multi-failure categories
		// collapsed so the operator can choose what to expand.
		openAttr := ""
		if len(failures) == 1 {
			openAttr = " open"
		}
		b.WriteString(fmt.Sprintf("<details class=\"lane-group\"%s>", openAttr))
		b.WriteString(fmt.Sprintf("<summary>%s (%d)</summary>", html.EscapeString(runLogDayLaneLabel(lane)), len(failures)))
		b.WriteString("<div class=\"lane-detail-list\">")
		for _, failure := range failures {
			b.WriteString("<div class=\"detail-item\">")
			b.WriteString(fmt.Sprintf("<div class=\"detail-title\">%s</div>", html.EscapeString(runLogDayFailureTitle(failure))))
			b.WriteString(fmt.Sprintf("<div class=\"detail-meta\">%s</div>", html.EscapeString(runLogDayFailureMeta(failure))))
			if flags := runLogDayFailureFlagsHTML(failure); flags != "" {
				b.WriteString(flags)
			}
			b.WriteString(runLogDayRawFailureToggleHTML(failure))
			b.WriteString("</div>")
		}
		b.WriteString("</div>")
		b.WriteString("</details>")
	}
	b.WriteString("</div>")
	return b.String()
}

// runLogDayGroupFailuresByLane groups failure rows by their failure type (lane)
// and returns a deterministic lane ordering: provision, e2e, alert, then any
// other lanes alphabetically.
func runLogDayGroupFailuresByLane(rows []readmodelrunlog.JobHistoryFailureRow) (map[string][]readmodelrunlog.JobHistoryFailureRow, []string) {
	groups := map[string][]readmodelrunlog.JobHistoryFailureRow{}
	for _, row := range rows {
		lane := strings.TrimSpace(row.Lane)
		if lane == "" {
			lane = "unknown"
		}
		groups[lane] = append(groups[lane], row)
	}

	rank := map[string]int{"provision": 0, "e2e": 1, "alert": 2}
	order := make([]string, 0, len(groups))
	for lane := range groups {
		order = append(order, lane)
	}
	sort.Slice(order, func(i, j int) bool {
		ri, iok := rank[order[i]]
		rj, jok := rank[order[j]]
		if iok && jok {
			return ri < rj
		}
		if iok != jok {
			return iok
		}
		return order[i] < order[j]
	})
	return groups, order
}

func runLogDayLaneLabel(lane string) string {
	trimmed := strings.TrimSpace(lane)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func runLogDayAllFailuresNonArtifactBacked(rows []readmodelrunlog.JobHistoryFailureRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !row.NonArtifactBacked {
			return false
		}
	}
	return true
}

func runLogDayFailureTitle(row readmodelrunlog.JobHistoryFailureRow) string {
	if phrase := strings.TrimSpace(row.SemanticAttachment.CanonicalEvidencePhrase); phrase != "" {
		return phrase
	}
	if text := strings.TrimSpace(row.FailureText); text != "" {
		return runLogDayPreviewText(text, 140)
	}
	return "Failure detail"
}

func runLogDayFailureMeta(row readmodelrunlog.JobHistoryFailureRow) string {
	parts := make([]string, 0, 4)
	if occurredAt := runLogDayRunTime(row.OccurredAt); occurredAt != "" {
		parts = append(parts, occurredAt)
	}
	if lane := strings.TrimSpace(row.Lane); lane != "" {
		parts = append(parts, "failed at "+lane)
	}
	if testName := strings.TrimSpace(row.TestName); testName != "" {
		parts = append(parts, "test "+testName)
	}
	if testSuite := strings.TrimSpace(row.TestSuite); testSuite != "" {
		parts = append(parts, "suite "+testSuite)
	}
	return strings.Join(parts, " · ")
}

func runLogDayFailureFlagsHTML(row readmodelrunlog.JobHistoryFailureRow) string {
	flags := make([]string, 0, 1)
	if row.NonArtifactBacked {
		flags = append(flags, "synthetic raw row")
	}
	if len(flags) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<div class=\"failure-flags\">")
	for _, flag := range flags {
		b.WriteString(fmt.Sprintf("<span class=\"mini-badge\">%s</span>", html.EscapeString(flag)))
	}
	b.WriteString("</div>")
	return b.String()
}

func runLogDayRawFailureToggleHTML(row readmodelrunlog.JobHistoryFailureRow) string {
	text := strings.TrimSpace(row.FailureText)
	if text == "" {
		return ""
	}
	return fmt.Sprintf(
		"<details class=\"raw-failure-toggle\"><summary>Show raw failure</summary><pre>%s</pre></details>",
		html.EscapeString(text),
	)
}

func runLogDaySemanticPhrases(row readmodelrunlog.JobHistoryRunRow) []string {
	set := map[string]struct{}{}
	for _, failure := range row.FailureRows {
		phrase := strings.TrimSpace(failure.SemanticAttachment.CanonicalEvidencePhrase)
		if phrase == "" {
			continue
		}
		set[phrase] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for phrase := range set {
		out = append(out, phrase)
	}
	sort.Strings(out)
	return out
}

func runLogDayEnvironmentList(environments []string) string {
	normalized := normalizedQueryEnvironments(environments)
	if len(normalized) == 0 {
		return "none"
	}
	for i := range normalized {
		normalized[i] = strings.ToUpper(normalized[i])
	}
	return strings.Join(normalized, ", ")
}

func normalizedQueryEnvironments(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func runLogDayLaneSummary(row readmodelrunlog.JobHistoryRunRow) string {
	if len(row.Lanes) == 0 {
		return "n/a"
	}
	return strings.Join(row.Lanes, ", ")
}

func runLogDayFailedTestsLabel(row readmodelrunlog.JobHistoryRunRow) string {
	if len(row.FailureRows) == 0 && row.Run.Failed {
		return "n/a"
	}
	return fmt.Sprintf("%d", row.FailedTestCount)
}

func runLogDayGitHubPRURL(prNumber int) string {
	if prNumber <= 0 {
		return ""
	}
	owner := strings.TrimSpace(sourceoptions.DefaultGitHubRepoOwner())
	repo := strings.TrimSpace(sourceoptions.DefaultGitHubRepoName())
	if owner == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNumber)
}

func runLogDayShortSHA(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 7 {
		return trimmed
	}
	return trimmed[:7]
}

func runLogDayPreviewText(value string, max int) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " "), "\t", " "))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if max <= 0 || len([]rune(normalized)) <= max {
		return normalized
	}
	runes := []rune(normalized)
	return string(runes[:max-1]) + "..."
}
