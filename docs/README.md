# Kongweave documentation

Learn how to compose environment-specific configurations, inspect their history, and hand portable bundles to decK or kongctl.

| Language | Guide | Overview |
| --- | --- | --- |
| English | [User guide](guide.md) | [Project README](../README.md) |
| 日本語 | [利用ガイド](guide_ja.md) | [プロジェクト概要](../README_ja.md) |

The guides cover installation, a complete overlay example, resource selectors, patch semantics, native YAML, file references, CLI options, reproducibility, validation, and troubleshooting.
Both languages use the same executable examples.

利用ガイドには、インストール、overlayの作成例、リソースの対象指定、パッチの仕様、ネイティブYAML、ファイル参照、CLIオプション、再現性、検証、トラブルシューティングをまとめています。
英語版と日本語版の実行例は共通です。

## Examples

| Example | What it demonstrates |
| --- | --- |
| [decK](../examples/deck/) | Shared Services, Routes, scoped Plugins, inherited timeouts, and dev/prod patches |
| [kongctl](../examples/kongctl/) | Portals, APIs, AI Gateway models and policies, deferred tags, and portable file references |

Builds run offline and do not deploy configurations.
Official-tool checks and their limits are described in each guide.
