package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A logical restore writes rows that the source database already holds in
// their final, consistent form, including every table its triggers maintain
// (counters, search names, revisions, the viewer-policy epoch). Re-running
// those triggers row by row on PostgreSQL is both wrong and ruinous: a
// counter table restored before the table that feeds it is counted twice,
// and a trigger that updates one row per restored row (the policy epoch, a
// series' added date) leaves a chain of dead versions of that row inside the
// one restore transaction, so every further update walks a longer chain.
// A large library never finishes.
//
// ImportLogical therefore suspends the target's own triggers for the
// restore (ALTER TABLE ... DISABLE TRIGGER is transactional, needs only
// table ownership, which migrations give the Ether role, and leaves
// foreign-key checks on) and restores every table verbatim. Two things then
// stand in for what the triggers would have done:
//   - Components register RestoreHooks for state that must move because a
//     restore happened (the policy epoch advances once, never per row).
//   - A trigger that writes a table the dump does not carry (a dump from a
//     release before that table existed) is replayed over the restored
//     rows, so derived tables the dump lacks are built as row-by-row writes
//     would have built them.

// RestoreHook runs inside a logical restore's transaction. It is called
// once the target is verified empty, before any row is written; the
// function it returns runs after every row is restored, before commit.
type RestoreHook func(ctx context.Context, tx *sql.Tx) (after func(context.Context, *sql.Tx) error, err error)

var (
	restoreHooks     = map[string]RestoreHook{}
	restoreMaintains = map[string]bool{}
)

// RegisterRestore adds component's restore hook. Call from init(). The
// hook alone maintains the named tables across a restore: their triggers'
// writes are never replayed, even for a dump that lacks the tables.
func RegisterRestore(component string, hook RestoreHook, maintains ...string) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := restoreHooks[component]; ok {
		panic("store: RegisterRestore " + component + " already registered")
	}
	restoreHooks[component] = hook
	for _, table := range maintains {
		restoreMaintains[table] = true
	}
}

