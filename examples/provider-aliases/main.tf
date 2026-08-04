terraform {
  required_providers {
    podlet = {
      source = "registry.terraform.io/xiantongc612/podlet"
    }
  }
}

variable "hosts" {
  type = object({
    edge_1 = string
    edge_2 = string
  })
}

variable "user" {
  type = string
}

provider "podlet" {
  alias = "edge_1"
  host  = var.hosts.edge_1
  user  = var.user
}

provider "podlet" {
  alias = "edge_2"
  host  = var.hosts.edge_2
  user  = var.user
}

resource "podlet_container" "edge_1" {
  provider = podlet.edge_1

  metadata {
    name = "web"
  }

  spec {
    image = "docker.io/library/nginx:1.29"
  }
}

resource "podlet_container" "edge_2" {
  provider = podlet.edge_2

  metadata {
    name = "web"
  }

  spec {
    image = "docker.io/library/nginx:1.29"
  }
}
