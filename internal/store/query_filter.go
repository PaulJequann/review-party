package store

import (
	"encoding/json"
	"reviewparty/internal/model"
	"strings"
)

type queryFilter struct {
	predicate string
	value     any
	enabled   bool
}

func whereClause(filters []queryFilter) (string, []any) {
	var predicates []string
	var arguments []any
	for _, filter := range filters {
		if filter.enabled {
			predicates = append(predicates, filter.predicate)
			arguments = append(arguments, filter.value)
		}
	}
	if len(predicates) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(predicates, " AND "), arguments
}

// reviewIDsFilter matches column against any of ids; no ids leaves it off.
func reviewIDsFilter(column string, ids []model.ReviewID) (queryFilter, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return queryFilter{}, err
	}
	return queryFilter{column + " IN (SELECT value FROM json_each(?))", string(encoded), len(ids) > 0}, nil
}
