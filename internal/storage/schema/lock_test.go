package schema

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrationLockNameUsesRawNameWhenBounded(t *testing.T) {
	got := MigrationLockName("testdb_short")
	want := migrationLockPrefix + "testdb_short"
	if got != want {
		t.Fatalf("MigrationLockName() = %q, want %q", got, want)
	}
}

func TestMigrationLockNameHashesLongNames(t *testing.T) {
	dbName := strings.Repeat("a", 64)
	got := MigrationLockName(dbName)
	if len(got) > migrationLockNameMaxLength {
		t.Fatalf("MigrationLockName() length = %d, want <= %d", len(got), migrationLockNameMaxLength)
	}
	if got == migrationLockPrefix+dbName {
		t.Fatalf("MigrationLockName() used over-limit raw name %q", got)
	}
	if got != MigrationLockName(dbName) {
		t.Fatal("MigrationLockName() is not deterministic")
	}
}

func TestIsMigrationLockError(t *testing.T) {
	err := errors.Join(ErrMigrationLockUnavailable, errors.New("timeout"))
	if !IsMigrationLockError(err) {
		t.Fatal("IsMigrationLockError() = false, want true")
	}
}

func TestMigrateUpRunsWithoutAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer db.Close()

	expectOnePendingMigration(t, mock)

	applied, err := MigrateUp(context.Background(), db)
	if err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("MigrateUp() applied = %d, want 1", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestMigrateUpWithLockUsesDatabaseScopedLockOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin mock connection: %v", err)
	}
	defer conn.Close()

	// Pre-lock fast-path probe: seed present, but the main cursor is behind,
	// so migration work is needed and the locked pass runs.
	expectSeedRowPresent(mock, true)
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations", "version", LatestVersion()-1)

	lockName := MigrationLockName("testdb")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).
		WithArgs(lockName, migrationLockAcquireTimeoutSeconds).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	expectOnePendingMigration(t, mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(lockName).
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	applied, err := MigrateUpWithLock(ctx, conn, "testdb")
	if err != nil {
		t.Fatalf("MigrateUpWithLock() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("MigrateUpWithLock() applied = %d, want 1", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}


// TestMigrateUpWithLockSkipsLockWhenCurrent is the fleet-load guard: a database
// that is already fully migrated and seeded must be probed read-only and never
// touch GET_LOCK — otherwise every command in every concurrent bd process
// serializes on the per-database advisory lock just to discover there is no
// work. sqlmock's ordered expectations fail the test on any GET_LOCK,
// RELEASE_LOCK, dolt_ignore REPLACE, or DOLT_COMMIT this path would issue.
func TestMigrateUpWithLockSkipsLockWhenCurrent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin mock connection: %v", err)
	}
	defer conn.Close()

	expectSeedRowPresent(mock, true)
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations", "version", LatestVersion())
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM ignored_schema_migrations", "version", LatestIgnoredVersion())
	expectContentHashColumnExists(mock)
	expectContentHashColumnExists(mock)
	expectScalar(mock, "SELECT COUNT(*) FROM custom_types", "count", 1)
	expectScalar(mock, "SELECT COUNT(*) FROM custom_statuses", "count", 1)

	applied, err := MigrateUpWithLock(ctx, conn, "testdb")
	if err != nil {
		t.Fatalf("MigrateUpWithLock() error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("MigrateUpWithLock() applied = %d, want 0", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations (an already-current database must skip GET_LOCK and the migration pass): %v", err)
	}
}

// TestMigrateUpWithLockFallsThroughWhenSeedMissing: a database without the
// ignored_schema_migrations dolt_ignore row must NOT take the fast path — the
// missing row sends it through the locked pass, which asserts it exactly as
// before.
func TestMigrateUpWithLockFallsThroughWhenSeedMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin mock connection: %v", err)
	}
	defer conn.Close()

	// Fast-path probe: row absent -> not current, no further probe queries.
	expectSeedRowMissing(mock)

	lockName := MigrationLockName("testdb")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).
		WithArgs(lockName, migrationLockAcquireTimeoutSeconds).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	expectOnePendingMigration(t, mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(lockName).
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	applied, err := MigrateUpWithLock(ctx, conn, "testdb")
	if err != nil {
		t.Fatalf("MigrateUpWithLock() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("MigrateUpWithLock() applied = %d, want 1", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations (missing seed row must fall through to the locked pass): %v", err)
	}
}

