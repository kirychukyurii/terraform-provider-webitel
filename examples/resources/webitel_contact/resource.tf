resource "webitel_contact" "example" {
  name   = "foo"
  about  = "about foo bar"
  labels = ["label-foo", "label-bar"]

  variables = [
    {
      key   = "foo-key"
      value = "foo-value"
    }
  ]

  phones = [
    {
      code        = "1"
      destination = "123"
    }
  ]
}

output "example" {
  value = webitel_contact.example.id
}