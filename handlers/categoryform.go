package handlers

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

func parseCategoryIDs(form url.Values) ([]int, error) {
	values := form["category_ids"]
	if len(values) == 0 {
		values = form["category"] // Keep accepting forms from older clients.
	}
	unique := make(map[int]struct{}, len(values))
	categoryIDs := make([]int, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		id, err := strconv.Atoi(value)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("invalid category ID")
		}
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		categoryIDs = append(categoryIDs, id)
	}
	sort.Ints(categoryIDs)
	return categoryIDs, nil
}
