# Infohub-Grafana

Grafana plugins for Flow.

| Folder | Plugin | Download |
| --- | --- | --- |
| [`Flow/`](Flow) | Flow data source: model and template measures, tags, Flow charts as tables, and measure dependencies as a node graph | [Releases](https://github.com/flow-software/Infohub-Grafana/releases) (`flow-v*`) |

Each release has a zip per platform (Windows, Linux, macOS), one for any platform, checksums and install steps. The plugin is not signed yet, so Grafana must be told to load it; see [`Flow/INSTALL.md`](Flow/INSTALL.md).

## License

The Flow plugin is licensed under the Apache License 2.0, copyright Flow Software: see [`Flow/LICENSE`](Flow/LICENSE) and [`Flow/NOTICE`](Flow/NOTICE).

## Releasing

Set `version` in `Flow/package.json`, add a matching section to `Flow/CHANGELOG.md`, then push a tag:

```sh
git tag flow-v1.0.1 && git push origin flow-v1.0.1
```

GitHub Actions builds and tests the plugin, then publishes the release with the zips and the changelog section as notes.
