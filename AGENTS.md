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
