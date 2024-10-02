// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

func TestUniqueContactFunction_Known(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_8_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
				locals {
					# We've included this inline to create a complete example, but in practice
				  	# this is more likely to be loaded from a file using the "file" function.
				  	csv_data = <<-CSV
						name,code,destination
						foo1,1,ami-54d2a63b
						foo1,1,ami-54d2a63c
						foo1,2,ami-54d2a63b
						bar1,m3.large,ami-54d2a63b
				  	CSV
				
				  	instances = csvdecode(local.csv_data)
				}

				output "test" {
					value = provider::webitel::unique_contact(local.instances, "name", "code", "destination", ["code", "destination"], ["name"])
				}
				`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownOutputValue("test", knownvalue.MapExact(map[string]knownvalue.Check{
						"bar1": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"labels":    knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("m3.large"), knownvalue.StringExact("ami-54d2a63b")}),
							"variables": knownvalue.MapExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("bar1")}),
							"destinations": knownvalue.ListExact([]knownvalue.Check{
								knownvalue.ObjectExact(map[string]knownvalue.Check{
									"code":        knownvalue.StringExact("m3.large"),
									"destination": knownvalue.StringExact("ami-54d2a63b"),
								}),
							}),
						}),
						"foo1": knownvalue.ObjectExact(map[string]knownvalue.Check{
							"labels": knownvalue.ListExact([]knownvalue.Check{
								knownvalue.StringExact("1"), knownvalue.StringExact("ami-54d2a63b"),
								knownvalue.StringExact("ami-54d2a63c"), knownvalue.StringExact("2"),
							}),
							"variables": knownvalue.MapExact(map[string]knownvalue.Check{"name": knownvalue.StringExact("foo1")}),
							"destinations": knownvalue.ListExact([]knownvalue.Check{
								knownvalue.ObjectExact(map[string]knownvalue.Check{
									"code":        knownvalue.StringExact("1"),
									"destination": knownvalue.StringExact("ami-54d2a63b"),
								}),
								knownvalue.ObjectExact(map[string]knownvalue.Check{
									"code":        knownvalue.StringExact("1"),
									"destination": knownvalue.StringExact("ami-54d2a63c"),
								}),
								// FIXME: Make unique destination list based on `code` and `destination`
								// knownvalue.ObjectExact(map[string]knownvalue.Check{
								// 	"code":        knownvalue.StringExact("2"),
								// 	"destination": knownvalue.StringExact("ami-54d2a63b"),
								// }),
							}),
						}),
					})),
				},
			},
		},
	})
}

func TestUniqueContactFunction_Null(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_8_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
				output "test" {
					value = provider::webitel::unique_contact(null, null, null, null, null, null)
				}
				`,
				// The parameter does not enable AllowNullValue
				ExpectError: regexp.MustCompile(`argument must not be null`),
			},
		},
	})
}
