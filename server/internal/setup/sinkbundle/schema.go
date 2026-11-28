package sinkbundle

import (
	"regexp"
	"strings"
)

// The backup database's expected shape: each table's and trigger's CREATE statement as
// the migrations write it, normalized with NormalizeSQL. setup compares them with the
// sql column of sqlite_master ("The text is usually a copy of the original statement used
// to create the object but with normalizations applied", https://www.sqlite.org/schematab.html),
// normalized the same way, so a table of the right name but another shape is drift.
// TestDefinitionsMatchTheMigrations pins them to the migration files.
var (
	TableDefinitions = map[string]string{
		"budgets":   "create table budgets(day text primary key,writes integer not null default 0,uploads integer not null default 0)",
		"chunks":    "create table chunks(snapshot_id text not null references snapshots(id)on delete cascade,n integer not null,blob blob not null check(length(blob)between 1 and 1900000),primary key(snapshot_id,n))",
		"snapshots": "create table snapshots(id text primary key,kind text not null check(kind in('hourly','daily','d1-weekly')),created_at text not null default(strftime('%y-%m-%dt%h:%m:%fz','now')),sha256 text not null,size integer not null check(size between 1 and 24700000),chunks integer not null check(chunks between 1 and 13),committed integer not null default 0,allocation integer not null,write_cost integer not null)",
	}
	TriggerDefinitions = map[string]string{
		"reserve": "create trigger reserve before insert on snapshots begin select case when(select count(*)from snapshots)>= 100 or coalesce((select sum(allocation)from snapshots),0)+ new.allocation > 450000000 or coalesce((select writes from budgets where day=date('now')),0)+ new.write_cost > 40000 or coalesce((select uploads from budgets where day=date('now')),0)>= 200 then raise(abort,'backup budget exhausted')end;insert into budgets(day,writes,uploads)values(date('now'),new.write_cost,1)on conflict(day)do update set writes=writes+new.write_cost,uploads=uploads+1;end",
	}
)

var (
	sqlComment   = regexp.MustCompile(`--[^\n]*`)
	sqlSpace     = regexp.MustCompile(`\s+`)
	sqlPunctGap  = regexp.MustCompile(` ?([(),;]) ?`)
	sqlQuoteChar = strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "")
)

// NormalizeSQL makes two spellings of one CREATE statement compare equal: no comments,
// lower case, no identifier quotes, single spaces, none around ( ) , and ;, and no
// trailing semicolon. It is for comparing definitions, never for running SQL.
func NormalizeSQL(s string) string {
	s = sqlComment.ReplaceAllString(s, " ")
	s = sqlQuoteChar.Replace(strings.ToLower(s))
	s = sqlSpace.ReplaceAllString(s, " ")
	s = sqlPunctGap.ReplaceAllString(s, "$1")
	return strings.TrimSuffix(strings.TrimSpace(s), ";")
}

var (
	createTableAt   = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?\s*\(`)
	createTriggerAt = regexp.MustCompile(`(?is)CREATE\s+TRIGGER\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?.*?;\s*END\b`)
)

// Definitions returns the normalized CREATE TABLE and CREATE TRIGGER statements in sql,
// by name: a table up to the parenthesis that closes its column list, a trigger up to its
// END, as sqlite_master stores them.
func Definitions(sql string) (tables, triggers map[string]string) {
	sql = sqlComment.ReplaceAllString(sql, " ")
	tables, triggers = map[string]string{}, map[string]string{}
	for _, m := range createTableAt.FindAllStringSubmatchIndex(sql, -1) {
		depth, end := 0, -1
		for i := m[1] - 1; i < len(sql) && end < 0; i++ {
			switch sql[i] {
			case '(':
				depth++
			case ')':
				if depth--; depth == 0 {
					end = i + 1
				}
			}
		}
		if end > 0 {
			name := strings.ToLower(sql[m[2]:m[3]])
			tables[name] = NormalizeSQL(sql[m[0]:end])
		}
	}
	for _, m := range createTriggerAt.FindAllStringSubmatch(sql, -1) {
		triggers[strings.ToLower(m[1])] = NormalizeSQL(m[0])
	}
	return tables, triggers
}
