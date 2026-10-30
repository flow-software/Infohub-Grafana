package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flowdb"
	"github.com/flow-software/Infohub-Grafana/Flow/pkg/models"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// How long folder, metric, measure and calendar lookups are reused.
const cacheTTL = 5 * time.Minute

// NewDatasource builds one instance per configured datasource. Grafana
// disposes it and builds a new one whenever the settings change, which also
// drops the caches.
//
// Invalid settings do not fail construction: the error is kept and reported
// by the health check and by every query, which is clearer than a generic
// "plugin unavailable".
func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := models.LoadPluginSettings(settings)
	if err != nil {
		return &Datasource{configErr: err}, nil
	}
	if err := cfg.Validate(); err != nil {
		return &Datasource{configErr: err}, nil
	}

	opts, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("http client options: %w", err)
	}
	// Template measures can expand to hundreds of instances; give Flow time.
	opts.Timeouts.Timeout = 2 * time.Minute
	httpClient, err := httpclient.New(opts)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}

	client := &flow.Client{BaseURL: cfg.URL, Token: cfg.Secrets.Token, HTTP: httpClient}
	ds := &Datasource{
		client:            client,
		index:             newModelIndex(client, cacheTTL),
		httpClient:        httpClient,
		defaultCalendarID: cfg.DefaultCalendarID,
	}
	if cfg.SQLConfigured() {
		db, err := flowdb.Open(flowdb.Config{
			Server: cfg.SQLServer, Database: cfg.SQLDatabase, User: cfg.SQLUser, Password: cfg.Secrets.SQLPassword,
			Encrypt: cfg.SQLEncrypt, TrustServerCertificate: cfg.SQLTrustServerCertificate,
		})
		if err != nil {
			return &Datasource{configErr: fmt.Errorf("flow database: %w", err)}, nil
		}
		ds.db = db
	}
	return ds, nil
}

type Datasource struct {
	client            *flow.Client
	index             *modelIndex
	httpClient        *http.Client
	defaultCalendarID int
	configErr         error
	// db is the optional Flow database, for the processing state of
	// measures; nil when not configured.
	db processingSource

	calMu        sync.Mutex
	calendars    []flow.Calendar
	calendarsAge time.Time

	trees      treeCache
	charts     chartCache
	chartLists chartMeasureCache
	deps       depIndex
	attrs      attributeCache
}

func (d *Datasource) Dispose() {
	if d.httpClient != nil {
		d.httpClient.CloseIdleConnections()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
}

// processingSource reads how far the engine has got with measures; the Flow
// database in production, a fake in tests.
type processingSource interface {
	Check(ctx context.Context) (string, error)
	Processing(ctx context.Context, ids []int) (map[int]flowdb.Processing, error)
	Close() error
}

// listCalendars caches Flow's calendars, which change rarely.
func (d *Datasource) listCalendars(ctx context.Context) ([]flow.Calendar, error) {
	d.calMu.Lock()
	if d.calendars != nil && time.Since(d.calendarsAge) < cacheTTL {
		defer d.calMu.Unlock()
		return d.calendars, nil
	}
	d.calMu.Unlock()

	list, err := d.client.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	d.calMu.Lock()
	d.calendars, d.calendarsAge = list, time.Now()
	d.calMu.Unlock()
	return list, nil
}

// calendarFor picks the query's calendar, else the datasource default, else
// the first calendar Flow lists. Flow cannot read period values without one.
func (d *Datasource) calendarFor(ctx context.Context, queryCalendarID int) (int, error) {
	if queryCalendarID > 0 {
		return queryCalendarID, nil
	}
	if d.defaultCalendarID > 0 {
		return d.defaultCalendarID, nil
	}
	list, err := d.listCalendars(ctx)
	if err != nil {
		return 0, fmt.Errorf("could not list calendars: %w", err)
	}
	if len(list) == 0 {
		return 0, errors.New("there are no calendars in Flow, and period measures need one")
	}
	return list[0].ID, nil
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, q backend.DataQuery) backend.DataResponse {
	if d.configErr != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, "datasource is not configured: "+d.configErr.Error())
	}
	var qm queryModel
	if err := json.Unmarshal(q.JSON, &qm); err != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, fmt.Sprintf("invalid query: %v", err))
	}
	// A chart read as measures is a measures query over the chart's measures.
	if qm.QueryType == queryTypeChart && qm.ChartMode == chartModeMeasures {
		if qm.ChartID == 0 {
			return backend.DataResponse{}
		}
		label := qm.ChartLabel
		if label == "" {
			label = fmt.Sprintf("chart %d", qm.ChartID)
		}
		qm.QueryType = queryTypeMeasures
		qm.Items = []queryItem{{Tree: flow.TreeModel, Type: itemChart, ID: qm.ChartID, Label: label}}
	}
	if err := qm.validate(); err != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, err.Error())
	}
	var resp backend.DataResponse
	var err error
	switch {
	case qm.QueryType == queryTypeChart && qm.ChartID == 0:
		return backend.DataResponse{}
	case qm.QueryType == queryTypeChart:
		resp, err = d.runChart(ctx, qm, q)
	case qm.QueryType == queryTypeVariable:
		resp, err = d.runVariable(ctx, qm, q.TimeRange)
	case len(qm.Items) == 0:
		return backend.DataResponse{}
	case qm.QueryType == queryTypeDeps:
		resp, err = d.runDependencies(ctx, qm, q)
	case qm.QueryType == queryTypeEvents:
		resp, err = d.runEvents(ctx, qm, q)
	default:
		resp, err = d.run(ctx, qm, q)
	}
	if err != nil {
		return errorResponse(err)
	}
	return resp
}

func errorResponse(err error) backend.DataResponse {
	var withStatus interface{ HTTPStatus() int }
	if errors.As(err, &withStatus) {
		return backend.ErrDataResponseWithSource(backend.Status(withStatus.HTTPStatus()), backend.ErrorSourceDownstream, err.Error())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return backend.ErrDataResponseWithSource(backend.StatusTimeout, backend.ErrorSourceDownstream, err.Error())
	}
	return backend.ErrDataResponseWithSource(backend.StatusBadGateway, backend.ErrorSourceDownstream, err.Error())
}

// CheckHealth backs the "Save & test" button: the token first, so a bad token
// is reported as such, then a v1 call that proves the URL reaches the API.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	fail := func(msg string) (*backend.CheckHealthResult, error) {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: msg}, nil
	}
	if d.configErr != nil {
		return fail(d.configErr.Error())
	}
	if err := d.client.Validate(ctx); err != nil {
		return fail("Token check failed: " + err.Error())
	}

	d.calMu.Lock()
	d.calendars = nil
	d.calMu.Unlock()
	calendars, err := d.listCalendars(ctx)
	if err != nil {
		return fail("The token works, but the public API call failed: " + err.Error())
	}
	if d.defaultCalendarID > 0 {
		found := false
		for _, c := range calendars {
			found = found || c.ID == d.defaultCalendarID
		}
		if !found {
			return fail(fmt.Sprintf("Connected, but the default calendar (id %d) does not exist in Flow", d.defaultCalendarID))
		}
	}
	msg := fmt.Sprintf("Connected to Flow. %d calendar(s) available.", len(calendars))
	if d.db != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		name, err := d.db.Check(dbCtx)
		if err != nil {
			return fail(msg + " But the Flow database connection failed: " + err.Error())
		}
		msg += fmt.Sprintf(" Flow database %s connected: dependency queries can show processing.", name)
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}, nil
}
