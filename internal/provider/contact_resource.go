// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	webitel "github.com/kirychukyurii/webitel-openapi-client-go/client"
	"github.com/kirychukyurii/webitel-openapi-client-go/client/contacts"
	"github.com/kirychukyurii/webitel-openapi-client-go/models"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ContactResource{}
var _ resource.ResourceWithImportState = &ContactResource{}

var contactDefaultFields = []string{"id", "etag", "name", "about", "labels", "variables", "phones"}

type ContactResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ETag      types.String `tfsdk:"etag"`
	Name      types.String `tfsdk:"name"`
	About     types.String `tfsdk:"about"`
	Labels    types.List   `tfsdk:"labels"`
	Variables types.Map    `tfsdk:"variables"`
	Phones    types.Set    `tfsdk:"phones"`
}

type ContactResourcePhones struct {
	Code        types.String `tfsdk:"code"`
	Destination types.String `tfsdk:"destination"`
}

// ContactResource defines the resource implementation.
type ContactResource struct {
	client *webitel.WebitelAPI
}

func NewContactResource() resource.Resource {
	return &ContactResource{}
}

func (r *ContactResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact"
}

func (r *ContactResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Contact principal resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique ID of the association. Never changes.",
			},
			"etag": schema.StringAttribute{
				Computed: true,
				Description: "Unique ID of the latest version of the update. " +
					"This ID changes after any update to the underlying value(s).",
			},
			"name": schema.StringAttribute{
				Required: true,
				Description: "End-User's full name in displayable form including all name parts, " +
					"possibly including titles and suffixes, ordered according to the End-User's locale and preferences.",
			},
			"about": schema.StringAttribute{
				Optional:    true,
				Description: "BIO. Short description about the Contact person. Multi-lined text.",
			},
			"labels": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "A Contact's associated Tags. Keep in mind, hashtags are not case-sensitive, " +
					"but adding capital letters does make them easier to read: #MakeAWish vs. #makeawish.",
			},
			"variables": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "The Contact's variables. " +
					"Arbitrary data that is populated by users or clients.",
			},
			"phones": schema.SetNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"code": schema.StringAttribute{
							Optional: true,
							Description: "The type of the phone number. Reference on CommunicationType dictionary. " +
								"Used for outbound routing while dialup a phone number.",
						},
						"destination": schema.StringAttribute{
							Optional:    true,
							Description: "The phone number.",
						},
					},
				},
				Description: "The Contact's phone numbers.",
			},
		},
	}
}

func (r *ContactResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*webitel.WebitelAPI)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *webitel.WebitelAPI, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *ContactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Read Terraform plan data into the model
	var data ContactResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &models.WebitelContactsInputContact{
		About: data.About.ValueString(),
		Name: &models.WebitelContactsInputName{
			CommonName: stripSpaces(data.Name.ValueString()),
		},
	}

	if !data.Labels.IsNull() {
		labels, err := listToLabels(data.Labels)
		if err != nil {
			resp.Diagnostics.AddError("Unable to make contact labels structure", err.Error())

			return
		}

		input.Labels = make([]*models.WebitelContactsInputLabel, 0, len(labels))
		for _, l := range labels {
			label := &models.WebitelContactsInputLabel{
				Label: l,
			}

			input.Labels = append(input.Labels, label)
		}
	}

	if !data.Variables.IsNull() {
		variables, err := mapToVariables(data.Variables)
		if err != nil {
			resp.Diagnostics.AddError("Unable to make contact variables structure", err.Error())

			return
		}

		input.Variables = make([]*models.WebitelContactsInputVariable, 0, len(variables))
		for k, v := range variables {
			variable := &models.WebitelContactsInputVariable{
				Key:   &k,
				Value: v,
			}

			input.Variables = append(input.Variables, variable)
		}
	}

	if !data.Phones.IsNull() {
		var phones []ContactResourcePhones
		data.Phones.ElementsAs(ctx, &phones, true)
		input.Phones = make([]*models.WebitelContactsInputPhoneNumber, 0, len(phones))
		for _, phone := range phones {
			obj := &models.WebitelContactsInputPhoneNumber{
				Type: &models.WebitelContactsLookup{
					ID: phone.Code.ValueString(),
				},
				Number: phone.Destination.ValueStringPointer(),
			}

			input.Phones = append(input.Phones, obj)
		}
	}

	httpResp, err := r.client.Contacts.ContactsCreateContact(&contacts.ContactsCreateContactParams{Context: ctx, Input: input, Fields: contactDefaultFields})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			"An unexpected error occurred while attempting to create the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Error: "+err.Error(),
		)

		return
	}

	// Return error if the HTTP status code is not 200 OK
	if !httpResp.IsCode(http.StatusOK) {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			"An unexpected error occurred while attempting to create the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Status: "+fmt.Sprintf("%d", httpResp.Code()),
		)

		return
	}

	contact := httpResp.GetPayload()

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, contactToTF(contact))...)
}

