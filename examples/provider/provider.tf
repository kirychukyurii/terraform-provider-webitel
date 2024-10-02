provider "webitel" {
  endpoint = "https://webitel.example.com/api"
  token    = "token"

  insecure = false

  retry {
    attempts = 3
    delay_ms = 2000
  }
}
