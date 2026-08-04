# Implementation Plan

The initial provider implementation is complete. It exposes typed container,
network, and volume resources; manages rootless Quadlets over SSH; reconciles
their user systemd units; supports imports and drift refresh; and includes
Devbox tooling, generated schema documentation, examples, unit tests, opt-in SSH
integration coverage, and CI.

Local checks and OpenTofu plans for both single-host and provider-alias examples
pass. A full apply has not been run against a real rootless Podman host in this
repository environment.

## Constraints

- Use Go and the Terraform Plugin Framework, with OpenTofu as the primary CLI.
- Configure one remote machine per provider instance and use provider aliases
  for multiple machines.
- Manage rootless Podman and user systemd units in the initial release.
- Expose typed, Kubernetes-inspired `metadata` and `spec` blocks.
- Treat each resource as the owner of one marked Quadlet file and its generated
  systemd unit.
- Verify SSH host keys by default.
- Keep `metadata.name` immutable and use it as the Quadlet file name.
- Render deterministic Quadlet content and refuse to overwrite or delete an
  unmarked remote file.
- Keep local checks non-deploying. Remote integration and deployment require an
  explicitly configured test host.

## Planned: dual-mode (rootless / rootful) support

Design approved. New provider attributes `mode` (`"user"` / `"system"`) and
`sudo` enclose all mode-dependent behavior:

- Quadlet directory defaults: `.config/containers/systemd` (user) /
  `/etc/containers/systemd` (system).
- systemctl prefix: `systemctl --user` (user) / `sudo systemctl` (system).
- WantedBy target: `default.target` (user) / `multi-user.target` (system).
- File-write elevation: direct SFTP for root logins; staged `sudo install`
  for NOPASSWD sudo. Both root SSH login and NOPASSWD-sudo are first-class.
- The `Client` interface stays unchanged; elevation branches inside
  SSHClient and the provider lifecycle layer.

Implementation phases: provider schema → remote elevation → lifecycle prefix /
install target → renderer call sites → tests → docs → examples → verification.

## Before First Release

- Run `devbox run integration` against a prepared rootless Podman host and verify
  its SSH, SFTP, Podman, and user systemd prerequisites.
- Add end-to-end acceptance coverage for create, update, drift refresh, import,
  and delete on that host.
- Establish the supported Podman version range and test the generated Quadlet
  directives against each supported release.
- Rename the repository to `terraform-provider-podlet` and update provider
  addresses, module paths, documentation, and examples.
- Add signed release artifacts, checksums, and OpenTofu Registry
  publishing automation.

## Deferred Decisions

- Password-based SSH authentication, encrypted private keys, and jump hosts.
- Quadlet pod, image, build, kube, and artifact resources.
- A generic manifest escape hatch for unsupported Quadlet directives.
- Secret delivery that does not persist values in OpenTofu state.
- Fleet-wide orchestration beyond Terraform provider aliases.
- Automatic Podman installation or remote host provisioning.
