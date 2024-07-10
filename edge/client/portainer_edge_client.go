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
	RollbackTo *int `json:",omitempty"`
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
		ctxMsg := "EdgeAgentGetEnvironmentID"
		errMsg := "EdgeAgent failed to request global key"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Msg(errMsg)
		}
		return 0, newNonOkResponseError(errMsg)
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

	req.Header.Set(agent.HTTPResponseAgentHeaderName, agent.Version)
	req.Header.Set(agent.HTTPEdgeIdentifierHeaderName, client.edgeID)

	timeZone := time.Local.String()
	req.Header.Set(agent.HTTPResponseAgentTimeZone, timeZone)

	req.Header.Set(agent.HTTPResponseAgentPlatform, strconv.Itoa(int(client.agentPlatform)))
	log.Debug().Int("agent_platform", int(client.agentPlatform)).Str("time_zone", timeZone).Msg("sending headers")

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
		ctxMsg := "EdgeAgentGetEnvironmentStatus"
		errMsg := "EdgeAgent failed to request edge environment status"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Msg(errMsg)
		}
		return nil, newNonOkResponseError(errMsg)
	}

	var responseData PollStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return nil, err
	}

	client.cacheResponse(resp.Header.Get("ETag"), &responseData)

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ctxMsg := "EdgeAgentGetEdgeStackConfig"
		errMsg := "EdgeAgent failed to request edge stack config"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Msg(errMsg)
		}
		return nil, newNonOkResponseError(errMsg)
	}

	var data edge.StackPayload
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

// SetEdgeStackStatus updates the status of an Edge stack on the Portainer server
func (client *PortainerEdgeClient) SetEdgeStackStatus(
	edgeStackID int,
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
		ctxMsg := "EdgeAgentSetEdgeStackStatus"
		errMsg := "EdgeAgent failed to set edge stack status"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Msg(errMsg)
		}
		return newNonOkResponseError(errMsg)
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
		ctxMsg := "EdgeAgentSetEdgeJobStatus"
		errMsg := "EdgeAgent failed to set edge job status"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Msg(errMsg)
		}
		return newNonOkResponseError(errMsg)
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
		ctxMsg := "EdgeAgentGetEdgeConfig"
		errMsg := "EdgeAgent failed to get edge config info"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Int("edge config", int(id)).
				Msg(errMsg)
		}

		if resp.StatusCode == http.StatusForbidden {
			return nil, newForbiddenResponseError(errMsg)
		}

		return nil, newNonOkResponseError(errMsg)
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

	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ctxMsg := "EdgeAgentSetEdgeConfigState"
		errMsg := "EdgeAgent failed to set state to edge config"
		if err := decodeNonOkayResponse(resp, ctxMsg); err != nil {
			log.
				Error().Err(err.Err).
				Str("context", ctxMsg).
				Str("response message", err.Message).
				Int("status code", err.StatusCode).
				Int("endpoint id", int(client.getEndpointIDFn())).
				Int("edge config id", int(id)).
				Int("edge state", int(state)).
				Msg(errMsg)
		}
		return newNonOkResponseError(errMsg)
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
