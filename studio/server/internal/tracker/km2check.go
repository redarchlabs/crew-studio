package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"crewstudio/internal/model"
)

// km2Required are the fields Studio reads or writes, per entity, besides the activity field (Check lists the ones
// an organisation lacks). Relationships are not listed by KM2's catalog API, so they cannot be checked.
var km2Required = []struct {
	entity string
	fields []string
}{
	{"person", []string{"name", "email"}},
	{"project", []string{"key", "name"}},
	{"issue", []string{"identifier", "number", "project_key", "title", "description", "status", "status_category", "priority", "labels",
		"due_date", "parent_key"}},
	{"comment", []string{"body", "issue_key", "project_key", "author_name", "mentions"}},
}

// km2StatusCategories are the exact options status_category must have.
var km2StatusCategories = []string{model.StatusCanceled, model.StatusDone, model.StatusInProgress, model.StatusToDo}

type km2Field struct {
	Slug            string   `json:"slug"`
	FieldType       string   `json:"field_type"`
	PicklistOptions []string `json:"picklist_options"`
	IsUnique        bool     `json:"is_unique"`
}

// Check lists what the organisation lacks of the tracker entities: missing entities and fields, and a status_category
// that is not a picklist of exactly to_do, in_progress, done and canceled. A non-unique issue identifier is a warning
// (two writers could make the same key). Relationships are not listed by KM2's catalog API, so they are not checked.
func (k *km2Tracker) Check(ctx context.Context) (CheckResult, error) {
	res := CheckResult{Problems: []string{}, Warnings: []string{}}
	for _, want := range km2Required {
		slug := k.s.Entity(want.entity)
		var def struct {
			Fields []km2Field `json:"fields"`
		}
		err := k.call(ctx, http.MethodGet, km2Path+url.PathEscape(slug), nil, nil, &def)
		var te *Error
		if asError(err, &te) && te.Status == http.StatusNotFound {
			res.Problems = append(res.Problems, "there is no "+slug+" entity")
			continue
		}
		if err != nil {
			return CheckResult{}, err
		}
		have := make(map[string]km2Field, len(def.Fields))
		for _, f := range def.Fields {
			have[f.Slug] = f
		}
		fields := slices.Clone(want.fields)
		if want.entity == "issue" && !slices.Contains(km2BaseColumns, k.s.ActivityField) {
			fields = append(fields, k.s.ActivityField)
		}
		var missing []string
		for _, f := range fields {
			if _, ok := have[f]; !ok {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			res.Problems = append(res.Problems, fmt.Sprintf("%s lacks the fields %s", slug, strings.Join(missing, ", ")))
		}
		if want.entity == "issue" {
			k.checkIssueFields(slug, have, &res)
		}
	}
	return res, nil
}

func (k *km2Tracker) checkIssueFields(slug string, have map[string]km2Field, res *CheckResult) {
	if f, ok := have["status_category"]; ok {
		options := slices.Clone(f.PicklistOptions)
		slices.Sort(options)
		if f.FieldType != "picklist" || !slices.Equal(options, km2StatusCategories) {
			res.Problems = append(res.Problems, fmt.Sprintf("%s.status_category must be a picklist of exactly to_do, in_progress, done and canceled "+
				"(it is a %s field with the options %s)", slug, oneLine(f.FieldType, 40), oneLine(strings.Join(f.PicklistOptions, ", "), 200)))
		}
	}
	if f, ok := have["identifier"]; ok && !f.IsUnique {
		res.Warnings = append(res.Warnings, slug+".identifier is not unique, so two writers could make the same issue key; make it unique in KM2")
	}
}
