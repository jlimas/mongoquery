package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestPipelineSafety(t *testing.T) {
	tests := []struct {
		name, input, forbidden string
	}{
		{"group", `[{"$match":{"active":true}},{"$group":{"_id":"$team","n":{"$sum":1}}}]`, ""},
		{"lookup", `[{"$lookup":{"from":"users","pipeline":[{"$match":{"active":true}}],"as":"users"}}]`, ""},
		{"out", `[{"$out":"archive"}]`, "$out"},
		{"merge", `[{"$merge":{"into":"archive"}}]`, "$merge"},
		{"nested code", `[{"$lookup":{"from":"users","pipeline":[{"$project":{"value":{"$function":{"body":"function(){return 1}","args":[],"lang":"js"}}}}],"as":"users"}}]`, "$function"},
		{"facet code", `[{"$facet":{"items":[{"$match":{"$where":"return true"}}]}}]`, "$where"},
		{"accumulator", `[{"$group":{"_id":null,"n":{"$accumulator":{"init":"function(){return 0}","lang":"js"}}}}]`, "$accumulator"},
		{"bad stage", `[42]`, "not a document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline, err := parseExtJSONArray(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			err = validatePipeline(pipeline)
			if tt.forbidden == "" {
				if err != nil {
					t.Fatalf("safe pipeline rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.forbidden) {
				t.Fatalf("want rejection mentioning %q, got %v", tt.forbidden, err)
			}
		})
	}
}

func TestFindRejectsCodeInProjectionBeforeConnecting(t *testing.T) {
	t.Setenv("MONGOQUERY_CONFIG", t.TempDir()+"/missing.yaml")
	err := cmdFind(context.Background(), []string{
		"--env", "production", "--service", "orders", "--collection", "users",
		"--projection", `{"computed":{"$function":{"body":"function(){return 1}","args":[],"lang":"js"}}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden operator $function") {
		t.Fatalf("unsafe projection reached config/connection: %v", err)
	}
}

func TestInvalidBoundsRejectedBeforeConnecting(t *testing.T) {
	t.Setenv("MONGOQUERY_CONFIG", t.TempDir()+"/missing.yaml")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"find", "--limit", "0"}, "--limit"},
		{[]string{"find", "--skip", "-1"}, "--skip"},
		{[]string{"aggregate", "--limit", "-1"}, "--limit"},
		{[]string{"distinct", "--limit", "0"}, "--limit"},
		{[]string{"count", "--timeout", "0"}, "--timeout"},
		{[]string{"collections", "--timeout", "-1"}, "--timeout"},
		{[]string{"find", "--timeout", "9223372036854775807"}, "--timeout"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			err := run(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %s validation error, got %v", tt.want, err)
			}
		})
	}
}

func TestCollectDocumentsPropagatesCursorFailure(t *testing.T) {
	// A real driver cursor with a terminal error reproduces Next returning false
	// for failure rather than exhaustion (the original loop treated both alike).
	failure := errors.New("getMore failed")
	cursor, err := mongo.NewCursorFromDocuments(nil, failure, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cursor.Close(context.Background())
	docs, err := collectDocuments(context.Background(), cursor, 10)
	if !errors.Is(err, failure) || len(docs) != 0 {
		t.Fatalf("want failure without successful results, got %v, %v", docs, err)
	}
}

func TestCollectDocumentsHonorsCap(t *testing.T) {
	cursor, err := mongo.NewCursorFromDocuments([]any{
		map[string]any{"n": int32(1)}, map[string]any{"n": int32(2)},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cursor.Close(context.Background())
	docs, err := collectDocuments(context.Background(), cursor, 1)
	if err != nil || len(docs) != 1 || docs[0]["n"] != int32(1) {
		t.Fatalf("unexpected capped results: %v, %v", docs, err)
	}
}
