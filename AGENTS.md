# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `kmodules.xyz/image-packer` — a CLI of OCI image tools used by AppsCode catalog and release tooling. Notable subcommands:

- `parse` — parse an image reference into registry/repo/tag/digest components.
- `list` — list all images referenced by a Helm chart.
- `list-editor-charts` — list editor charts (catalog mode).
- `list-feature-charts` — list feature-set charts (`feature-charts.yaml`) and, unless `--with-images=false`, render each of them at its pinned version with the values its Feature carries to list the container images they reference (`feature-chart-images.yaml`). The second output exists because feature charts are third-party — their images appear in the installer tree only as `image.repository` / `image.name` maps inside a Feature's `spec.values`, which `list` cannot see, and without a tag.
- `ace-up` — upload the ACE catalog images to a target registry.
- `generate-scripts` — generate `copy-images.sh` / `export-images.sh` / `import-images.sh` / `import-into-k3s.sh` for an image catalog.
- `generate-gcp-script` — GCP-flavored mirror script.
- `generate-cve-report` — run trivy across an image catalog and emit a markdown CVE table (this is what generates the `catalog/README.md` files in the installer repos).
- `replace-image-digest` — patch an image reference to a pinned digest.

Plus `version` and `completion`. The produced binary is `image-packer`.

The local filesystem path is `kubeops.dev/img-tools` and the GitHub repo is `kubeops/img-tools` (which redirects to `kubeops/image-packer`). The **Go module is `kmodules.xyz/image-packer`** — use that in imports. Other AppsCode installer repos import this module and shell out to `image-packer` via `hack/scripts/update-catalog.sh`.

## Architecture

- `main.go` — entry point at the module root.
- `pkg/cmds/` — one file per top-level subcommand. `root.go` registers them; `completion.go` is shared.
- `pkg/lib/`:
  - `image.go` — image-reference parsing/manipulation.
  - `shell.go` — script generation helpers (`copy-images.sh`, etc.).
  - `trivy.go` — wraps the [trivy](https://github.com/aquasecurity/trivy) CLI for CVE reports.
  - `tests.go` — test helpers.
- `testdata/` — fixtures (chart bundles, image lists).
- `hack/`, `Makefile` — AppsCode build harness. Binary builds for **5 platforms** (linux amd64/arm/arm64 + windows/amd64 + darwin/amd64 + darwin/arm64) — used from operator workstations and CI.
- `vendor/` — checked-in deps.

There is no Docker image — this is a host CLI.

## Common commands

All Make targets run inside `ghcr.io/appscode/golang-dev` — Docker must be running.

- `make ci` — CI pipeline.
- `make build` — build for the host OS/ARCH.
- `make all-build` — build for every `BIN_PLATFORMS` (linux/arm/arm64 + windows + darwin amd64/arm64).
- `make fmt`, `make lint`, `make unit-tests` / `make test` — standard.
- `make verify` — `verify-gen verify-modules`; `go mod tidy && go mod vendor` must leave the tree clean.
- `make add-license` / `make check-license` — manage license headers.

Run a single Go test (requires a local Go toolchain):

```
go test ./pkg/lib/... -run TestName -v
```

## Conventions

- Module path is `kmodules.xyz/image-packer` (vanity URL). **Note**: the GitHub repo name (`kubeops/img-tools`) and Go module name (`kmodules.xyz/image-packer`) are different — use the module path in imports.
- License: Apache-2.0 (`LICENSE`); new files need the standard "Copyright AppsCode Inc. and Contributors" header (`make add-license`).
- Sign off commits (`git commit -s`); contributions follow the DCO (`DCO`, `CONTRIBUTING.md`).
- Vendor directory is checked in — `go mod tidy && go mod vendor` must leave the tree clean (enforced by `verify-modules`).
- New subcommand: drop a `pkg/cmds/<name>.go` and register it in `root.go`. Image-reference parsing and shell-script generation logic belong in `pkg/lib/`; don't grow `pkg/cmds/*.go` files into utility libraries.
- The `generate-cve-report` flow shells out to **trivy**; users must have it installed (the README/script docs the version constraints — keep them in sync when bumping).
- Builds linux/windows/darwin host binaries; do not pull in linux-only or cgo deps.
- Downstream installer repos consume this binary via `hack/scripts/update-catalog.sh` — keep `generate-scripts` output stable, since those scripts are committed to dozens of repos.
