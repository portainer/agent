package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/portainer/agent"
	aos "github.com/portainer/agent/os"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const requestRetryWait = 5 * time.Second

// PortainerEdgeClient is used to execute HTTP requests against the Portainer API
type PortainerEdgeClient struct {
	version          string
	httpClient       *edgeHTTPClient
	serverAddress    string
	setEndpointIDFn  setEndpointIDFn
	getEndpointIDFn  getEndpointIDFn
	edgeID           string
	agentPlatform    agent.ContainerPlatform
	metaFields       agent.EdgeMetaFields
	reqCache         *lru.Cache
	alertStateHeader string
}

type globalKeyResponse struct {
	EndpointID portainer.EndpointID `json:"endpointID"`
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     portainer.EdgeStackStatusType
	EndpointID portainer.EndpointID
	RollbackTo *int `json:",omitempty"`
	Time       int64
	Version    int
}

type logFilePayload struct {
	FileContent string
}

type NonOkResponseError struct {
	msg string
}

func newNonOkResponseError(msg string) *NonOkResponseError {
	return &NonOkResponseError{msg: msg}
}

func (e *NonOkResponseError) Error() string {
	return e.msg
}

// NewPortainerEdgeClient returns a pointer to a new PortainerEdgeClient instance
func NewPortainerEdgeClient(
	serverAddress string,
	setEIDFn setEndpointIDFn,
	getEIDFn getEndpointIDFn,
	edgeID string,
	agentPlatform agent.ContainerPlatform,
	metaFields agent.EdgeMetaFields,
	httpClient *edgeHTTPClient,
	opts ...Option,
) *PortainerEdgeClient {
	clientOpts := defaultOptions()
	for _, o := range opts {
		o(clientOpts)
	}

	c := &PortainerEdgeClient{
		version:         clientOpts.version,
		serverAddress:   serverAddress,
		setEndpointIDFn: setEIDFn,
		getEndpointIDFn: getEIDFn,
		edgeID:          edgeID,
		agentPlatform:   agentPlatform,
		httpClient:      httpClient,
		metaFields:      metaFields,
	}

	cache, err := lru.New(8)
	if err == nil {
		c.reqCache = cache
	} else {
		log.Warn().Err(err).Msg("could not initialize the cache")
	}

	return c
}

func (client *PortainerEdgeClient) SetTimeout(t time.Duration) {
	client.httpClient.httpClient.Timeout = t
}

// SetAlertState stores the JSON-serialised alert state for inclusion in the
// next poll request as an HTTP header.
//
// WARNING: the header value is unbounded in size. With many alert rules or
// verbose reload-error text the payload may exceed the header-size limit of
// an intermediate reverse proxy (commonly 8 KB per header). This should be
// migrated to a dedicated request-body field in a future change.
func (client *PortainerEdgeClient) SetAlertState(state *pkgmetrics.EdgeAlertState) {
	client.alertStateHeader = ""
	if state == nil {
		return
	}

	alertJSON, err := json.Marshal(state)
	if err != nil {
		log.Warn().Err(err).Msg("failed to marshal alert state")
		return
	}

	client.alertStateHeader = string(alertJSON)
}

func (client *PortainerEdgeClient) GetEnvironmentID() (portainer.EndpointID, error) {
	if client.edgeID == "" {
		return 0, errors.New("edge ID not set")
	}

	// set default payload
	payloadJson := []byte("{}")
	if len(client.metaFields.EdgeGroupsIDs) > 0 || len(client.metaFields.TagsIDs) > 0 || client.metaFields.EnvironmentGroupID > 0 {
		payload := &MetaFields{
			EdgeGroupsIDs:      client.metaFields.EdgeGroupsIDs,
			TagsIDs:            client.metaFields.TagsIDs,
			EnvironmentGroupID: client.metaFields.EnvironmentGroupID,
		}

		var err error
		payloadJson, err = json.Marshal(payload)
		if err != nil {
			return 0, errors.WithMessage(err, "failed to marshal meta fields")
		}
	}

	gkURL := client.serverAddress + "/api/endpoints/global-key"
	req, err := http.NewRequest(http.MethodPost, gkURL, bytes.NewReader(payloadJson))
	if err != nil {
		return 0, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("response_code", resp.StatusCode).Msg("global key request failure")

		return 0, errors.New("global key request failed")
	}

	var responseData globalKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return 0, err
	}

	return responseData.EndpointID, nil
}