func (r *ContactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Read Terraform prior state data into the model
	var data ContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	size := int32(10)
	q := stripSpaces(data.Name.ValueString())
	input := &contacts.ContactsSearchContactsParams{
		Context: ctx,
		Fields:  contactDefaultFields,
		Q:       &q,
		Qin:     []string{"name"},
		Size:    &size,
	}

	httpResp, err := r.client.Contacts.ContactsSearchContacts(input)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Refresh Resource",
			"An unexpected error occurred while attempting to refresh resource state. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Error: "+err.Error(),
		)

		return
	}

	payload := httpResp.GetPayload()

	// Treat HTTP 404 Not Found status as a signal to recreate resource
	// and return early
	if httpResp.IsCode(http.StatusNotFound) || len(payload.Data) == 0 {
		resp.State.RemoveResource(ctx)

		return
	}

	var contact *models.WebitelContactsContact
	for _, v := range payload.Data {
		if v.Name.CommonName == q {
			contact = v

			break
		}
	}

	if contact == nil {
		resp.State.RemoveResource(ctx)

		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, contactToTF(contact))...)
}

func (r *ContactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Read Terraform plan && state data into the model
	// to detect only value changes
	var plan, state ContactResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// TODO: Compare attributes value between plan and
	//  	prior state to update only changed values
	// if !plan.ETag.Equal(state.ETag) {
	// 	resp.Diagnostics.AddError(
	// 		"Unable to Update Resource",
	// 		"An error occurred while attempting to update the resource. "+
	// 			"Etag was changed.\n\n",
	// 	)
	//
	// 	resp.State.RemoveResource(ctx)
	//
	// 	return
	// }

	input := &models.ContactsUpdateContactParamsBody{
		About: plan.About.ValueString(),
		Name: &models.WebitelContactsInputName{
			CommonName: stripSpaces(plan.Name.ValueString()),
		},
	}

	if !plan.Labels.IsNull() {
		labels, err := listToLabels(plan.Labels)
		if err != nil {
			resp.Diagnostics.AddError("Unable to make contact labels structure", err.Error())

			return
		}

		input.Labels = make([]*models.WebitelContactsInputLabel, 0, len(labels))
		for _, l := range labels {
			label := &models.WebitelContactsInputLabel{
				Label: l,
			}

			input.Labels = append(input.Labels, label)
		}
	}

	if !plan.Variables.IsNull() {
		variables, err := mapToVariables(plan.Variables)
		if err != nil {
			resp.Diagnostics.AddError("Unable to make contact variables structure", err.Error())

			return
		}

		input.Variables = make([]*models.WebitelContactsInputVariable, 0, len(variables))
		for k, v := range variables {
			variable := &models.WebitelContactsInputVariable{
				Key:   &k,
				Value: v,
			}

			input.Variables = append(input.Variables, variable)
		}
	}

	if !plan.Phones.IsNull() {
		var phones []ContactResourcePhones
		plan.Phones.ElementsAs(ctx, &phones, true)
		input.Phones = make([]*models.WebitelContactsInputPhoneNumber, 0, len(phones))
		for _, phone := range phones {
			obj := &models.WebitelContactsInputPhoneNumber{
				Type: &models.WebitelContactsLookup{
					ID: phone.Code.ValueString(),
				},
				Number: phone.Code.ValueStringPointer(),
			}

			input.Phones = append(input.Phones, obj)
		}
	}

	params := &contacts.ContactsUpdateContactParams{
		Context: ctx,
		Etag:    plan.ETag.ValueString(),
		Input:   input,
		Fields:  contactDefaultFields,
	}

	httpResp, err := r.client.Contacts.ContactsUpdateContact(params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Update Resource",
			"An unexpected error occurred while attempting to update the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Error: "+err.Error(),
		)

		return
	}

	// Return error if the HTTP status code is not 200 OK
	if httpResp.IsCode(http.StatusOK) {
		resp.Diagnostics.AddError(
			"Unable to Update Resource",
			"An unexpected error occurred while attempting to update the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Status: "+fmt.Sprintf("%d", httpResp.Code()),
		)

		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, contactToTF(httpResp.GetPayload()))...)
}

