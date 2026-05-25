package main

import (
	"testing"
	"time"
)

func TestCoerce_Timestamp(t *testing.T) {
	at := time.Date(2026, 5, 24, 12, 30, 45, 0, time.UTC)
	for _, pgType := range []string{
		"timestamp with time zone",
		"TIMESTAMPTZ",
		"timestamp without time zone",
		"date",
	} {
		t.Run(pgType, func(t *testing.T) {
			got := coerce(pgType, at)
			s, ok := got.(string)
			if !ok {
				t.Fatalf("coerce(%q, time) = %T %v; want string", pgType, got, got)
			}
			if s != "2026-05-24T12:30:45Z" {
				t.Errorf("coerce(%q, time) = %q; want RFC3339 UTC", pgType, s)
			}
		})
	}
}

func TestCoerce_JSONB(t *testing.T) {
	got := coerce("jsonb", []byte(`{"k":"v"}`))
	s, ok := got.(string)
	if !ok {
		t.Fatalf("coerce(jsonb, []byte) = %T; want string", got)
	}
	if s != `{"k":"v"}` {
		t.Errorf("jsonb body lost: got %q", s)
	}
}

func TestCoerce_Boolean(t *testing.T) {
	if got := coerce("boolean", true); got != int64(1) {
		t.Errorf("coerce(boolean, true) = %v; want int64(1)", got)
	}
	if got := coerce("boolean", false); got != int64(0) {
		t.Errorf("coerce(boolean, false) = %v; want int64(0)", got)
	}
}

func TestCoerce_BYTEA_PassThrough(t *testing.T) {
	raw := []byte{0xde, 0xad, 0xbe, 0xef}
	got := coerce("bytea", raw)
	b, ok := got.([]byte)
	if !ok {
		t.Fatalf("coerce(bytea, []byte) = %T; want []byte", got)
	}
	if len(b) != 4 || b[0] != 0xde {
		t.Errorf("bytea bytes corrupted: got %x", b)
	}
}

func TestCoerce_Nil(t *testing.T) {
	for _, pgType := range []string{"timestamptz", "jsonb", "bytea", "boolean", "text", "integer"} {
		if got := coerce(pgType, nil); got != nil {
			t.Errorf("coerce(%q, nil) = %v; want nil", pgType, got)
		}
	}
}

func TestCoerce_TextPassThrough(t *testing.T) {
	if got := coerce("text", "hello"); got != "hello" {
		t.Errorf("text passthrough broken: got %v", got)
	}
}

func TestRedactPassword(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"postgres://pulse:hunter2@db:5432/pulse_db", "postgres://pulse:***@db:5432/pulse_db"},
		{"postgres://user:p%40ss@host/db", "postgres://user:***@host/db"},
		{"postgres://host/db", "postgres://host/db"}, // no userinfo
		{"postgres://user@host/db", "postgres://user@host/db"}, // no password
		{"not-a-dsn", "not-a-dsn"},
	}
	for _, tc := range cases {
		if got := redactPassword(tc.in); got != tc.want {
			t.Errorf("redactPassword(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}
