# Flow data source for Grafana

A backend data source plugin that reads measures and tags from the Flow Server public API (`api/v1`). The user-facing documentation, which Grafana.com shows, is [src/README.md](src/README.md).

## How it maps to Flow

| Plugin | Flow public API |
| --- | --- |
| Browse, Model tab | `api/v1/model/folders`, `folders/{id}/folders`, `folders/{id}/metrics`, `metrics/{id}/measures` |
| Browse, Templates tab | The same routes under `api/v1/template` |
| Search | `api/v1/search` (Metric/Measure, or TemplateMetric/TemplateMeasure) |
| Period measures | `api/v1/data/measures?id=..&templateid=..&calendarid=..` |
| Tags (timestamp measures) | `api/v1/data/tags?tagid=..&templateid=..[&aggregation=..&resolution=..]` |
| Series names | `api/v1/model/measures?id=..`, `model/metrics?id=..`, `model/folders` (cached 5 minutes) |
| Chart browser | `api/v1/information/folders`, `folders/{id}/folders`, `folders/{id}/charts` |
| Chart queries | `api/definition/charts/{id}` (layout, cached 5 minutes) joined with `api/data/charts/{id}?periodStart=..&periodEnd=..` |
| Dependencies | `api/v1/model|template/measures?id=..&metadata=true`: calculation `dependents`, aggregation `measure`/`event`, manual `SetToMeasure` `id`. "Used by" loads every measure's metadata and reverses the links (cached 5 minutes). Event names from `api/v1/model/events?id=..` |
| Option groups in the editor | `summary` resource: the selection resolved like a query, counted by kind |
| Save & test | `api/token/validate`, then `api/v1/config/calendars` |

Template expansion happens in Flow: `templateid` returns data for every accessible model measure whose `TemplateID` matches. The plugin decides which endpoint to use from each measure's `interval` (`timestamp` = tag).

Flow behaviour the code works around:

- `data/measures` caps periods across all measures (`limit`) without ordering them, so its `continuation` cannot be paged. The plugin asks for 100,000 periods and warns when the answer is truncated.
- `data/measures` answers 400 "Sequence contains no elements" when no period falls in the range. Aggregated `data/tags` answers 400 "No valid periods" in the same case. Both are treated as no data.
- `data/tags` sets one `continuation` for the whole answer, so later pages repeat values of tags that were not cut off. These repeats are dropped.
- Times without a `Z` are read by Flow as server-local, so the plugin always sends UTC with `Z`.
- Measure names are `<metric>.<measure>`, the same on every template instance, so series are named by folder path.
- The chart endpoints are the ones the Flow web app uses, not the public API. They accept a server token. Every `definition/charts` read adds a `ChartUsage` row in Flow, so definitions are cached. Data uses short field names (`mid`, `pid`, `v`, `fv`, `ex`; limits `mlid`, `h`, `l`, `t`). Cell colours follow Flow's `LimitColorProvider`: the value's exception for the row's limit picks the high or low colour, a value within the limit the target colour (green when the limit has none), and `theme.*` colours are skipped.
- `metadata=true` fails the whole request with a 500 when one measure's metadata cannot be built: `ModelMeasure.cs` builds aggregation metadata from `MeasureAggregationMeasure` without a null check, and some aggregated tag measures have none. The client retries a failing chunk one id at a time and reports the ids that still fail.
- The Flow table colours cells through Grafana's table `custom.styleField`: each period column names a hidden column of per-row CSS. Comments (`c` on each value, names from `usernames`) go in hidden per-period columns that the period column names as its `custom.tooltip.field`; Grafana marks cells whose tooltip value is not null. These need Grafana 12.2 or later.

## Layout

| Path | What it is |
| --- | --- |
| `pkg/flow` | Public API client and types. |
| `pkg/plugin` | The data source: queries (`query.go`), frames, the `browse`/`search`/`calendars` resources, and the series-name index. |
| `pkg/models` | Data source settings and validation. |
| `src` | Frontend: config editor, query editor, model browser. |
| `provisioning` | Datasource provisioned into the dev Grafana. |

## Developing without installing Go or Node

Every build step runs in a container. `node_modules` and the Go caches live in Docker volumes (`flowds-*`), so they stay out of this folder and out of OneDrive.

```powershell
$repo = (Get-Location).Path

# Frontend: install, check, test, build into dist/
docker run --rm -v "${repo}:/src" -v flowds-node-modules:/src/node_modules -v flowds-npm-cache:/root/.npm -w /src node:24 `
  sh -c "npm install && npm run typecheck && npm run lint && npm run test:ci && npm run build"

# Backend: test, then build the linux binary into dist/
docker run --rm -v "${repo}:/src" -v flowds-gomod:/go/pkg/mod -v flowds-gocache:/root/.cache/go-build -v flowds-gobin:/go/bin -w /src golang:1.26 `
  sh -c "go test ./... && (command -v mage >/dev/null || go install github.com/magefile/mage@v1.15.0) && mage -v build:linux"
```

For a release build of every platform, run `sh scripts/build-backend.sh` in the same container. It builds the targets one at a time, because the parallel `mage -v` intermittently fails to write the Linux binary when the repo is a Windows folder mounted into Docker.

### Dev Grafana

The plugin runs in the Timebase plugin's dev Grafana at http://localhost:3000, next to that plugin, rather than in a Grafana of its own. `docker-compose.timebase-grafana.yaml` layers onto that repo's compose files. It mounts this repo's `dist/`, allows both unsigned plugins, and keeps Grafana's data in the `tbi3x-grafana-data` volume, so dashboards, alert rules and datasources survive the container being recreated. Run it from this folder, with the Timebase client secret set for the one command:

```powershell
$env:TIMEBASE_CLIENT_SECRET = "<Historian client secret>"
docker compose -p timebase-i3x-datasource `
  -f ../../Timebase-Grafana/Historian/docker-compose.yaml `
  -f ../../Timebase-Grafana/Historian/docker-compose.timebase.yaml `
  -f docker-compose.timebase-grafana.yaml up -d
Remove-Item Env:TIMEBASE_CLIENT_SECRET
```

The Flow datasource there (uid `flow-dev`) was created through Grafana's API, not provisioned, so a token pasted on its settings page persists. Provisioning would reset the token at every start. From inside the container the Docker host is `host.docker.internal`, so its URL is `http://host.docker.internal:4501/flow-software/flow/instances/<InstanceID>/server`.

To rebuild the backend, stop that Grafana first (`docker stop flowsoftware-timebasei3x-datasource`), build, then `docker start` it. While it runs, Grafana holds `dist/gpx_infohub_linux_amd64` open and, on a Windows bind mount, the build fails to overwrite it. The frontend is picked up on a browser refresh.

`provisioning/` and `docker-compose.yaml` alone are the stand-alone Grafana that CI uses for the e2e tests.

To package shareable zips (with `INSTALL.md` inside each), run `scripts/package.py` after a full build; see the command at the top of that file.

## License

Apache License 2.0: see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE) (copyright Flow Software). The license came with Grafana's plugin template and was kept on purpose. Every release zip carries both files.

## Getting a token

Flow's public API accepts only a bearer JWT. Create a long-lived server token in Flow Config (server settings, or `--create-server-token` on the CLI). It is stored as a `ServerKey` row, and deleting the row revokes it. The key's group scopes which folders the plugin sees.
