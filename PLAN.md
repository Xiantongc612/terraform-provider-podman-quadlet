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

## Implemented: SSH password authentication

A `password` provider attribute enables password-based SSH logins, alongside
the existing agent and unencrypted-key methods. Authentication is exercised by
an in-process SSH server test in the remote package.

## Local Demo

`devbox run dev` builds the provider and runs a small hello-world demo against a
rootless Podman host. It targets `127.0.0.1` with the current user by default.
Configuration is read from a gitignored `.env.op` file (template in
`.env.op.example`); `PODLET_DEMO_HOST`, `PODLET_DEMO_USER`, `PODLET_DEMO_PORT`,
`PODLET_DEMO_KEY_PATH`, `PODLET_DEMO_PASSWORD`, and `PODLET_DEMO_KEEP` are
supported. The demo requires Podman and user systemd lingering
(`sudo loginctl enable-linger $USER`) on the host. It applies the container in
`examples/demo`, prints the served greeting, then destroys everything on exit
unless `PODLET_DEMO_KEEP=1` is set.

## Registry Publishing Plan (approved)

Decisions: namespace `xiantongc612`, first release `v0.1.0`, GoReleaser for
release tooling, six platform targets (`linux`/`darwin`/`windows` ×
`amd64`/`arm64`), and a real-host apply/destroy before tagging.

0. **Real-host validation (gate).** Run full create/update/drift/import/delete
   for user mode (rootless host) and system mode (root login and NOPASSWD
   sudo). Record the supported Podman version range.
1. **Rename and re-address.** Rename the repository to
   `terraform-provider-podlet`, update the Go module path, the provider address
   to `registry.opentofu.org/xiantongc612/podlet`, and all docs and examples.
2. **GPG signing key.** Generate a release signing key, store the private key
   in repository Actions secrets, and keep the ASCII-armored public key for the
   registry submission.
3. **Release automation.** Add `terraform-registry-manifest.json`,
   `.goreleaser.yml`, and a `v*` tag workflow producing per-platform zips,
   `SHA256SUMS`, and a GPG-signed `SHA256SUMS.sig`.
4. **Tag and push** `v0.1.0`; confirm the release assets.
5. **Registry submission.** Submit the provider and the signing key through the
   `opentofu/registry` issue forms (web UI only).
6. **Verify from the registry.** `tofu init` and apply using
   `registry.opentofu.org/xiantongc612/podlet`.

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

- Encrypted private keys and jump hosts.
- Quadlet pod, image, build, kube, and artifact resources.
- A generic manifest escape hatch for unsupported Quadlet directives.
- Secret delivery that does not persist values in OpenTofu state.
- Fleet-wide orchestration beyond Terraform provider aliases.
- Automatic Podman installation or remote host provisioning.
