package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/edge"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// PortainerEdgeClient is used to execute HTTP requests against the Portainer API
type PortainerEdgeClient struct {
	httpClient      *edgeHTTPClient
	serverAddress   string
	setEndpointIDFn setEndpointIDFn
	getEndpointIDFn getEndpointIDFn
	edgeID          string
	agentPlatform   agent.ContainerPlatform
	metaFields      agent.EdgeMetaFields
	reqCache        *lru.Cache
}

type globalKeyResponse struct {
	EndpointID portainer.EndpointID `json:"endpointID"`
}

type setEdgeStackStatusPayload struct {
	Error      string
	Status     portainer.EdgeStackStatusType
	EndpointID portainer.EndpointID
	Time       int64
}

type logFilePayload struct {
	FileContent string
}

// NewPortainerEdgeClient returns a pointer to a new PortainerEdgeClient instance
func NewPortainerEdgeClient(serverAddress string, setEIDFn setEndpointIDFn, getEIDFn getEndpointIDFn, edgeID string, agentPlatform agent.ContainerPlatform, metaFields agent.EdgeMetaFields, httpClient *edgeHTTPClient) *PortainerEdgeClient {
	c := &PortainerEdgeClient{
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

	gkURL := fmt.Sprintf("%s/api/endpoints/global-key", client.serverAddress)
	req, err := http.NewRequest(http.MethodPost, gkURL, bytes.NewReader(payloadJson))
	if err != nil {
		return 0, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("response_code", resp.StatusCode).Msg("global key request failure")

		return 0, errors.New("global key request failed")
	}

	var responseData globalKeyResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
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

	req.Header.Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	timeZone := time.Local.String()
	req.Header.Set(agent.HTTPResponseAgentTimeZone, timeZone)
	log.Debug().Str("timeZone", timeZone).Msg("sending timeZone header")

	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatform)))
	log.Debug().Int("header", int(client.agentPlatform)).Msg("sending agent platform header")

	req.Header.Set(agent.HTTPResponseUpdateIDHeaderName, strconv.Itoa(client.metaFields.UpdateID))

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	cachedResp, ok := client.cachedResponse(resp)
	if ok {
		return cachedResp, nil
	}

	if resp.StatusCode != http.StatusOK {
		errorData := parseError(resp)
		logError(resp, errorData)

		if errorData != nil {
			return nil, errors.New(errorData.Message + ": " + errorData.Details)
		}

		return nil, errors.New("short poll request failed")
	}

	var responseData PollStatusResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return nil, err
	}

	client.cacheResponse(resp.Header.Get("ETag"), &responseData)

	return &responseData, nil
}

// GetEdgeStackConfig retrieves the configuration associated to an Edge stack
func (client *PortainerEdgeClient) GetEdgeStackConfig(edgeStackID int) (*edge.StackPayload, error) {
	requestURL := fmt.Sprintf("%s/api/endpoints/%d/edge/stacks/%d", client.serverAddress, client.getEndpointIDFn(), edgeStackID)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeStackConfig operation failed")

		return nil, errors.New("GetEdgeStackConfig operation failed")
	}

	var data edge.StackPayload
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) SetEdgeStackStatus(edgeStackID int, edgeStackStatus portainer.EdgeStackStatusType, error string) error {
	payload := setEdgeStackStatusPayload{
		Error:      error,
		Status:     edgeStackStatus,
		EndpointID: client.getEndpointIDFn(),
		Time:       time.Now().Unix(),
	}

	log.Debug().
		Int("edgeStackID", edgeStackID).
		Int("edgeStackStatus", int(edgeStackStatus)).
		Int("time check", int(payload.Time)).
		Msg("SetEdgeStackStatus")

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/api/edge_stacks/%d/status", client.serverAddress, edgeStackID)

	req, err := http.NewRequest(http.MethodPut, requestURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeStackStatus operation failed")

		return errors.New("SetEdgeStackStatus operation failed")
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

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

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

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeConfig operation failed")

		return nil, errors.New("GetEdgeConfig operation failed")
	}

	var data EdgeConfig
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
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

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

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

func (client *PortainerEdgeClient) EnqueueLogCollectionForStack(logCmd LogCommandData) error {
	return nil
}

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
