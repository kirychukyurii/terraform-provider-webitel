locals {
  # We've included this inline to create a complete example, but in practice
  # this is more likely to be loaded from a file using the "file" function.
  csv_data = <<-CSV
    name,code,destination,foo_label,foo_variable,bar_variable
    foo1,1,123,local,foo,bar
    foo1,1,456,local,foo,bar
    foo1,2,789,other,foo,bar
    bar1,1,123,local,foo,bar
  CSV

  mapping = {
    name_field        = "name"
    code_field        = "code"
    destination_field = "destination"
    label_fields      = ["foo_label"]
    variable_fields   = ["foo_variable", "bar_variable"]
    group_by_fields   = ["name", "foo_variable"]
  }

  contacts = provider::webitel::unique_contact(csvdecode(csv_data), local.mapping)
}

resource "webitel_contact" "from_file" {
  for_each = local.contacts

  name      = each.value.name
  labels    = each.value.labels
  phones    = each.value.destinations
  variables = each.value.variables
}