func (r *ContactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Read Terraform prior state data into the model
	var data ContactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &contacts.ContactsDeleteContactParams{
		Context: ctx,
		Etag:    data.ETag.ValueString(),
	}

	httpResp, err := r.client.Contacts.ContactsDeleteContact(input)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			"An unexpected error occurred while attempting to delete the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Error: "+err.Error(),
		)

		return
	}

	// Return error if the HTTP status code is not 200 OK or 404 Not Found
	if httpResp.IsCode(http.StatusNotFound) && httpResp.IsCode(http.StatusOK) {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			"An unexpected error occurred while attempting to delete the resource. "+
				"Please retry the operation or report this issue to the provider developers.\n\n"+
				"HTTP Status: "+fmt.Sprintf("%d", httpResp.Code()),
		)

		return
	}
}

func (r *ContactResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func contactToTF(in *models.WebitelContactsContact) *ContactResourceModel {
	out := &ContactResourceModel{
		ID:   types.StringValue(in.ID),
		ETag: types.StringValue(in.Etag),
		Name: types.StringValue(in.Name.CommonName),
	}

	if in.About != "" {
		out.About = types.StringValue(in.About)
	}

	if in.Labels != nil {
		labels := make([]attr.Value, 0, len(in.Labels.Data))
		for _, v := range in.Labels.Data {
			labels = append(labels, types.StringValue(v.Label))
		}

		out.Labels = types.ListValueMust(types.StringType, labels)
	}

	if in.Variables != nil {
		variables := make(map[string]attr.Value, len(in.Variables.Data))
		for _, v := range in.Variables.Data {
			variables[v.Key] = types.StringValue(fmt.Sprintf("%v", v.Value))
		}

		out.Variables = types.MapValueMust(types.StringType, variables)
	}

	if in.Phones != nil {
		phones := make([]attr.Value, 0, len(in.Phones.Data))
		for _, v := range in.Phones.Data {
			obj := types.ObjectValueMust(destinationSchema().AttributeTypes(), map[string]attr.Value{
				"code":        types.StringValue(v.Type.ID),
				"destination": types.StringValue(v.Number),
			})

			phones = append(phones, obj)
		}

		out.Phones = types.SetValueMust(types.ObjectType{AttrTypes: destinationSchema().AttributeTypes()}, phones)
	}

	return out
}

func mapToVariables(m basetypes.MapValue) (map[string]string, error) {
	variables := map[string]string{}
	for k, v := range m.Elements() {
		if vString, ok := v.(types.String); ok {
			variables[k] = vString.ValueString()
		} else {
			return nil, fmt.Errorf("invalid variable value for %s: %v", k, v)
		}
	}

	return variables, nil
}

func listToLabels(l basetypes.ListValue) ([]string, error) {
	var labels []string

	for _, v := range l.Elements() {
		if vString, ok := v.(types.String); ok {
			labels = append(labels, vString.ValueString())
		} else {
			return nil, fmt.Errorf("invalid label value for %v", v)
		}
	}

	return labels, nil
}

func stripSpaces(in string) string {
	return strings.Join(strings.Fields(in), " ")
}
