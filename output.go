package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	defaultStringTruncate = 300
)

// Output writer wraps stdout for structured TOON-style output.
type out struct {
	w io.Writer
}

func newOut() *out { return &out{w: os.Stdout} }

func (o *out) line(format string, args ...any) {
	fmt.Fprintf(o.w, format+"\n", args...)
}

// printError writes a structured error block to stdout.
func printError(msg, help string) {
	w := newOut()
	w.line("error: %s", msg)
	if help != "" {
		w.line("help: %s", help)
	}
}

// printHelp writes a help[] block listing next-step suggestions.
func printHelp(items ...string) {
	if len(items) == 0 {
		return
	}
	o := newOut()
	o.line("help[%d]:", len(items))
	for _, it := range items {
		o.line("  %s", it)
	}
}

// renderResults emits a list of documents in indented block form.
// shown is len(docs); total is the unfiltered total (or -1 if unknown).
// fields, if non-empty, restricts which top-level keys are emitted.
// truncate is the max char length for string values; <=0 disables.
func renderResults(label string, docs []bson.M, shown, total int, fields []string, truncate int) {
	o := newOut()
	header := fmt.Sprintf("%s[%d", label, shown)
	if total >= 0 {
		header += fmt.Sprintf(" of %d total", total)
	}
	header += "]:"
	o.line(header)
	if shown == 0 {
		return
	}
	for _, d := range docs {
		o.line("  -")
		writeDoc(o.w, d, "    ", fields, truncate)
	}
}

func writeDoc(w io.Writer, d bson.M, indent string, fields []string, truncate int) {
	keys := selectKeys(d, fields)
	for _, k := range keys {
		writeKV(w, indent, k, d[k], truncate)
	}
}

func selectKeys(d bson.M, fields []string) []string {
	if len(fields) > 0 {
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			if _, ok := d[f]; ok {
				out = append(out, f)
			}
		}
		return out
	}
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	sort.Strings(out)
	// Place _id first if present.
	for i, k := range out {
		if k == "_id" {
			out = append([]string{"_id"}, append(out[:i], out[i+1:]...)...)
			break
		}
	}
	return out
}

func writeKV(w io.Writer, indent, key string, v any, truncate int) {
	switch x := v.(type) {
	case bson.M:
		fmt.Fprintf(w, "%s%s:\n", indent, key)
		for _, k := range sortedKeysM(x) {
			writeKV(w, indent+"  ", k, x[k], truncate)
		}
	case bson.D:
		fmt.Fprintf(w, "%s%s:\n", indent, key)
		for _, e := range x {
			writeKV(w, indent+"  ", e.Key, e.Value, truncate)
		}
	case bson.A:
		fmt.Fprintf(w, "%s%s[%d]:\n", indent, key, len(x))
		for _, item := range x {
			fmt.Fprintf(w, "%s  - ", indent)
			writeInline(w, item, indent+"    ", truncate)
		}
	case []any:
		fmt.Fprintf(w, "%s%s[%d]:\n", indent, key, len(x))
		for _, item := range x {
			fmt.Fprintf(w, "%s  - ", indent)
			writeInline(w, item, indent+"    ", truncate)
		}
	default:
		fmt.Fprintf(w, "%s%s: %s\n", indent, key, scalar(v, truncate))
	}
}

// writeInline writes a value after "- " (already printed). Adds newline at end.
func writeInline(w io.Writer, v any, indent string, truncate int) {
	switch x := v.(type) {
	case bson.M:
		fmt.Fprintln(w)
		for _, k := range sortedKeysM(x) {
			writeKV(w, indent, k, x[k], truncate)
		}
	case bson.D:
		fmt.Fprintln(w)
		for _, e := range x {
			writeKV(w, indent, e.Key, e.Value, truncate)
		}
	default:
		fmt.Fprintln(w, scalar(v, truncate))
	}
}

func sortedKeysM(m bson.M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// scalar formats a leaf value as a single-line string.
func scalar(v any, truncate int) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return quoteAndTrunc(x, truncate)
	case bool:
		return strconv.FormatBool(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bson.ObjectID:
		return x.Hex()
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case bson.DateTime:
		return x.Time().UTC().Format(time.RFC3339)
	default:
		return quoteAndTrunc(fmt.Sprintf("%v", v), truncate)
	}
}

func quoteAndTrunc(s string, truncate int) string {
	original := len(s)
	if truncate > 0 && original > truncate {
		s = s[:truncate] + "..."
	}
	if needsQuote(s) {
		s = strconv.Quote(s)
	}
	if truncate > 0 && original > truncate {
		s += fmt.Sprintf(" (truncated, %d chars total)", original)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, ":,\n\"#") {
		return true
	}
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return true
	}
	return false
}
