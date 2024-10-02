// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the desired interfaces.
var _ function.Function = &UniqueContactFunction{}

type UniqueContactModel struct {
	CSV              types.List   `tfsdk:"csv"`
	GroupByField     types.String `tfsdk:"group_by_field"`
	CodeField        types.String `tfsdk:"code_field"`
	DestinationField types.String `tfsdk:"destination_field"`
	Labels           types.List   `tfsdk:"label_fields"`
	Variables        types.List   `tfsdk:"variable_fields"`
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
		Description: "Merge contacts with duplicate names.",
		Parameters: []function.Parameter{
			function.ListParameter{
				Name: "csv",
				ElementType: types.MapType{
					ElemType: types.StringType,
				},
			},
			function.StringParameter{
				Name: "group_by_field",
			},
			function.StringParameter{
				Name: "code_field",
			},
			function.StringParameter{
				Name: "destination_field",
			},
			function.ListParameter{
				Name:        "label_fields",
				ElementType: types.StringType,
			},
			function.ListParameter{
				Name:        "variable_fields",
				ElementType: types.StringType,
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
	if err := req.Arguments.Get(ctx, &data.CSV, &data.GroupByField, &data.CodeField, &data.DestinationField, &data.Labels, &data.Variables); err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, err)

		return
	}

	var elements []map[string]string
	diag := data.CSV.ElementsAs(ctx, &elements, true)
	if diag.HasError() {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.FuncErrorFromDiags(ctx, diag))

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

			destinations []destination
			labels       []string
			variables    map[string]string
		}
	)

	seen := make(map[string]contact)
	for i, v := range elements {
		name := stripSpaces(v[data.GroupByField.ValueString()])
		if name == "" {
			tflog.Warn(ctx, "element has empty name", map[string]interface{}{"i": i, "name": name})

			continue
		}

		if _, ok := seen[name]; !ok {
			seen[name] = contact{
				mu:           &sync.Mutex{},
				destinations: make([]destination, 0),
				labels:       make([]string, 0),
				variables:    make(map[string]string),
			}
		}

		seen[name].mu.Lock()

		labels := make([]string, 0, len(labelFields))
		for _, field := range labelFields {
			labels = append(labels, v[field])
		}

		d := destination{
			code:        v[data.CodeField.ValueString()],
			destination: v[data.DestinationField.ValueString()],
		}

		c := contact{
			mu:           seen[name].mu,
			destinations: append(seen[name].destinations, d),
			labels:       append(seen[name].labels, labels...),
			variables:    seen[name].variables,
		}

		for _, field := range variableFields {
			if _, ok := c.variables[field]; ok {
				tflog.Warn(ctx, "variable already exists, overwriting", map[string]interface{}{"key": field})
			}

			c.variables[field] = v[field]
		}

		seen[name] = c
		seen[name].mu.Unlock()
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
			"destinations": types.SetValueMust(destinationSchema(), destinations),
			"labels":       types.ListValueMust(types.StringType, UniqueSliceElements(labels)),
			"variables":    types.MapValueMust(types.StringType, variables),
		})
	}

	// Set the result
	// "foo bar": {
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

func UniqueSliceElements[T comparable](inputSlice []T) []T {
	uniqueSlice := make([]T, 0, len(inputSlice))
	seen := make(map[T]bool, len(inputSlice))
	for _, element := range inputSlice {
		if !seen[element] {
			uniqueSlice = append(uniqueSlice, element)
			seen[element] = true
		}
	}

	return uniqueSlice
}
