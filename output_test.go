package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUnicodeTruncation(t *testing.T) {
	got := quoteAndTrunc("a😀é中z", 3)
	if got != "a😀é... (truncated, 5 chars total)" || !utf8.ValidString(got) {
		t.Fatalf("truncation corrupted Unicode or counted bytes: %q", got)
	}
	if got := quoteAndTrunc("😀é", 2); got != "😀é" {
		t.Fatalf("unnecessary truncation: %q", got)
	}
}

func TestDatesKeepFractionalSeconds(t *testing.T) {
	date := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	for _, value := range []any{date, bson.NewDateTimeFromTime(date)} {
		if got := scalar(value, 0); got != "2026-01-02T03:04:05.123Z" {
			t.Fatalf("lost date precision: %q", got)
		}
	}
}

func TestControlCharactersAreEscaped(t *testing.T) {
	for _, value := range []string{"\x1b[31mred", "a\tb", "a\rb", "\x00"} {
		got := scalar(value, 0)
		if strings.ContainsAny(got, "\x1b\t\r\x00") {
			t.Fatalf("raw terminal control character in %q", got)
		}
	}
}

func TestExtendedJSONPreservesBSONTypes(t *testing.T) {
	id := bson.ObjectID{1, 2, 3}
	decimal, err := bson.ParseDecimal128("1234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	original := bson.D{
		{Key: "_id", Value: id},
		{Key: "large", Value: int64(9007199254740993)},
		{Key: "date", Value: bson.DateTime(1767225600123)},
		{Key: "decimal", Value: decimal},
		{Key: "text", Value: strings.Repeat("😀", 400)},
	}
	var out bytes.Buffer
	if err := writeJSON(&out, original); err != nil {
		t.Fatal(err)
	}
	// Decode through the BSON Extended JSON parser, not the formatter's own
	// representation: loss of type or precision changes the resulting document.
	var decoded bson.D
	if err := bson.UnmarshalExtJSON(out.Bytes(), true, &decoded); err != nil {
		t.Fatal(err)
	}
	before, err := bson.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := bson.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("BSON types or values changed: %s", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestJSONWriteFailurePropagates(t *testing.T) {
	if err := writeJSON(failingWriter{}, bson.D{}); err == nil {
		t.Fatal("output failure reported success")
	}
}
