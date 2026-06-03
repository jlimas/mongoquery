package main

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Forbidden aggregation stages (mutate or sink data).
var forbiddenStages = map[string]bool{
	"$out":   true,
	"$merge": true,
}

// Forbidden expression operators (execute server-side JS or mutate).
var forbiddenOperators = map[string]bool{
	"$function":    true,
	"$accumulator": true,
	"$where":       true,
}

// validatePipeline walks an aggregation pipeline and rejects unsafe stages/operators.
func validatePipeline(pipeline bson.A) error {
	for i, stage := range pipeline {
		doc, ok := stage.(bson.D)
		if !ok {
			m, ok := stage.(bson.M)
			if !ok {
				return fmt.Errorf("stage %d is not a document", i)
			}
			for k := range m {
				if forbiddenStages[k] {
					return fmt.Errorf("stage %d uses forbidden operator %s (writes data)", i, k)
				}
			}
			if err := scanForbidden(m); err != nil {
				return fmt.Errorf("stage %d: %w", i, err)
			}
			continue
		}
		for _, e := range doc {
			if forbiddenStages[e.Key] {
				return fmt.Errorf("stage %d uses forbidden operator %s (writes data)", i, e.Key)
			}
		}
		if err := scanForbidden(doc); err != nil {
			return fmt.Errorf("stage %d: %w", i, err)
		}
	}
	return nil
}

// scanForbidden recursively walks a BSON value looking for forbidden operators.
func scanForbidden(v any) error {
	switch x := v.(type) {
	case bson.D:
		for _, e := range x {
			if strings.HasPrefix(e.Key, "$") && forbiddenOperators[e.Key] {
				return fmt.Errorf("forbidden operator %s (executes server-side code)", e.Key)
			}
			if err := scanForbidden(e.Value); err != nil {
				return err
			}
		}
	case bson.M:
		for k, vv := range x {
			if strings.HasPrefix(k, "$") && forbiddenOperators[k] {
				return fmt.Errorf("forbidden operator %s (executes server-side code)", k)
			}
			if err := scanForbidden(vv); err != nil {
				return err
			}
		}
	case bson.A:
		for _, item := range x {
			if err := scanForbidden(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range x {
			if err := scanForbidden(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateFilter checks find/count/distinct filters for forbidden operators.
func validateFilter(filter bson.D) error {
	return scanForbidden(filter)
}