// beginRestoreHooks runs every hook's first half and returns the second.
func beginRestoreHooks(ctx context.Context, tx *sql.Tx) (func(context.Context, *sql.Tx) error, error) {
	regMu.Lock()
	names := make([]string, 0, len(restoreHooks))
	for name := range restoreHooks {
		names = append(names, name)
	}
	hooks := make([]RestoreHook, len(names))
	sort.Strings(names)
	for i, name := range names {
		hooks[i] = restoreHooks[name]
	}
	regMu.Unlock()
	afters := make([]func(context.Context, *sql.Tx) error, 0, len(hooks))
	for i, hook := range hooks {
		after, err := hook(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("restore hook %s: %w", names[i], err)
		}
		if after != nil {
			afters = append(afters, after)
		}
	}
	return func(ctx context.Context, tx *sql.Tx) error {
		for _, after := range afters {
			if err := after(ctx, tx); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

type suspendedTrigger struct {
	table, name string
	def         string // CREATE TRIGGER statement
	qualified   string // PostgreSQL: the table as def names it
	enabled     string // PostgreSQL pg_trigger.tgenabled: O, A or R
	onInsert    bool
	writes      map[string]bool
}

// PostgreSQL pg_trigger.tgtype bit for INSERT.
const pgTriggerInsert = 4

var (
	rePLWrite        = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?(?:"?[A-Za-z_][A-Za-z0-9_]*"?\.)?"?([A-Za-z_][A-Za-z0-9_]*)`)
	rePLCall         = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reSQLiteOnInsert = regexp.MustCompile(`(?is)^\s*CREATE\s+TRIGGER\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"[^"]*"|\S+)\s+(?:BEFORE\s+|AFTER\s+|INSTEAD\s+OF\s+)?INSERT\s+ON\b`)
	reSQLiteCreate   = regexp.MustCompile(`(?i)^\s*CREATE\s+TRIGGER\b`)
)

// suspendTriggers removes every enabled trigger from the main schema for
// the rest of tx and reports each, with the tables its body (and, on
// PostgreSQL, the schema functions it calls) writes. PostgreSQL disables
// them; SQLite, which cannot, drops them for recreation (its DDL is
// transactional too, so a failed restore rolls back to the original set).
func suspendTriggers(ctx context.Context, tx *sql.Tx, d Dialect) ([]suspendedTrigger, error) {
	if d != Postgres {
		return suspendSQLiteTriggers(ctx, tx)
	}
	bodies := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT proname, prosrc FROM pg_proc WHERE pronamespace=current_schema()::regnamespace`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, src string
		if err := rows.Scan(&name, &src); err != nil {
			rows.Close()
			return nil, err
		}
		bodies[name] += src + "\n"
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT c.relname, t.tgname, quote_ident(n.nspname)||'.'||quote_ident(c.relname),
			pg_get_triggerdef(t.oid), t.tgenabled::text, (t.tgtype & `+fmt.Sprint(pgTriggerInsert)+`) <> 0, p.proname
		FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_proc p ON p.oid=t.tgfoid
		WHERE NOT t.tgisinternal AND t.tgenabled<>'D' AND n.nspname=current_schema()
		ORDER BY c.relname, t.tgname`)
	if err != nil {
		return nil, err
	}
	var out []suspendedTrigger
	for rows.Next() {
		var t suspendedTrigger
		var fn string
		if err := rows.Scan(&t.table, &t.name, &t.qualified, &t.def, &t.enabled, &t.onInsert, &fn); err != nil {
			rows.Close()
			return nil, err
		}
		t.writes = functionWrites(fn, bodies)
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range out {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quoteIdent(t.table)+` DISABLE TRIGGER `+quoteIdent(t.name)); err != nil {
			return nil, fmt.Errorf("suspend trigger %s on %s: %w", t.name, t.table, err)
		}
	}
	return out, nil
}

func suspendSQLiteTriggers(ctx context.Context, tx *sql.Tx) ([]suspendedTrigger, error) {
	// rowid order is creation order, which recreation keeps.
	rows, err := tx.QueryContext(ctx, `SELECT name, tbl_name, sql FROM sqlite_master WHERE type='trigger' AND sql IS NOT NULL ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	var out []suspendedTrigger
	for rows.Next() {
		var t suspendedTrigger
		if err := rows.Scan(&t.name, &t.table, &t.def); err != nil {
			rows.Close()
			return nil, err
		}
		t.onInsert = reSQLiteOnInsert.MatchString(t.def)
		t.writes = map[string]bool{}
		for _, m := range rePLWrite.FindAllStringSubmatch(t.def, -1) {
			t.writes[m[1]] = true
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range out {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER `+quoteIdent(t.name)); err != nil {
			return nil, fmt.Errorf("suspend trigger %s on %s: %w", t.name, t.table, err)
		}
	}
	return out, nil
}

// functionWrites lists the tables fn writes, following calls into other
// functions of the schema.
func functionWrites(fn string, bodies map[string]string) map[string]bool {
	writes := map[string]bool{}
	seen := map[string]bool{fn: true}
	queue := []string{fn}
	for len(queue) > 0 {
		body := bodies[queue[0]]
		queue = queue[1:]
		for _, m := range rePLWrite.FindAllStringSubmatch(body, -1) {
			writes[m[1]] = true
		}
		for _, m := range rePLCall.FindAllStringSubmatch(body, -1) {
			if _, ok := bodies[m[1]]; ok && !seen[m[1]] {
				seen[m[1]] = true
				queue = append(queue, m[1])
			}
		}
	}
	return writes
}

// resumeTriggers replays the INSERT triggers that maintain a table the dump
// did not carry (on SQLite that includes every full-text index, which dumps
// leave out) over the restored rows of the table they watch, then puts every
// suspended trigger back as it was.
func resumeTriggers(ctx context.Context, tx *sql.Tx, d Dialect, triggers []suspendedTrigger, restored map[string]bool) error {
	query := `SELECT name FROM sqlite_master WHERE type='table'`
	if d == Postgres {
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE'`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	absent := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if !restored[name] && !maintained(name) {
			absent[name] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	replay := map[string][]suspendedTrigger{}
	var tables []string
	for _, t := range triggers {
		if !t.onInsert || !restored[t.table] {
			continue
		}
		for w := range t.writes {
			if absent[w] {
				if replay[t.table] == nil {
					tables = append(tables, t.table)
				}
				replay[t.table] = append(replay[t.table], t)
				break
			}
		}
	}
	resume := func(t suspendedTrigger) error {
		stmt := t.def
		if d == Postgres {
			mode := ""
			switch t.enabled {
			case "A":
				mode = "ALWAYS "
			case "R":
				mode = "REPLICA "
			}
			stmt = `ALTER TABLE ` + quoteIdent(t.table) + ` ENABLE ` + mode + `TRIGGER ` + quoteIdent(t.name)
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("resume trigger %s on %s: %w", t.name, t.table, err)
		}
		return nil
	}
	// Rows a replay writes into a table the dump lacks are new writes, so
	// that table's own triggers are live for them first.
	for _, t := range triggers {
		if absent[t.table] {
			if err := resume(t); err != nil {
				return err
			}
		}
	}
	for _, table := range tables {
		if err := replayInserts(ctx, tx, d, table, replay[table]); err != nil {
			return fmt.Errorf("rebuild state derived from %s: %w", table, err)
		}
	}
	for _, t := range triggers {
		if !absent[t.table] {
			if err := resume(t); err != nil {
				return err
			}
		}
	}
	return nil
}

func maintained(table string) bool {
	regMu.Lock()
	defer regMu.Unlock()
	return restoreMaintains[table]
}

// replayInserts fires triggers as if table's restored rows were inserted
// again: the same trigger definitions on a temporary copy of the table's
// columns receive every row.
func replayInserts(ctx context.Context, tx *sql.Tx, d Dialect, table string, triggers []suspendedTrigger) error {
	const name = "ether_restore_replay"
	replica := "temp." + name
	create := `CREATE TEMP TABLE ` + name + ` AS SELECT * FROM ` + quoteIdent(table) + ` WHERE 0`
	if d == Postgres {
		replica = "pg_temp." + name
		create = `CREATE TEMP TABLE ` + name + ` (LIKE ` + quoteIdent(table) + `)`
	}
	if _, err := tx.ExecContext(ctx, create); err != nil {
		return err
	}
	for _, t := range triggers {
		def, err := retarget(d, t, name, replica)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, def); err != nil {
			return fmt.Errorf("trigger %s: %w", t.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+replica+` SELECT * FROM `+quoteIdent(table)); err != nil {
		return err
	}
	// Dropping the table drops the triggers just made on it.
	_, err := tx.ExecContext(ctx, `DROP TABLE `+replica)
	return err
}

// retarget rewrites a trigger definition onto the replay table.
func retarget(d Dialect, t suspendedTrigger, name, replica string) (string, error) {
	if d == Postgres {
		on := " ON " + t.qualified + " "
		if strings.Count(t.def, on) != 1 {
			return "", fmt.Errorf("unexpected definition of trigger %s", t.name)
		}
		return strings.Replace(t.def, on, " ON "+replica+" ", 1), nil
	}
	// A temporary trigger may write the main schema's tables, as these do.
	re := regexp.MustCompile(`(?is)^(.*?\bON\s+)(?:"` + regexp.QuoteMeta(t.table) + `"|` + regexp.QuoteMeta(t.table) + `)(\s)`)
	loc := re.FindStringSubmatchIndex(t.def)
	if loc == nil || !reSQLiteCreate.MatchString(t.def) {
		return "", fmt.Errorf("unexpected definition of trigger %s", t.name)
	}
	def := t.def[:loc[3]] + name + t.def[loc[4]:]
	return reSQLiteCreate.ReplaceAllString(def, "CREATE TEMP TRIGGER"), nil
}
