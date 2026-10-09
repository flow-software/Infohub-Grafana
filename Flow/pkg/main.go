package main

import (
	"os"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/plugin"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

func main() {
	// Blocks until Grafana stops the plugin. Grafana builds one Datasource per
	// configured datasource and rebuilds it whenever its settings change.
	if err := datasource.Manage("flowsoftware-flow-datasource", plugin.NewDatasource, datasource.ManageOpts{}); err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
