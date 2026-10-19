# Installing the Flow data source

This plugin is not signed yet, so Grafana must be told to load it.

1. Unzip this archive into Grafana's plugins directory, so the result is `<plugins>/flowsoftware-infohub-datasource/plugin.json`.
   The plugins directory is `/var/lib/grafana/plugins` on Linux packages and Docker, and `<Grafana install>\data\plugins` on Windows.
2. Allow the unsigned plugin in `grafana.ini`:

   ```ini
   [plugins]
   allow_loading_unsigned_plugins = flowsoftware-infohub-datasource
   ```

   or with the environment variable `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=flowsoftware-infohub-datasource`.
3. Restart Grafana.
4. Add a data source of type **Flow**. Enter the Flow Server URL and a server token from Flow Config, then select **Save & test**.
