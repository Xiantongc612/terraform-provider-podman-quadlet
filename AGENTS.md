# Agent Notes

## Toolchain

Go and OpenTofu are installed inside Devbox, not on the host PATH. Run toolchain
commands through Devbox, for example:

```shell
devbox run -- go test ./...
devbox run -- tofu plan
```

The `devbox.json` scripts (`devbox run check`, `devbox run test`, `devbox run
lint`, `devbox run build`, `devbox run docs`) already wrap the full workflow and
should be preferred where possible.

## Registry Publishing

The provider is published on the OpenTofu Registry as
`registry.opentofu.org/xiantongc612/podman-quadlet`. Requirements and gotchas:

- The repository must be named `{owner}/terraform-provider-{name}`. The repo is
  `Xiantongc612/terraform-provider-podman-quadlet`, so the provider type is
  `podman-quadlet`.
- The provider type contains a hyphen, so resource type names are hyphenated
  too (`podman-quadlet_container`, ...). OpenTofu derives the provider type
  from the resource-type prefix; underscore variants such as
  `podman_quadlet_*` do not resolve.
- Before a release: run the real-host validation gate, generate a GPG signing
  key stored in repository Actions secrets, and add
  `terraform-registry-manifest.json`, `.goreleaser.yml`, and a `v*` release
  workflow producing per-platform zips with a GPG-signed `SHA256SUMS(.sig)`.
- Submit the provider and the signing key through the `opentofu/registry` issue
  forms (web UI only; `gh`/API submissions are auto-closed). The tagged release
  must already exist and be valid before submission.