func (client *PortainerEdgeClient) GetEnvironmentStatus(flags ...string) (*PollStatusResponse, error) {
	pollURL := fmt.Sprintf("%s/api/endpoints/%d/edge/status", client.serverAddress, client.getEndpointIDFn())
	req, err := http.NewRequest("GET", pollURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("If-None-Match", client.cacheHeaders())

	req.Header.Set(agent.HTTPResponseAgentHeaderName, client.version)
	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	timeZone := time.Local.String()
	req.Header.Set(agent.HTTPResponseAgentTimeZone, timeZone)

	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatform)))
	log.Debug().Int("agent_platform", int(client.agentPlatform)).Str("time_zone", timeZone).Msg("sending headers")

	req.Header.Set(agent.HTTPResponseUpdateIDHeaderName, strconv.Itoa(client.metaFields.UpdateID))

	if containerEngine := aos.GetContainerEngineName(aos.DetermineContainerPlatform()); containerEngine != "" {
		req.Header.Set(agent.HTTPResponseAgentContainerEngine, containerEngine)
	}

	if client.alertStateHeader != "" {
		req.Header.Set(agent.HTTPAlertStateHeaderName, client.alertStateHeader)
	}

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	cachedResp, ok := client.cachedResponse(resp)
	if ok {
		return cachedResp, nil
	}

	if resp.StatusCode != http.StatusOK {
		errorData := parseError(resp)
		logError(resp, errorData)

		if errorData != nil {
			return nil, newNonOkResponseError(errorData.Message + ": " + errorData.Details)
		}

		return nil, newNonOkResponseError("short poll request failed")
	}

	var responseData PollStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return nil, err
	}

	respForCache := mutateResponseForCaching(&responseData)

	client.cacheResponse(resp.Header.Get("ETag"), &respForCache)

	return &responseData, nil
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerEdgeClient) GetEdgeStackConfig(edgeStackID int, version *int) (*edge.StackPayload, error) {
	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/stacks/%d", client.serverAddress, client.getEndpointIDFn(), edgeStackID)

	if version != nil {
		requestURL += fmt.Sprintf("?version=%d", *version)
	}

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeStackConfig operation failed")

		return nil, errors.New("GetEdgeStackConfig operation failed")
	}

	var data edge.StackPayload
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) SetEdgeStackStatus(
	edgeStackID, version int,
	edgeStackStatus portainer.EdgeStackStatusType,
	rollbackTo *int,
	error string,
) error {
	payload := setEdgeStackStatusPayload{
		Error:      error,
		Status:     edgeStackStatus,
		EndpointID: client.getEndpointIDFn(),
		RollbackTo: rollbackTo,
		Time:       time.Now().Unix(),
		Version:    version,
	}

	log.Debug().
		Int("edgeStackID", edgeStackID).
		Str("edgeStackStatus", edgeStackStatus.String()).
		Int("time_check", int(payload.Time)).
		Msg("SetEdgeStackStatus")

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/edge_stacks/%d/status", client.serverAddress, edgeStackID)

	var resp *http.Response
	for {
		req, err := http.NewRequest(http.MethodPut, requestURL, bytes.NewReader(data))
		if err != nil {
			return err
		}

		req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)
		req.Header.Set("X-Portainer-No-Body", "1")

		resp, err = client.httpClient.Do(req)
		if err != nil {
			log.Error().
				Err(err).
				Int("edgeStackID", edgeStackID).
				Msg("could not set edge stack status, retrying...")

			time.Sleep(requestRetryWait)

			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)

		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}

		if resp.StatusCode < http.StatusInternalServerError {
			break
		}

		log.Debug().
			Str("status", resp.Status).
			Int("edgeStackID", edgeStackID).
			Msg("could not set edge stack status, retrying...")

		time.Sleep(requestRetryWait)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeStackStatus operation failed")

		return errors.New("SetEdgeStackStatus operation failed")
	}

	return nil
}

type HelmChartStatusPayload struct {
	Statuses []portainer.PolicyChartStatus `json:"chartStatuses"`
}

func (client *PortainerEdgeClient) UpdatePolicyChartStatuses(statuses []portainer.PolicyChartStatus) error {
	payload := HelmChartStatusPayload{
		Statuses: statuses,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/charts/statuses", client.serverAddress, client.getEndpointIDFn())

	var resp *http.Response
	for {
		req, err := http.NewRequest(http.MethodPut, requestURL, bytes.NewReader(data))
		if err != nil {
			return err
		}

		req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

		resp, err = client.httpClient.Do(req)
		if err != nil {
			log.Error().
				Err(err).
				Msg("could not update policy chart statuses, retrying...")

			time.Sleep(requestRetryWait)

			continue
		}

		_, _ = io.Copy(io.Discard, resp.Body)

		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}

		if resp.StatusCode < http.StatusInternalServerError {
			break
		}

		log.Debug().
			Str("status", resp.Status).
			Msg("could not update policy chart statuses, retrying...")

		time.Sleep(requestRetryWait)
	}

	if resp.StatusCode != http.StatusNoContent {
		log.Error().Int("response_code", resp.StatusCode).Msg("UpdatePolicyChartStatuses operation failed")

		return errors.New("UpdatePolicyChartStatuses operation failed")
	}

	return nil
}

