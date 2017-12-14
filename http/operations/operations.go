package operations

import (
	"net/http"
	"sync"

	"bitbucket.org/portainer/agent"
)

func NodeOperation(request *http.Request, targetNode string) (interface{}, error) {
	response, err := executeRequestOnSpecifiedHost(request, targetNode)
	if err != nil {
		return nil, err
	}

	data, err := getResponseBodyAsGenericJSON(response)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func ClusterOperation(request *http.Request, clusterMembers []agent.ClusterMember) ([]interface{}, error) {

	memberCount := len(clusterMembers)

	// we create a slice with a capacity of memberCount but 0 size
	// so we'll avoid extra unneeded allocations
	data := make([]interface{}, 0, memberCount)

	// we create a buffered channel so writing to it won't block while we wait for the waitgroup to finish
	ch := make(chan parallelRequestResult, memberCount)

	// we create a waitgroup - basically block until N tasks say they are done
	wg := sync.WaitGroup{}

	for _, member := range clusterMembers {
		//we add 1 to the wait group - each worker will decrease it back
		wg.Add(1)

		go executeParallelRequest(request, member.Name, ch, &wg)
	}

	// now we wait for everyone to finish - again, not a must.
	// you can just receive from the channel N times, and use a timeout or something for safety

	// TODO: a timeout should be used to here (or when executing HTTP requests)
	// to avoid blocking if one of the agent is not responding
	wg.Wait()

	// we need to close the channel or the following loop will get stuck
	close(ch)

	// we iterate over the closed channel and receive all data from it

	// TODO: find a way to manage any error that would be raised in a parallel request
	// It's available in the result.err field
	for result := range ch {
		for _, JSONObject := range result.data {

			// TODO: object should be decorated inside the "Portainer" namespace
			object := JSONObject.(map[string]interface{})
			agentMetadata := make(map[string]interface{})
			agentMetadata["Node"] = result.nodeName
			object["PortainerAgent"] = agentMetadata

			data = append(data, object)
		}
	}

	return data, nil
}
