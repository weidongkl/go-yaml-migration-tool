# go-yaml-migration-tool

`yaml-import-migrator` is a lightweight CLI tool that scans Go projects and automatically migrates deprecated YAML imports.

## Background

 `gopkg.in/yaml.v3 `was [marked as unmaintained in April 2025](https://github.com/go-yaml/yaml). 

The official YAML organization now maintains [the fork](https://github.com/yaml/go-yaml):`go.yaml.in/yaml/v3`

This tool helps migrate your project automatically and safely.

## Features

- Recursively scans Go source code
- Updates deprecated import paths
- Preserves major versions (v2, v3)
- Works with monorepos
- Automatically runs `go mod tidy`

## Installation

```bash
go install github.com/weidongkl/go-yaml-migration-tool@latest
```

## Usage

Run in the root of your Go project:

```bash
go-yaml-migration-tool
```

Specify a custom path:

```bash
go-yaml-migration-tool -path ./myproject
```
