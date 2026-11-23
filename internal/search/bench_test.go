package search

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// The index choice (#481): a plain scan against an FTS5 trigram index over
// made-up mail of the server's size (6,000 messages, about 2 KB each).
//
//	scripts/go test -run '^$' -bench . -benchtime 20x ./internal/search/
//
// A word that occurs nowhere makes the scan read every row; a common one
// stops it after the first page.

const benchMail = 6000

var words = strings.Fields("배포 캐시 시험 검색 대시보드 메일 보고 질문 결정 서버 역할 묶음 지시 완료 막힘 원인 증거 " +
	"deploy cache test search dashboard mail report question decision server role group directive done blocked cause evidence")

func fill(b *testing.B, db *sql.DB) {
	b.Helper()
	r := rand.New(rand.NewPCG(1, 2))
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := range benchMail {
		var body strings.Builder
		for body.Len() < 1500+r.IntN(1000) {
			body.WriteString(words[r.IntN(len(words))])
			if r.IntN(12) == 0 {
				body.WriteString("\n")
			} else {
				body.WriteString(" ")
			}
		}
		at := t0.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		if _, err := tx.Exec(`INSERT INTO messages(sender,recipient,subject,body,created_at) VALUES(?,?,?,?,?)`,
			"team/boss", fmt.Sprintf("team/worker-%d", 1+i%5), fmt.Sprintf("%s %d", words[i%len(words)], i), body.String(), at); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

func benchDB(b *testing.B) *sql.DB {
	db := open(b)
	fill(b, db)
	return db
}

func BenchmarkScan(b *testing.B) {
	db := benchDB(b)
	sv := New(db)
	for _, c := range []struct {
		name string
		q    Query
	}{
		{"absent-word", Query{Pattern: "없는낱말"}},
		{"absent-word-case", Query{Pattern: "nowhere-word", Case: true}},
		{"absent-regex", Query{Pattern: `deploy\s+\d{4}`, Regex: true}},
		{"common-word", Query{Pattern: "대시보드"}},
		{"scoped-absent", Query{Pattern: "nowhere-word", Kinds: []string{"mail"}}},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				res, err := sv.Search(context.Background(), c.q, func(string) bool { return true }, "")
				if err != nil || res.Cut {
					b.Fatal(err, res.Cut)
				}
			}
		})
	}
}

// BenchmarkFTS5Trigram measures the index the scan was weighed against: the
// same rows in an FTS5 table with the trigram tokenizer (case-insensitive
// substrings of three characters or more; no regexp).
func BenchmarkFTS5Trigram(b *testing.B) {
	db := benchDB(b)
	start := time.Now()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE bench_fts USING fts5(subject, body, tokenize='trigram');
		INSERT INTO bench_fts(rowid,subject,body) SELECT id,subject,body FROM messages`); err != nil {
		b.Fatal(err)
	}
	b.Logf("index build: %v", time.Since(start))
	for _, c := range []struct{ name, q string }{{"absent-word", "없는낱말"}, {"common-word", "대시보드"}, {"two-letters", "캐시"}} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				rows, err := db.Query(`SELECT m.id,m.subject,m.body FROM bench_fts f JOIN messages m ON m.id=f.rowid WHERE bench_fts MATCH ? ORDER BY f.rowid DESC LIMIT 51`, `"`+c.q+`"`)
				if err != nil {
					b.Fatal(err)
				}
				for rows.Next() {
				}
				rows.Close()
			}
		})
	}
}
