# AGENTS.md

## Language
- Prefer Golang language and idiomatic Go conventions.

## Architecture
- Prefer Clean Architecture.
- Prefer Single Responsibility Principle.
- Prefer Dependency Inversion.
- Prefer interface-driven design.
- Prefer Test Driven Development.
- Prefer Spec Driven Development.

## CLI
- Prefer `github.com/dh-kam/refutils/flagsbinder`.
- Usually use `cobra` and `viper` with `flagsbinder`.
- Set each cobra command `SilenceErrors=true` and `SilenceUsage=true`.
- Validate args/options in `PreRunE`.
- If validation fails in `PreRunE`, call `cmd.Usage()` then return error.
- `RunE` executes behavior using validated options and returns error on failure.
- `main.go` receives returned errors, prints `ERROR:` prefix to `os.Stderr`, then calls `os.Exit(1)`.
- This should be the only `os.Exit`/crash point in the program.

## Build
- Prefer `make` based build flow.
- Support selectors: `OS-ARCH-VARIANT`, `OS-ARCH`, `ARCH-VARIANT`, `OS`, `ARCH`, `VARIANT`, `OS-VARIANT`.
- Output layout: `build/OS-ARCH/VARIANT/<app_name>`.
- Windows artifacts require `.exe` suffix.
- Value sets:
  - OS: `linux`, `windows`
  - ARCH: `arm64`, `amd64`
  - VARIANT: `debug`, `release`
- `release` should be statically linked standalone binary.
