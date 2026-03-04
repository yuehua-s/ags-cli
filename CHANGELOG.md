# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Add `--user` flag to `exec`, `file`, and `instance login` commands to specify the user identity for data plane operations (default: "user")
- Add `sandbox.default_user` configuration option in config.toml for setting the default user globally

## [0.1.2] - 2026-02-11

### Changed
- E2B backend now supports token acquisition via GET /sandboxes/{id}, removing the limitation that tokens were only available at instance creation time
- Unified token recovery logic for both Cloud and E2B backends when token cache is missing

## [0.1.1] - 2026-01-20

### Changed
- Separate control plane and data plane with token caching

## [0.1.0] - 2026-01-16

### Added
- Initial release
- Update module path to github.com/TencentCloudAgentRuntime/ags-cli
- Replace all git.woa.com references with github.com/TencentCloudAgentRuntime/ags-go-sdk v0.0.10
