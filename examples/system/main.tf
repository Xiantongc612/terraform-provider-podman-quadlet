terraform {
  required_providers {
    podman-quadlet = {
      source = "registry.opentofu.org/xiantongc612/podman-quadlet"
    }
  }
}

variable "host" {
  type = string
}

variable "user" {
  type = string
}

variable "become_password" {
  type      = string
  default   = ""
  sensitive = true
}

provider "podman-quadlet" {
  host            = var.host
  user            = var.user
  mode            = "system"
  sudo            = true
  become_password = var.become_password
}

resource "podman-quadlet_network" "service" {
  metadata {
    name = "service"
  }

  spec {
    driver = "bridge"
  }
}

resource "podman-quadlet_container" "service" {
  metadata {
    name        = "service"
    description = "System-wide web service"
  }

  spec {
    image = "docker.io/library/nginx:1.29"

    port {
      host_port      = 8080
      container_port = 80
    }

    networks = [podman-quadlet_network.service.reference]
  }
}
