terraform {
  required_providers {
    podlet = {
      source = "registry.terraform.io/xiantongc612/podlet"
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

provider "podlet" {
  host            = var.host
  user            = var.user
  mode            = "system"
  sudo            = true
  become_password = var.become_password
}

resource "podlet_network" "service" {
  metadata {
    name = "service"
  }

  spec {
    driver = "bridge"
  }
}

resource "podlet_container" "service" {
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

    networks = [podlet_network.service.reference]
  }
}
