# Installing Infohub

## From the Grafana plugin catalog

In Grafana, open **Administration → Plugins and data → Plugins**, search for **Infohub** and select **Install**. Or, on the Grafana server:

```sh
grafana cli plugins install flowsoftware-infohub-datasource
```

Then restart Grafana if you used the command line.

## From a release zip

1. Unzip the archive into Grafana's plugins directory, so the result is `<plugins>/flowsoftware-infohub-datasource/plugin.json`.
   The plugins directory is `/var/lib/grafana/plugins` on Linux packages and Docker, and `<Grafana install>\data\plugins` on Windows.
2. Only if the build is unsigned (a build from before the catalog release, or one you built yourself), allow it in `grafana.ini`:

   ```ini
   [plugins]
   allow_loading_unsigned_plugins = flowsoftware-infohub-datasource
   ```

   or with the environment variable `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=flowsoftware-infohub-datasource`.
3. Restart Grafana.

## Add the data source

Add a data source of type **Infohub**. Enter the Flow Bootstrap **Host** (and **Port**, if not 4501), the Flow **Instance ID**, and a server token from Flow Config, then select **Save & test**. The data source's **Dashboards** tab offers example dashboards to import.