// TestMigrateUpWithLockFallsThroughOnProbeError: a fast-path probe failure must
// not fail the open — it falls through to the locked pass, which re-checks and
// surfaces any genuine error itself.
func TestMigrateUpWithLockFallsThroughOnProbeError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin mock connection: %v", err)
	}
	defer conn.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ignored FROM dolt_ignore WHERE pattern = 'ignored_schema_migrations'")).
		WillReturnError(errors.New("transient catalog race"))

	lockName := MigrationLockName("testdb")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).
		WithArgs(lockName, migrationLockAcquireTimeoutSeconds).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
	expectOnePendingMigration(t, mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs(lockName).
		WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))

	applied, err := MigrateUpWithLock(ctx, conn, "testdb")
	if err != nil {
		t.Fatalf("MigrateUpWithLock() error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("MigrateUpWithLock() applied = %d, want 1", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations (probe error must fall through to the locked pass): %v", err)
	}
}

// expectSeedRowPresent mocks the fast-path read-only seed probe
// (doltIgnoreSeedCurrent) returning the ignored_schema_migrations row.
func expectSeedRowPresent(mock sqlmock.Sqlmock, ignored bool) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ignored FROM dolt_ignore WHERE pattern = 'ignored_schema_migrations'")).
		WillReturnRows(sqlmock.NewRows([]string{"ignored"}).AddRow(ignored))
}

// expectSeedRowMissing mocks the probe finding no seed row at all.
func expectSeedRowMissing(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ignored FROM dolt_ignore WHERE pattern = 'ignored_schema_migrations'")).
		WillReturnRows(sqlmock.NewRows([]string{"ignored"}))
}
func expectOnePendingMigration(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()

	latest := LatestVersion()
	latestIgnored := LatestIgnoredVersion()

	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations", "version", latest-1)
	expectDoltStatusRows(mock)
	expectDoltStatusRows(mock)
	// MigrateUp probes the aux-rekey crash sentinel (bd-578h9.16); this
	// mocked world has no local_metadata table, so no crashed pass.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.TABLES`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// MigrateUp captures the pre-pass main cursor for the aux re-key
	// watershed (bd-578h9.4) before the main migrations run.
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations", "version", latest-1)
	mock.ExpectExec("(?s)^CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectContentHashColumnExists(mock)
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations", "version", latest-1)
	if latest == 53 {
		// The v53 pre-repair probes the six rig/agent columns on issues and
		// then the local wisp_dependencies table; this mocked world has all
		// issue columns and no local wisp_dependencies table, so no ALTERs follow.
		for _, col := range []string{"hook_bead", "role_bead", "agent_state", "last_activity", "role_type", "rig"} {
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.COLUMNS`).
				WithArgs("issues", col).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		}
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.TABLES`).
			WithArgs("wisp_dependencies").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	mock.ExpectExec("(?s).*").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO schema_migrations (version, content_hash) VALUES (?, ?)")).
		WithArgs(latest, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectScalar(mock, "SELECT COUNT(*) FROM custom_types", "count", 1)
	expectScalar(mock, "SELECT COUNT(*) FROM custom_statuses", "count", 1)
	// rekeyDependencyIDs probes whether each edge table has an id column; this
	// mocked world has no such table, so both probes return 0 and the re-key
	// no-ops without scanning/updating rows.
	expectColumnExists(mock, false)
	expectColumnExists(mock, false)
	// rekeyAuxRowIDs reads the ignored cursor to see whether its clone-local
	// marker is pending; at latest it is not, so the re-key no-ops.
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM ignored_schema_migrations", "version", latestIgnored)
	mock.ExpectExec(regexp.QuoteMeta("REPLACE INTO dolt_ignore VALUES ('ignored_schema_migrations', true)")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("(?s)^CREATE TABLE IF NOT EXISTS ignored_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectContentHashColumnExists(mock)
	expectScalar(mock, "SELECT COALESCE(MAX(version), 0) FROM ignored_schema_migrations", "version", latestIgnored)
	expectDoltStatusRows(mock)
	expectDoltStatusRows(mock)
	mock.ExpectQuery("(?s)SELECT t\\.TABLE_NAME\\s+FROM INFORMATION_SCHEMA\\.TABLES t").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("schema_migrations"))
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_ADD('-f', ?)")).
		WithArgs("schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("CALL DOLT_COMMIT('-m', 'schema: apply migrations')")).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectColumnExists(mock sqlmock.Sqlmock, present bool) {
	n := 0
	if present {
		n = 1
	}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA\.COLUMNS`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(n))
}

// expectContentHashColumnExists mocks the idempotent ensureContentHashColumn
// probe, reporting that the content_hash column already exists (so no ALTER runs).
func expectContentHashColumnExists(mock sqlmock.Sqlmock) {
	expectColumnExists(mock, true)
}

func expectScalar(mock sqlmock.Sqlmock, query, column string, value any) {
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(sqlmock.NewRows([]string{column}).AddRow(value))
}

func expectDoltStatusRows(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("(?s)SELECT s\\.table_name, s\\.staged\\s+FROM dolt_status s").
		WillReturnRows(sqlmock.NewRows([]string{"table_name", "staged"}))
}
