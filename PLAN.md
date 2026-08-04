# Implementation Plan

The initial provider implementation is complete. It exposes typed container,
network, and volume resources; manages rootless and rootful Quadlets over SSH;
reconciles their user or system systemd units; supports imports and drift
refresh; and includes Devbox tooling, generated schema documentation, examples,
unit tests, opt-in SSH integration coverage, and CI.

## Implemented: dual-mode (rootless / rootful) support

Provider attributes `mode` (`"user"` / `"system"`) and `sudo` enclose all
mode-dependent behavior:

- Quadlet directory defaults: `.config/containers/systemd` (user) /
  `/etc/containers/systemd` (system).
- systemctl prefix: `systemctl --user` (user) / `sudo systemctl` (system sudo) /
  `systemctl` (system root login).
- WantedBy target: `default.target` (user) / `multi-user.target` (system).
- File-write elevation: direct SFTP for root logins; staged `sudo install` for
  NOPASSWD sudo. Both root SSH login and NOPASSWD-sudo are first-class.
- The `Client` interface stays unchanged; elevation branches inside SSHClient
  and the provider lifecycle layer.

## Before First Release

- Run `devbox run integration` against a prepared rootless Podman host and verify
  its SSH, SFTP, Podman, and user systemd prerequisites.
- Run the extended SSH integration against a prepared system-mode host with root
  login or NOPASSWD sudo to verify elevated file operations.
- Add end-to-end acceptance coverage for create, update, drift refresh, import,
  and delete on those hosts.
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
