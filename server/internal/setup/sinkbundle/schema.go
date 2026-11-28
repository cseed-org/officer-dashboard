package sinkbundle

import (
	"regexp"
	"strings"
	"unicode"
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
		"snapshots": "create table snapshots(id text primary key,kind text not null check(kind in('hourly','daily','d1-weekly')),created_at text not null default(strftime('%Y-%m-%dT%H:%M:%fZ','now')),sha256 text not null,size integer not null check(size between 1 and 24700000),chunks integer not null check(chunks between 1 and 13),committed integer not null default 0,allocation integer not null,write_cost integer not null)",
	}
	TriggerDefinitions = map[string]string{
		"reserve": "create trigger reserve before insert on snapshots begin select case when(select count(*)from snapshots)>= 100 or coalesce((select sum(allocation)from snapshots),0)+ new.allocation > 450000000 or coalesce((select writes from budgets where day=date('now')),0)+ new.write_cost > 40000 or coalesce((select uploads from budgets where day=date('now')),0)>= 200 then raise(abort,'backup budget exhausted')end;insert into budgets(day,writes,uploads)values(date('now'),new.write_cost,1)on conflict(day)do update set writes=writes+new.write_cost,uploads=uploads+1;end",
	}
)

var (
	sqlComment = regexp.MustCompile(`--[^\n]*`)
)

// NormalizeSQL makes two spellings of one CREATE statement compare equal: no comments,
// lower case, no identifier quotes, single spaces, none around ( ) , and ;, and no
// trailing semicolon. String literals ('…', with ” escapes) are copied exactly as
// written, since a CHECK or DEFAULT that differs only inside one is another shape
// (review G-1 of #129). It is for comparing definitions, never for running SQL.
func NormalizeSQL(s string) string {
	var b strings.Builder
	space := false
	punct := func(c byte) bool { return strings.IndexByte("(),;", c) >= 0 }
	emit := func(tok string) {
		if space && b.Len() > 0 && !punct(b.String()[b.Len()-1]) && !punct(tok[0]) {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString(tok)
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\'':
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			emit(s[i:j])
			i = j
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			space = true
		case unicode.IsSpace(rune(c)):
			space = true
			i++
		case c == '"' || c == '`' || c == '[' || c == ']':
			i++
		default:
			emit(strings.ToLower(string(c)))
			i++
		}
	}
	return strings.TrimSuffix(b.String(), ";")
}

var (
	createTableAt   = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?\s*\(`)
	createTriggerAt = regexp.MustCompile(`(?is)CREATE\s+TRIGGER\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?.*?;\s*END\b`)
)

// Definitions returns the normalized CREATE TABLE and CREATE TRIGGER statements in sql,
// by name.
func Definitions(sql string) (tables, triggers map[string]string) {
	tables, triggers = Statements(sql)
	for _, m := range []map[string]string{tables, triggers} {
		for k, v := range m {
			m[k] = NormalizeSQL(v)
		}
	}
	return tables, triggers
}

// Statements returns the CREATE TABLE and CREATE TRIGGER statements in sql as written,
// by name: a table up to the parenthesis that closes its column list, a trigger up to its
// END, as sqlite_master stores them (comments dropped).
func Statements(sql string) (tables, triggers map[string]string) {
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
			tables[name] = sql[m[0]:end]
		}
	}
	for _, m := range createTriggerAt.FindAllStringSubmatch(sql, -1) {
		triggers[strings.ToLower(m[1])] = m[0]
	}
	return tables, triggers
}
