// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var destinationRegexp = regexp.MustCompile(`\D`)

// Ensure the implementation satisfies the desired interfaces.
var _ function.Function = &UniqueContactFunction{}

type UniqueContactModel struct {
	NameField        types.String `tfsdk:"name_field"`
	CodeField        types.String `tfsdk:"code_field"`
	DestinationField types.String `tfsdk:"destination_field"`
	Labels           types.List   `tfsdk:"label_fields"`
	Variables        types.List   `tfsdk:"variable_fields"`
	GroupByFields    types.List   `tfsdk:"group_by_fields"`
}

type UniqueContactFunction struct{}

func NewUniqueContactFunction() function.Function {
	return &UniqueContactFunction{}
}

func (f *UniqueContactFunction) Metadata(ctx context.Context, req function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "unique_contact"
}

func (f *UniqueContactFunction) Definition(ctx context.Context, req function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary:     "Compute contacts data without duplicates",
		Description: "Merge contacts grouped by group_by_fields.",
		Parameters: []function.Parameter{
			function.ListParameter{
				Name: "csv",
				ElementType: types.MapType{
					ElemType: types.StringType,
				},
			},
			function.ObjectParameter{
				Name: "csv_mapping",
				AttributeTypes: map[string]attr.Type{
					"name_field":        types.StringType,
					"code_field":        types.StringType,
					"destination_field": types.StringType,
					"label_fields": types.ListType{
						ElemType: types.StringType,
					},
					"variable_fields": types.ListType{
						ElemType: types.StringType,
					},
					"group_by_fields": types.ListType{
						ElemType: types.StringType,
					},
				},
			},
		},
		Return: function.MapReturn{
			ElementType: returnSchema(),
		},
	}
}

func destinationSchema() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"code":        types.StringType,
			"destination": types.StringType,
		},
	}
}

func returnSchema() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"name": types.StringType,
			"labels": types.ListType{
				ElemType: types.StringType,
			},
			"variables": types.MapType{
				ElemType: types.StringType,
			},
			"destinations": types.SetType{
				ElemType: destinationSchema(),
			},
		},
	}
}

func (f *UniqueContactFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	schema := returnSchema()

	// Read Terraform argument data into the variables
	var data UniqueContactModel
	var csv types.List
	if err := req.Arguments.Get(ctx, &csv, &data); err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, err)

		return
	}

	// TODO: add bounds check to access map element
	var elements []map[string]string
	diag := csv.ElementsAs(ctx, &elements, true)
	if diag.HasError() {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.FuncErrorFromDiags(ctx, diag))

		return
	}

	groupByFields, err := listToLabels(data.GroupByFields)
	if err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.NewFuncError(err.Error()))

		return
	}

	labelFields, err := listToLabels(data.Labels)
	if err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.NewFuncError(err.Error()))

		return
	}

	variableFields, err := listToLabels(data.Variables)
	if err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.NewFuncError(err.Error()))

		return
	}

	type (
		destination struct {
			code        string
			destination string
		}

		contact struct {
			mu *sync.Mutex

			name         string
			destinations []destination
			labels       []string
			variables    map[string]string
		}
	)

	seen := make(map[string]contact)
	for i, v := range elements {
		name := stripSpaces(v[data.NameField.ValueString()])
		if name == "" {
			tflog.Warn(ctx, "element has empty name", map[string]interface{}{"i": i, "name": name})

			continue
		}

		keyFields := make([]string, 0, len(groupByFields))
		for _, k := range groupByFields {
			if v[k] != "" {
				keyFields = append(keyFields, v[k])
			}
		}

		key := name
		if len(keyFields) != 0 {
			key = strings.Join(keyFields, "-")
		}

		if _, ok := seen[key]; !ok {
			seen[key] = contact{
				mu:           &sync.Mutex{},
				name:         name,
				destinations: make([]destination, 0),
				labels:       make([]string, 0),
				variables:    make(map[string]string),
			}
		}

		seen[key].mu.Lock()

		labels := make([]string, 0, len(labelFields))
		for _, field := range labelFields {
			labels = append(labels, v[field])
		}

		d := destination{
			code:        v[data.CodeField.ValueString()],
			destination: "+" + destinationRegexp.ReplaceAllString(v[data.DestinationField.ValueString()], ""),
		}

		c := contact{
			mu:           seen[key].mu,
			name:         seen[key].name,
			destinations: append(seen[key].destinations, d),
			labels:       append(seen[key].labels, labels...),
			variables:    seen[key].variables,
		}

		for _, field := range variableFields {
			if _, ok := c.variables[field]; ok {
				tflog.Warn(ctx, "variable already exists, overwriting", map[string]interface{}{"key": field})
			}

			c.variables[field] = v[field]
		}

		seen[key] = c
		seen[key].mu.Unlock()
	}

	contacts := make(map[string]attr.Value, len(seen))
	for n, c := range seen {
		labels := make([]attr.Value, 0, len(c.labels))
		seenLabels := make(map[string]bool, len(c.labels))
		for _, label := range c.labels {
			if !seenLabels[label] {
				labels = append(labels, types.StringValue(label))
			}

			seenLabels[label] = true
		}

		variables := make(map[string]attr.Value, len(c.variables))
		for k, variable := range c.variables {
			variables[k] = types.StringValue(variable)
		}

		destinations := make([]attr.Value, 0, len(c.destinations))

		// TODO: Make unique destination list based on `code` and `destination`
		seenDestination := make(map[string]bool, len(c.destinations))
		for _, dest := range c.destinations {
			if !seenDestination[dest.destination] {
				obj := types.ObjectValueMust(destinationSchema().AttrTypes, map[string]attr.Value{
					"code":        types.StringValue(dest.code),
					"destination": types.StringValue(dest.destination),
				})

				destinations = append(destinations, obj)
			}

			seenDestination[dest.destination] = true
		}

		contacts[n] = types.ObjectValueMust(schema.AttrTypes, map[string]attr.Value{
			"name":         types.StringValue(c.name),
			"destinations": types.SetValueMust(destinationSchema(), destinations),
			"labels":       types.ListValueMust(types.StringType, labels),
			"variables":    types.MapValueMust(types.StringType, variables),
		})
	}

	// Set the result
	// "foo bar-bar": {
	// 		"name": "foo bar",
	// 		"labels": ["one", "foo", "bar"],
	// 		"variables": [
	// 			{
	// 				"key": "foo",
	// 				"value": "bar"
	// 			}
	// 		],
	// 		"destinations": [
	// 			{
	// 				"code": "1",
	// 				"destination": "123"
	// 			}
	// 		]
	// }
	// m := map[string]map[string]any{}
	resp.Error = function.ConcatFuncErrors(resp.Error, resp.Result.Set(ctx, types.MapValueMust(returnSchema(), contacts)))
}
