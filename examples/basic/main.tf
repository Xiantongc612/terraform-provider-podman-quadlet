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

provider "podman-quadlet" {
  host = var.host
  user = var.user
}

resource "podman-quadlet_network" "service" {
  metadata {
    name = "service"
    labels = {
      application = "example"
    }
  }

  spec {
    driver = "bridge"
  }
}

resource "podman-quadlet_volume" "data" {
  metadata {
    name = "service-data"
  }

  spec {
    driver = "local"
  }
}

resource "podman-quadlet_container" "service" {
  metadata {
    name        = "service"
    description = "Example web service"
  }

  spec {
    image       = "docker.io/library/nginx:1.29"
    pull_policy = "newer"

    environment = {
      NGINX_ENTRYPOINT_QUIET_LOGS = "1"
    }

    port {
      host_port      = 8080
      container_port = 80
    }

    mount {
      source    = podman-quadlet_volume.data.reference
      target    = "/usr/share/nginx/html"
      read_only = true
    }

    networks = [podman-quadlet_network.service.reference]

    health_check {
      command  = ["curl", "--fail", "http://localhost/"]
      interval = "30s"
      timeout  = "5s"
      retries  = 3
    }

    service {
      restart     = "on-failure"
      restart_sec = "5s"
    }
  }
}
