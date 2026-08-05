terraform {
  required_providers {
    podman-quadlet = {
      source = "registry.opentofu.org/xiantongc612/podman-quadlet"
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

provider "podman-quadlet" {
  alias = "edge_1"
  host  = var.hosts.edge_1
  user  = var.user
}

provider "podman-quadlet" {
  alias = "edge_2"
  host  = var.hosts.edge_2
  user  = var.user
}

resource "podman-quadlet_container" "edge_1" {
  provider = podman-quadlet.edge_1

  metadata {
    name = "web"
  }

  spec {
    image = "docker.io/library/nginx:1.29"
  }
}

resource "podman-quadlet_container" "edge_2" {
  provider = podman-quadlet.edge_2

  metadata {
    name = "web"
  }

  spec {
    image = "docker.io/library/nginx:1.29"
  }
}
