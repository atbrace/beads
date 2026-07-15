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
	reverseBlockerCount := `(
				SELECT COUNT(*) FROM dependencies
				WHERE type = 'blocks'
				  AND COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = i.id
			)`
	if includeWispReverseDeps {
		reverseBlockerCount = `(` + reverseBlockerCount + ` + (
				SELECT COUNT(*) FROM wisp_dependencies
				WHERE type = 'blocks'
				  AND COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = i.id
			))`
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
				SELECT COUNT(*) FROM %s
				WHERE type = 'blocks' AND issue_id = i.id
			), 0) AS dep_count,
			COALESCE(%s, 0) AS rdep_count,
			COALESCE((
				SELECT COUNT(*) FROM %s WHERE issue_id = i.id
			), 0) AS comment_count,
			(
				SELECT MIN(COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external))
				FROM %s
				WHERE type = 'parent-child' AND issue_id = i.id
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
