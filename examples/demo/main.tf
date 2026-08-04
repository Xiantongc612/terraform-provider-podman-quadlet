terraform {
  required_providers {
    podlet = {
      source = "registry.terraform.io/xiantongc612/podlet"
    }
  }
}

variable "host" {
  type    = string
  default = "127.0.0.1"
}

variable "user" {
  type = string
}

variable "private_key_path" {
  type    = string
  default = ""
}

variable "password" {
  type      = string
  default   = ""
  sensitive = true
}

provider "podlet" {
  host             = var.host
  user             = var.user
  private_key_path = var.private_key_path
  password         = var.password
}

resource "podlet_container" "hello" {
  metadata {
    name        = "hello"
    description = "Hello from podlet"
  }

  spec {
    image = "docker.io/library/nginx:1.29"

    port {
      host_ip        = "127.0.0.1"
      host_port      = 18080
      container_port = 80
    }
  }
}
