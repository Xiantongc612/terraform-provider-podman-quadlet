# podlet-provider

A community OpenTofu and Terraform provider for managing rootless Podman Quadlet
resources on remote machines.

## Status

The repository currently contains the development-tooling foundation. The Go
provider and its resources are planned but not yet implemented. See
[`PLAN.md`](PLAN.md) for the approved contracts and remaining work.

The initial provider will:

- Connect to one remote machine per provider instance over SSH.
- Use provider aliases to address multiple machines.
- Manage rootless user Quadlets and their generated systemd services.
- Provide typed `podlet_container`, `podlet_network`, and `podlet_volume`
  resources.
- Use Kubernetes-inspired `metadata` and `spec` blocks.

The intended configuration shape is:

```hcl
provider "podlet" {
  host             = "edge-1.example.com"
  user             = "containers"
  private_key_path = "~/.ssh/id_ed25519"
  known_hosts_path = "~/.ssh/known_hosts"
}

resource "podlet_network" "service" {
  metadata {
    name = "service"
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
    description = "Example HTTP service"
  }

  spec {
    image = "ghcr.io/example/service:1.0.0"

    port {
      host_port      = 8080
      container_port = 8080
      protocol       = "tcp"
    }

    mount {
      source = podlet_volume.data.reference
      target = "/var/lib/service"
    }

    networks = [podlet_network.service.reference]

    service {
      restart = "on-failure"
    }
  }
}
```

This example documents the approved API direction and will not work until the
provider implementation milestone is complete.

## Development

Install [Devbox](https://www.jetify.com/devbox/docs/installing_devbox/), then run:

```shell
devbox run init
devbox run check
```

At present, `check` runs the repository's gitleaks scan. Additional Go checks
will be added with the provider foundation.
