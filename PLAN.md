# Implementation Plan

The repository currently provides a Devbox environment with OpenTofu, gitleaks,
and pre-commit. The provider implementation has not started.

## Approved Contracts

- Use Go and the Terraform Plugin Framework, while supporting OpenTofu as the
  primary CLI.
- Manage Podman Quadlet files on remote machines over SSH.
- Configure one remote machine per provider instance. Use provider aliases to
  manage multiple machines.
- Support rootless Podman and user systemd units in the initial release.
- Expose typed, Kubernetes-inspired `metadata` and `spec` blocks rather than a
  generic manifest resource.
- Initially provide `podlet_container`, `podlet_network`, and `podlet_volume`
  resources.
- Treat each resource as the owner of one Quadlet file and its generated systemd
  unit.
- Verify SSH host keys by default and support SSH agent or private-key
  authentication.
- Keep `metadata.name` immutable and use it as the Quadlet file name.
- Render deterministic Quadlet content so configuration drift can be detected
  and reconciled.
- Do not silently delete a remote file that is not marked as provider-managed.

## Provider Foundation

- Add Go, golangci-lint, and provider documentation tooling to Devbox with
  commands for formatting, linting, testing, building, and aggregate checks.
- Initialize the Go module and provider executable.
- Implement provider configuration for host, port, user, private key, SSH agent,
  known-hosts file, connection timeout, and Quadlet directory overrides.
- Implement SSH command execution and atomic SFTP file operations behind a
  testable remote-client interface.
- Add secure defaults, configuration validation, contextual diagnostics, and
  cancellation support.

## Quadlet Model

- Implement shared metadata fields for name, description, and Podman labels.
- Implement a computed status model containing the remote path, systemd unit,
  content checksum, load state, active state, and substate.
- Implement deterministic rendering and parsing for the supported `.container`,
  `.network`, and `.volume` files.
- Add a provider ownership marker and format version to generated files.
- Validate names, ports, protocols, durations, pull policies, restart policies,
  mount paths, and network settings before remote operations.

## Resources

- Implement `podlet_network` with bridge-driver settings, addressing, DNS,
  internal-network behavior, labels, and driver options.
- Implement `podlet_volume` with local-driver settings, device, filesystem type,
  mount options, copy behavior, and labels.
- Implement `podlet_container` with image and pull policy, command and arguments,
  environment values and files, ports, mounts, networks, user, working directory,
  health checks, labels, and systemd restart behavior.
- Expose network and volume Quadlet references as computed values so Terraform
  expressions establish resource dependencies.
- Implement create, read, update, delete, and import behavior for every resource.
- Reload user systemd after file changes and enable, start, restart, stop, or
  disable generated units as required by the resource lifecycle.
- Preserve failed or inactive services in state while reporting their current
  systemd status.

## Verification And Documentation

- Add unit tests for schemas, validation, rendering, parsing, shell argument
  handling, drift detection, and lifecycle behavior with a fake remote client.
- Add opt-in integration tests against a rootless Podman host.
- Add OpenTofu examples for a single host, provider aliases, and a container using
  managed network and volume resources.
- Document local provider installation, authentication, imports, drift behavior,
  troubleshooting, and the fact that sensitive Terraform values remain in state.
- Add CI for formatting, linting, unit tests, builds, secret scanning, and
  generated documentation checks.

## Deferred Decisions

- Rootful Podman and sudo support.
- Password-based SSH authentication and jump hosts.
- Quadlet pod, image, build, kube, and artifact resources.
- A generic manifest escape hatch for unsupported Quadlet directives.
- Secret delivery that does not persist values in OpenTofu state.
- Fleet-wide orchestration beyond Terraform provider aliases.
- Automatic Podman installation or remote host provisioning.
