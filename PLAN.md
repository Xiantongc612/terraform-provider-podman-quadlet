# Plan

The provider is implemented. It exposes typed container, network, and volume
resources, manages rootless user and rootful system Quadlets over SSH, and
reconciles their systemd units with ownership protection, drift refresh, and
import. SSH agent, private-key, and password authentication are supported,
including `become_password` sudo elevation. The repository, Go module, provider
type, and address were renamed to `podman-quadlet` on
`registry.opentofu.org/xiantongc612/podman-quadlet`.

CI runs `devbox run check` on every pull request and push to `main`. The active
work is the first registry release: the real-host validation gate, release
signing and automation, and the registry submission.

## Constraints

- The provider only rewrites or deletes files it generated; unmarked remote
  files are never replaced.
- The target host must already have Podman, Quadlet, and a working systemd
  session; the provider does not provision hosts or install Podman.
- Resource type names follow the hyphenated provider type (`podman-quadlet_*`);
  OpenTofu derives the provider type from the resource-type prefix.
- The OpenTofu Registry requires a `{owner}/terraform-provider-{name}`
  repository, a semver release, and GPG-signed checksums.
- Dependency updates merge only after the required `check` status passes;
  semver-patch updates auto-merge, everything else is reviewed.

## Dependency Automation Milestone

### Approved contracts

- Dependabot updates the Go module and GitHub Actions daily, with a bounded
  number of open pull requests.
- Auto-merge is limited to semver-patch updates, uses squash merges, and only
  applies to `dependabot[bot]` pull requests.
- `main` requires the `check` status check before merging; no review is
  required.

### Remaining work

None.

### Implemented scope

- `.github/dependabot.yml` enables daily Go module and GitHub Actions updates
  with a bounded number of open pull requests.
- The Dependabot auto-merge workflow squash-merges `dependabot[bot]` pull
  requests whose update type is semver-patch.
- `main` branch protection requires the `check` status check with strict
  up-to-date branches; no review is required.

## Registry Publishing Milestone

### Approved contracts

- Namespace `xiantongc612`, first release `v0.1.0`, GoReleaser for release
  tooling, six platform targets (`linux`/`darwin`/`windows` × `amd64`/`arm64`),
  and a real-host apply/destroy gate before tagging.
- The provider and signing key are submitted through the `opentofu/registry`
  issue forms (web UI only).

### Remaining work

- Run the real-host validation gate for user and system modes and record the
  supported Podman version range.
- Generate the release GPG signing key, store the private key in repository
  Actions secrets, and keep the armored public key for the submission.
- Add `terraform-registry-manifest.json`, `.goreleaser.yml`, and a `v*` release
  workflow producing per-platform zips, `SHA256SUMS`, and a GPG-signed
  `SHA256SUMS.sig`.
- Tag and push `v0.1.0` and confirm the release assets.
- Submit the provider and the signing key through the registry issue forms.
- Verify `tofu init` and apply using
  `registry.opentofu.org/xiantongc612/podman-quadlet`.

### Implemented scope

- The repository was renamed to `terraform-provider-podman-quadlet`; the Go
  module, provider type, address, resources, generated docs, and examples now
  use `podman-quadlet`.

## Long-Term Goals

- Encrypted private keys and jump hosts.
- Quadlet pod, image, build, kube, and artifact resources.
- A generic manifest escape hatch for unsupported Quadlet directives.

## Deferred Design Decisions

- Secret delivery that does not persist values in OpenTofu state.
- Fleet-wide orchestration beyond Terraform provider aliases.
- Automatic Podman installation or remote host provisioning.
