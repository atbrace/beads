package sqlbuild

import "fmt"

// ReadyWorkIssueColumns is IssueSelectColumns qualified with the "i." alias
// used by the counts mega-query.
var ReadyWorkIssueColumns = QualifyColumns(IssueSelectColumns, "i.")

// DepJSONObject renders one dependency row as JSON for JSON_ARRAYAGG
// aggregation in the counts mega-query. Field names must match the JSON tags
// of types.Dependency.
const DepJSONObject = `JSON_OBJECT(
	'issue_id', issue_id,
	'depends_on_id', COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external),
	'type', type,
	'created_at', DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'),
	'created_by', created_by,
	'metadata', CAST(metadata AS CHAR),
	'thread_id', thread_id
)`

// reverseBlockerCountSQL renders the "how many open blockers point AT this
// issue" count for one dependency table as three correlated subqueries, one
// per target column.
//
// The natural spelling — WHERE COALESCE(depends_on_issue_id,
// depends_on_wisp_id, depends_on_external) = i.id — is unindexable: no index
// covers a COALESCE expression, so it scans the table once per outer row.
// Splitting it gives each subquery a bare-column correlation the planner can
// serve from an index.
//
// The IS NULL guards keep the split exactly equivalent to the COALESCE: a row
// counts against the wisp target only when the issue target is absent, and
// against the external target only when both of the others are, so a row with
// several non-null targets is still counted once, under its first non-null
// column. (No such row exists in practice, but the schema does not forbid one
// and a count must not depend on that.)
func reverseBlockerCountSQL(table string) string {
	const blockers = `SUM(CASE WHEN type = 'blocks' THEN 1 ELSE 0 END)`
	return fmt.Sprintf(`(
				COALESCE((SELECT %[1]s FROM %[2]s
					WHERE depends_on_issue_id = i.id), 0)
				+ COALESCE((SELECT %[1]s FROM %[2]s
					WHERE depends_on_wisp_id = i.id
					  AND depends_on_issue_id IS NULL), 0)
				+ COALESCE((SELECT %[1]s FROM %[2]s
					WHERE depends_on_external = i.id
					  AND depends_on_issue_id IS NULL
					  AND depends_on_wisp_id IS NULL), 0)
			)`, blockers, table)
}

// SearchCountsSQL renders the counts mega-query: full issue rows aliased "i"
// plus labels JSON, dep/rdep/comment counts, parent ID, and dependency JSON,
// for one table family. whereSQL/orderBySQL/limitSQL may be empty; the
// reverse-blocker count unions wisp_dependencies only when the caller has
// probed that the table exists.
//
// The scan side is issueops.ScanReadyWorkRowWithCounts, which scans
// IssueSelectColumns positionally followed by the six extra columns in the
// order projected here.
func SearchCountsSQL(tables FilterTables, whereSQL, orderBySQL, limitSQL string, includeWispReverseDeps, skipLabels bool) string {
	// Every per-issue aggregate is a correlated scalar subquery, NOT a
	// LEFT JOIN onto an unfiltered GROUP BY derived table. Dolt's engine
	// (go-mysql-server) materializes each derived table in full before the
	// outer WHERE applies, so the join form pays whole-table aggregations
	// over labels/dependencies/comments on EVERY list call regardless of
	// filter selectivity. Correlated subqueries evaluate only for rows that
	// survive whereSQL, as indexed point lookups (labels/deps/comments all
	// index issue_id). Result semantics are identical: an empty correlated
	// aggregate yields NULL exactly like an unmatched LEFT JOIN row.
	//
	// Each subquery's WHERE holds the correlation and NOTHING ELSE. gms drops
	// the issue_id index as soon as a second equality (`type = '...'`) shares
	// that WHERE, and falls back to scanning every row of that dependency type
	// once per outer row — the whole point of correlating was to avoid exactly
	// that. Type filters therefore live inside the aggregate's CASE, where they
	// cost one comparison per already-fetched row. On a 2,140-issue /
	// 2,506-dependency store this took `bd list --status=open` from 1,640ms to
	// 75ms of server time; the dominant subqueries individually went 286ms->12ms
	// (dep_count) and 477ms->11ms (parent_id).
	reverseBlockerCount := reverseBlockerCountSQL("dependencies")
	if includeWispReverseDeps {
		reverseBlockerCount = `(` + reverseBlockerCount + ` + ` + reverseBlockerCountSQL("wisp_dependencies") + `)`
	}

	labelsSelect := fmt.Sprintf(`(
				SELECT JSON_ARRAYAGG(label) FROM %s WHERE issue_id = i.id
			) AS labels_json`, tables.Labels)
	if skipLabels {
		labelsSelect = "NULL AS labels_json"
	}

	return fmt.Sprintf(`
		SELECT %s,
			%s,
			COALESCE((
				SELECT SUM(CASE WHEN type = 'blocks' THEN 1 ELSE 0 END) FROM %s
				WHERE issue_id = i.id
			), 0) AS dep_count,
			COALESCE(%s, 0) AS rdep_count,
			COALESCE((
				SELECT COUNT(*) FROM %s WHERE issue_id = i.id
			), 0) AS comment_count,
			(
				SELECT MIN(CASE WHEN type = 'parent-child' THEN
					COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)
				END)
				FROM %s
				WHERE issue_id = i.id
			) AS parent_id,
			(
				SELECT JSON_ARRAYAGG(%s) FROM %s WHERE issue_id = i.id
			) AS deps_json
		FROM %s i
		%s
		%s
		%s
	`,
		ReadyWorkIssueColumns,
		labelsSelect,
		tables.Dependencies,
		reverseBlockerCount,
		tables.Comments,
		tables.Dependencies,
		DepJSONObject,
		tables.Dependencies,
		tables.Main,
		whereSQL,
		orderBySQL,
		limitSQL,
	)
}
