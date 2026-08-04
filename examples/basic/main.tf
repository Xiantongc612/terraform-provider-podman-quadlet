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

provider "podlet" {
  host = var.host
  user = var.user
}

resource "podlet_network" "service" {
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

resource "podlet_volume" "data" {
  metadata {
    name = "service-data"
  }

  spec {
    driver = "local"
  }
}

resource "podlet_container" "service" {
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
      source    = podlet_volume.data.reference
      target    = "/usr/share/nginx/html"
      read_only = true
    }

    networks = [podlet_network.service.reference]

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
