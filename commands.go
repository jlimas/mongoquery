package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const defaultTimeout = 30 * time.Second

type targetFlags struct {
	env, service, collection string
}

func registerTarget(fs *flag.FlagSet, t *targetFlags, withCollection bool) {
	fs.StringVar(&t.env, "env", "", "environment name from config (required)")
	fs.StringVar(&t.service, "service", "", "service name from config (required)")
	if withCollection {
		fs.StringVar(&t.collection, "collection", "", "collection name (required)")
	}
}

func (t targetFlags) validate(withCollection bool) error {
	var missing []string
	if t.env == "" {
		missing = append(missing, "--env")
	}
	if t.service == "" {
		missing = append(missing, "--service")
	}
	if withCollection && t.collection == "" {
		missing = append(missing, "--collection")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateBounds(timeout, limit int, skip int64) error {
	if timeout <= 0 || time.Duration(timeout) > time.Duration(1<<63-1)/time.Second {
		return fmt.Errorf("--timeout must be positive and fit in a duration")
	}
	if limit <= 0 {
		return fmt.Errorf("--limit must be positive")
	}
	if skip < 0 {
		return fmt.Errorf("--skip must be nonnegative")
	}
	return nil
}

// collectDocuments never returns partial results after a cursor failure.
func collectDocuments(ctx context.Context, cursor *mongo.Cursor, limit int) ([]bson.M, error) {
	var docs []bson.M
	for len(docs) < limit && cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

// connect opens a Mongo client and returns the resolved database handle.
func connect(ctx context.Context, cfg *Config, env, service string) (*mongo.Client, *mongo.Database, error) {
	svc, err := cfg.resolve(env, service)
	if err != nil {
		return nil, nil, err
	}
	opts := options.Client().ApplyURI(svc.URI).SetReadPreference(readpref.SecondaryPreferred())
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("connect failed: %w", err)
	}
	dbName := svc.Database
	if dbName == "" {
		dbName = parseDBFromURI(svc.URI)
	}
	if dbName == "" {
		_ = client.Disconnect(ctx)
		return nil, nil, fmt.Errorf("no database in uri or config for %s/%s", env, service)
	}
	return client, client.Database(dbName), nil
}

// parseDBFromURI extracts the database name from a Mongo URI.
func parseDBFromURI(uri string) string {
	i := strings.Index(uri, "://")
	if i < 0 {
		return ""
	}
	rest := uri[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[j+1:]
		if k := strings.IndexAny(rest, "?"); k >= 0 {
			rest = rest[:k]
		}
		return rest
	}
	return ""
}

// parseExtJSONDoc parses a MongoDB Extended JSON document.
func parseExtJSONDoc(s string) (bson.D, error) {
	if strings.TrimSpace(s) == "" {
		return bson.D{}, nil
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(s), false, &d); err != nil {
		return nil, fmt.Errorf("invalid query JSON: %w", err)
	}
	return d, nil
}

// parseExtJSONArray parses a MongoDB Extended JSON array (for pipelines).
func parseExtJSONArray(s string) (bson.A, error) {
	if strings.TrimSpace(s) == "" {
		return bson.A{}, nil
	}
	wrapped := []byte(`{"p":` + s + `}`)
	var w struct {
		P bson.A `bson:"p"`
	}
	if err := bson.UnmarshalExtJSON(wrapped, false, &w); err != nil {
		return nil, fmt.Errorf("invalid pipeline JSON: %w", err)
	}
	return w.P, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------------- find ----------------

func cmdFind(parentCtx context.Context, args []string) error {
	fs := flag.NewFlagSet("find", flag.ContinueOnError)
	var t targetFlags
	registerTarget(fs, &t, true)
	query := fs.String("query", "{}", "filter as MongoDB Extended JSON")
	projection := fs.String("projection", "", "projection as MongoDB Extended JSON")
	sortFlag := fs.String("sort", "", "sort as MongoDB Extended JSON, e.g. '{\"createdAt\":-1}'")
	limit := fs.Int("limit", 20, "max documents to return")
	count := fs.Bool("count", false, "include an exact matching count (may scan many documents)")
	skip := fs.Int64("skip", 0, "documents to skip")
	fields := fs.String("fields", "", "comma-separated top-level fields to display")
	full := fs.Bool("full", false, "do not truncate string values")
	timeoutSec := fs.Int("timeout", 30, "query timeout in seconds")
	fs.Usage = func() { findUsage() }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBounds(*timeoutSec, *limit, *skip); err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := t.validate(true); err != nil {
		printError(err.Error(), "Run `mongoquery find --help` for flag reference")
		return err
	}
	filter, err := parseExtJSONDoc(*query)
	if err != nil {
		printError(err.Error(), `Example: --query '{"email":"x@y.com"}'`)
		return err
	}
	if err := validateFilter(filter); err != nil {
		printError(err.Error(), "Remove $where/$function/$accumulator from the filter")
		return err
	}
	proj, err := parseExtJSONDoc(*projection)
	if err != nil {
		printError(err.Error(), `Example: --projection '{"_id":0,"email":1}'`)
		return err
	}
	if err := scanForbidden(proj); err != nil {
		printError(err.Error(), "Remove forbidden operators from the projection")
		return err
	}
	sortDoc, err := parseExtJSONDoc(*sortFlag)
	if err != nil {
		printError(err.Error(), `Example: --sort '{"createdAt":-1}'`)
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	client, db, err := connect(ctx, cfg, t.env, t.service)
	if err != nil {
		printError(err.Error(), "Run `mongoquery list` to see configured targets")
		return err
	}
	defer client.Disconnect(ctx)

	coll := db.Collection(t.collection)
	opts := options.Find().SetLimit(int64(*limit)).SetSkip(*skip)
	if len(proj) > 0 {
		opts.SetProjection(proj)
	}
	if len(sortDoc) > 0 {
		opts.SetSort(sortDoc)
	}

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		printError(fmt.Sprintf("find failed: %s", err), "Verify collection name with `mongoquery collections`")
		return err
	}
	defer cursor.Close(ctx)
	docs, err := collectDocuments(ctx, cursor, *limit)
	if err != nil {
		printError(fmt.Sprintf("decode failed: %s", err), "")
		return err
	}

	totalI := -1
	if *count {
		total, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			printError(fmt.Sprintf("count failed: %s", err), "Omit --count to retrieve documents without an exact total")
			return err
		}
		totalI = int(total)
	}

	truncate := defaultStringTruncate
	if *full {
		truncate = 0
	}
	renderResults("results", docs, len(docs), totalI, splitCSV(*fields), truncate)

	if len(docs) == 0 {
		printHelp(
			fmt.Sprintf("0 documents in %s.%s match the filter", db.Name(), t.collection),
			"Run `mongoquery collections --env "+t.env+" --service "+t.service+"` to verify the collection",
		)
		return nil
	}
	hints := []string{}
	if totalI > len(docs) {
		hints = append(hints, fmt.Sprintf("Increase --limit (currently %d) to see more of the %d total", *limit, totalI))
	}
	if !*full {
		hints = append(hints, "Add --full to see untruncated string values")
	}
	hints = append(hints, "Use --fields _id,name,... to narrow output", "Use --sort '{\"createdAt\":-1}' to order results")
	printHelp(hints...)
	return nil
}

func findUsage() {
	o := newOut()
	o.line("usage: mongoquery find --env <env> --service <svc> --collection <c> [flags]")
	o.line("flags:")
	o.line("  --query JSON         filter (default '{}')")
	o.line("  --projection JSON    e.g. '{\"_id\":0,\"email\":1}'")
	o.line("  --sort JSON          e.g. '{\"createdAt\":-1}'")
	o.line("  --limit N            default 20")
	o.line("  --count              include exact total (potentially expensive)")
	o.line("  --skip N             default 0")
	o.line("  --fields a,b,c       restrict displayed top-level fields")
	o.line("  --full               disable string truncation")
	o.line("  --timeout N          seconds, default 30")
	o.line("examples:")
	o.line("  mongoquery find --env staging --service orders --collection users --query '{\"email\":\"x@y.com\"}'")
	o.line("  mongoquery find --env production --service billing --collection invoices --sort '{\"createdAt\":-1}' --limit 5")
}

// ---------------- aggregate ----------------

func cmdAggregate(parentCtx context.Context, args []string) error {
	fs := flag.NewFlagSet("aggregate", flag.ContinueOnError)
	var t targetFlags
	registerTarget(fs, &t, true)
	pipeline := fs.String("pipeline", "[]", "pipeline as MongoDB Extended JSON array")
	limit := fs.Int("limit", 100, "soft cap on returned documents")
	fields := fs.String("fields", "", "comma-separated top-level fields to display")
	full := fs.Bool("full", false, "do not truncate string values")
	timeoutSec := fs.Int("timeout", 60, "query timeout in seconds")
	fs.Usage = func() {
		o := newOut()
		o.line("usage: mongoquery aggregate --env <env> --service <svc> --collection <c> --pipeline JSON [flags]")
		o.line("flags:")
		o.line("  --pipeline JSON   aggregation array (required)")
		o.line("  --limit N         soft cap on results (default 100)")
		o.line("  --fields a,b,c    restrict displayed top-level fields")
		o.line("  --full            disable string truncation")
		o.line("  --timeout N       seconds, default 60")
		o.line("forbidden stages: $out, $merge")
		o.line("forbidden operators: $where, $function, $accumulator")
		o.line("example:")
		o.line("  mongoquery aggregate --env staging --service orders --collection orders --pipeline '[{\"$match\":{\"status\":\"paid\"}},{\"$group\":{\"_id\":\"$userId\",\"n\":{\"$sum\":1}}}]'")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBounds(*timeoutSec, *limit, 0); err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := t.validate(true); err != nil {
		printError(err.Error(), "Run `mongoquery aggregate --help` for flag reference")
		return err
	}
	pipe, err := parseExtJSONArray(*pipeline)
	if err != nil {
		printError(err.Error(), "Pipeline must be a JSON array, e.g. '[{\"$match\":{}}]'")
		return err
	}
	if err := validatePipeline(pipe); err != nil {
		printError(err.Error(), "Remove the forbidden stage/operator and retry")
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	client, db, err := connect(ctx, cfg, t.env, t.service)
	if err != nil {
		printError(err.Error(), "Run `mongoquery list` to see configured targets")
		return err
	}
	defer client.Disconnect(ctx)

	coll := db.Collection(t.collection)
	cursor, err := coll.Aggregate(ctx, pipe)
	if err != nil {
		printError(fmt.Sprintf("aggregate failed: %s", err), "Verify pipeline syntax against the MongoDB docs")
		return err
	}
	defer cursor.Close(ctx)

	docs, err := collectDocuments(ctx, cursor, *limit)
	if err != nil {
		printError(fmt.Sprintf("read results failed: %s", err), "")
		return err
	}

	truncate := defaultStringTruncate
	if *full {
		truncate = 0
	}
	renderResults("results", docs, len(docs), -1, splitCSV(*fields), truncate)
	if len(docs) == 0 {
		printHelp("Pipeline returned 0 documents", "Verify $match clauses and field names")
		return nil
	}
	hints := []string{}
	if len(docs) >= *limit {
		hints = append(hints, fmt.Sprintf("Output capped at --limit %d; raise it for more results", *limit))
	}
	if !*full {
		hints = append(hints, "Add --full to see untruncated string values")
	}
	hints = append(hints, "Use --fields a,b to narrow output")
	printHelp(hints...)
	return nil
}

// ---------------- count ----------------

func cmdCount(parentCtx context.Context, args []string) error {
	fs := flag.NewFlagSet("count", flag.ContinueOnError)
	var t targetFlags
	registerTarget(fs, &t, true)
	query := fs.String("query", "{}", "filter as MongoDB Extended JSON")
	timeoutSec := fs.Int("timeout", 30, "query timeout in seconds")
	fs.Usage = func() {
		o := newOut()
		o.line("usage: mongoquery count --env <env> --service <svc> --collection <c> [--query JSON]")
		o.line("example:")
		o.line("  mongoquery count --env staging --service orders --collection users --query '{\"active\":true}'")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBounds(*timeoutSec, 1, 0); err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := t.validate(true); err != nil {
		printError(err.Error(), "Run `mongoquery count --help` for flag reference")
		return err
	}
	filter, err := parseExtJSONDoc(*query)
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := validateFilter(filter); err != nil {
		printError(err.Error(), "")
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	client, db, err := connect(ctx, cfg, t.env, t.service)
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	defer client.Disconnect(ctx)
	n, err := db.Collection(t.collection).CountDocuments(ctx, filter)
	if err != nil {
		printError(fmt.Sprintf("count failed: %s", err), "")
		return err
	}
	o := newOut()
	o.line("count:")
	o.line("  collection: %s.%s", db.Name(), t.collection)
	o.line("  matched: %d", n)
	return nil
}

// ---------------- distinct ----------------

func cmdDistinct(parentCtx context.Context, args []string) error {
	fs := flag.NewFlagSet("distinct", flag.ContinueOnError)
	var t targetFlags
	registerTarget(fs, &t, true)
	field := fs.String("field", "", "field name to compute distinct values for (required)")
	query := fs.String("query", "{}", "filter as MongoDB Extended JSON")
	limit := fs.Int("limit", 100, "max values to display; server still computes all distinct values")
	timeoutSec := fs.Int("timeout", 30, "query timeout in seconds")
	fs.Usage = func() {
		o := newOut()
		o.line("usage: mongoquery distinct --env <env> --service <svc> --collection <c> --field <f> [--query JSON]")
		o.line("flags:")
		o.line("  --limit N         max values displayed, default 100 (not a server-side limit)")
		o.line("  --timeout N       seconds, default 30")
		o.line("warning: MongoDB computes all distinct values; use a $group/$limit pipeline for high-cardinality fields")
		o.line("example:")
		o.line("  mongoquery distinct --env staging --service orders --collection orders --field status")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBounds(*timeoutSec, *limit, 0); err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := t.validate(true); err != nil {
		printError(err.Error(), "Run `mongoquery distinct --help` for flag reference")
		return err
	}
	if *field == "" {
		printError("missing required flag: --field", "Run `mongoquery distinct --help`")
		return fmt.Errorf("missing field")
	}
	filter, err := parseExtJSONDoc(*query)
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := validateFilter(filter); err != nil {
		printError(err.Error(), "")
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	client, db, err := connect(ctx, cfg, t.env, t.service)
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	defer client.Disconnect(ctx)

	res := db.Collection(t.collection).Distinct(ctx, *field, filter)
	if err := res.Err(); err != nil {
		printError(fmt.Sprintf("distinct failed: %s", err), "")
		return err
	}
	var values []any
	if err := res.Decode(&values); err != nil {
		printError(fmt.Sprintf("decode failed: %s", err), "")
		return err
	}
	total := len(values)
	shown := total
	if shown > *limit {
		shown = *limit
	}
	o := newOut()
	o.line("distinct[%d of %d total]:", shown, total)
	for i := 0; i < shown; i++ {
		o.line("  - %s", scalar(values[i], 0))
	}
	if total > shown {
		printHelp(fmt.Sprintf("Raise --limit (currently %d) to see all %d values", *limit, total))
	}
	return nil
}

// ---------------- collections ----------------

func cmdCollections(parentCtx context.Context, args []string) error {
	fs := flag.NewFlagSet("collections", flag.ContinueOnError)
	var t targetFlags
	registerTarget(fs, &t, false)
	timeoutSec := fs.Int("timeout", 15, "query timeout in seconds")
	fs.Usage = func() {
		o := newOut()
		o.line("usage: mongoquery collections --env <env> --service <svc>")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateBounds(*timeoutSec, 1, 0); err != nil {
		printError(err.Error(), "")
		return err
	}
	if err := t.validate(false); err != nil {
		printError(err.Error(), "Run `mongoquery collections --help`")
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	client, db, err := connect(ctx, cfg, t.env, t.service)
	if err != nil {
		printError(err.Error(), "")
		return err
	}
	defer client.Disconnect(ctx)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		printError(fmt.Sprintf("listCollections failed: %s", err), "")
		return err
	}
	o := newOut()
	o.line("collections[%d] in %s:", len(names), db.Name())
	for _, n := range names {
		o.line("  - %s", n)
	}
	if len(names) == 0 {
		printHelp("Database has no collections accessible to this user")
		return nil
	}
	printHelp(fmt.Sprintf("Run `mongoquery find --env %s --service %s --collection <name> --query '{}'` to query one", t.env, t.service))
	return nil
}
