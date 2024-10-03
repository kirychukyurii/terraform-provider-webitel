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

  contacts = csvdecode(local.csv_data)

  mapping = {
    name_field        = "name"
    code_field        = "code"
    destination_field = "destination"
    label_fields      = ["foo_label"]
    variable_fields   = ["foo_variable", "bar_variable"]
    group_by_fields   = ["name", "foo_variable"]
  }
}

output "example" {
  value = provider::webitel::unique_contact(local.contacts, local.mapping)
}

# Outputs:
#  + example = {
#      + bar1-foo = {
#          + destinations = [
#              + {
#                  + code        = "1"
#                  + destination = "+123"
#                },
#            ]
#          + labels       = [
#              + "local",
#            ]
#          + name         = "bar1"
#          + variables    = {
#              + bar_variable = "bar"
#              + foo_variable = "foo"
#            }
#        }
#      + foo1-foo = {
#          + destinations = [
#              + {
#                  + code        = "1"
#                  + destination = "+123"
#                },
#              + {
#                  + code        = "1"
#                  + destination = "+456"
#                },
#              + {
#                  + code        = "2"
#                  + destination = "+789"
#                },
#            ]
#          + labels       = [
#              + "local",
#              + "other",
#            ]
#          + name         = "foo1"
#          + variables    = {
#              + bar_variable = "bar"
#              + foo_variable = "foo"
#            }
#        }
#    }