// SetEdgeJobStatus sends the jobID log to the Portainer server
func (client *PortainerEdgeClient) SetEdgeJobStatus(edgeJobStatus agent.EdgeJobStatus) error {
	payload := logFilePayload{
		FileContent: edgeJobStatus.LogFileContent,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/jobs/%d/logs", client.serverAddress, client.getEndpointIDFn(), edgeJobStatus.JobID)

	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("failed to close response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeJobStatus operation failed")

		return errors.New("SetEdgeJobStatus operation failed")
	}

	return nil
}

func (client *PortainerEdgeClient) GetEdgeConfig(id EdgeConfigID) (*EdgeConfig, error) {
	requestURL := fmt.Sprintf("%s/api/edge_configurations/%d/files", client.serverAddress, id)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeConfig operation failed")

		if resp.StatusCode == http.StatusForbidden {
			return nil, errors.New("GetEdgeConfig operation forbidden")
		}

		return nil, errors.New("GetEdgeConfig operation failed")
	}

	var data EdgeConfig
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

func (client *PortainerEdgeClient) SetEdgeConfigState(id EdgeConfigID, state EdgeConfigStateType) error {
	requestURL := fmt.Sprintf("%s/api/edge_configurations/%d/%d", client.serverAddress, id, state)

	req, err := http.NewRequest(http.MethodPut, requestURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("failed to close response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("edge_config_id", int(id)).Stringer("state", state).Int("response_code", resp.StatusCode).Msg("SetEdgeConfigState operation failed")

		return errors.New("SetEdgeConfigState operation failed")
	}

	return nil
}

func (client *PortainerEdgeClient) ProcessAsyncCommands() error {
	return nil // edge mode only
}

func (client *PortainerEdgeClient) SetLastCommandTimestamp(timestamp time.Time) {} // edge mode only

func (client *PortainerEdgeClient) EnqueueLogCollectionForStack(logCmd LogCommandData) {}

func (client *PortainerEdgeClient) cacheHeaders() string {
	if client.reqCache == nil {
		return ""
	}

	ks := client.reqCache.Keys()

	var strKs []string
	for _, k := range ks {
		strKs = append(strKs, k.(string))
	}

	return strings.Join(strKs, ",")
}

// GetCharts retrieves the chart contents for the specified charts from the Portainer server
func (client *PortainerEdgeClient) GetCharts(chartNames []string) ([]portainer.PolicyChartBundle, portainer.RestoreSettingsBundle, error) {
	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/charts", client.serverAddress, client.getEndpointIDFn())

	// Prepare the charts to install data
	chartsData, err := json.Marshal(chartNames)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal charts data: %w", err)
	}

	// Create URL with chartNames query parameter
	queryParams := url.Values{}
	queryParams.Set("chartNames", string(chartsData)) // With only a few chart types, hitting the max URL length is unlikely
	requestURL += "?" + queryParams.Encode()

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("GetCharts operation failed")
		return nil, nil, fmt.Errorf("GetCharts operation failed with status code %d", resp.StatusCode)
	}

	var res PolicyHelmCharts
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return res.PolicyChartBundles, res.RestoreSettingsBundle, nil
}

func (client *PortainerEdgeClient) cachedResponse(r *http.Response) (*PollStatusResponse, bool) {
	etag := r.Header.Get("ETag")

	if client.reqCache == nil || r.StatusCode != http.StatusNotModified || etag == "" {
		return nil, false
	}

	if resp, ok := client.reqCache.Get(etag); ok {
		return resp.(*PollStatusResponse), true
	}

	return nil, false
}

func (client *PortainerEdgeClient) cacheResponse(etag string, resp *PollStatusResponse) {
	if client.reqCache == nil || etag == "" {
		return
	}

	client.reqCache.Add(etag, resp)
}

// mutateResponseForCaching makes a copy of the original PollStatusResponse
// and mutates it for caching purpose (e.g. removing ForceRedeploy flags)
// This is to avoid to cache a response that would trigger a redeployment.
// Without mutation, when a ForceRedeploy flag is set to true and cached, the
// redeployment would be triggered on every subsequent poll request. e.g. every 5s
func mutateResponseForCaching(resp *PollStatusResponse) PollStatusResponse {
	// Clone the original poll status response
	respForCache := *resp
	respForCache.Stacks = make([]StackStatus, len(resp.Stacks))
	copy(respForCache.Stacks, resp.Stacks)

	// Mutate for caching
	for i, stack := range respForCache.Stacks {
		if stack.ForceRedeploy {
			respForCache.Stacks[i].ForceRedeploy = false
		}
	}

	return respForCache
}
