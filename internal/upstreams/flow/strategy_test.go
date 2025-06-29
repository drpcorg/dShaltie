package flow_test

import (
	"context"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/drpcorg/dsheltie/internal/protocol"
	"github.com/drpcorg/dsheltie/internal/upstreams"
	"github.com/drpcorg/dsheltie/internal/upstreams/flow"
	"github.com/drpcorg/dsheltie/internal/upstreams/fork_choice"
	"github.com/drpcorg/dsheltie/internal/upstreams/methods"
	"github.com/drpcorg/dsheltie/pkg/chains"
	"github.com/drpcorg/dsheltie/pkg/test_utils/mocks"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestBaseStrategyNoUpstreamsThenError(t *testing.T) {
	chSupervisor := upstreams.NewChainSupervisor(context.Background(), chains.POLYGON, fork_choice.NewHeightForkChoice())
	baseStrategy := flow.NewBaseStrategy(chSupervisor)

	_, err := baseStrategy.SelectUpstream(nil)

	assert.NotNil(t, err)
	assert.Equal(t, protocol.NoAvailableUpstreamsError(), err)
}

func TestBaseStrategyGetUpstreams(t *testing.T) {
	chSup := createChainSupervisor()
	publishEvent(chSup, "id1", protocol.Available)
	publishEvent(chSup, "id2", protocol.Available)
	request := protocol.NewHttpUpstreamRequest("eth_getBalance", nil, nil)
	baseStrategy := flow.NewBaseStrategy(chSup)

	upId, err := baseStrategy.SelectUpstream(request)

	assert.Nil(t, err)
	assert.Equal(t, "id2", upId)

	upId, err = baseStrategy.SelectUpstream(request)

	assert.Nil(t, err)
	assert.Equal(t, "id1", upId)

	_, err = baseStrategy.SelectUpstream(request)

	assert.NotNil(t, err)
	assert.Equal(t, protocol.NoAvailableUpstreamsError(), err)
}

func TestBaseStrategyNoAvailableUpstreams(t *testing.T) {
	chSup := createChainSupervisor()
	publishEvent(chSup, "id1", protocol.Unavailable)
	baseStrategy := flow.NewBaseStrategy(chSup)
	request := protocol.NewHttpUpstreamRequest("eth_getBalance", nil, nil)

	_, err := baseStrategy.SelectUpstream(request)

	assert.NotNil(t, err)
	assert.Equal(t, protocol.NoAvailableUpstreamsError(), err)
}

func TestBaseStrategyNotSupportedMethod(t *testing.T) {
	chSup := createChainSupervisor()
	publishEvent(chSup, "id1", protocol.Unavailable)
	baseStrategy := flow.NewBaseStrategy(chSup)
	request := protocol.NewHttpUpstreamRequest("test", nil, nil)

	_, err := baseStrategy.SelectUpstream(request)

	assert.NotNil(t, err)
	assert.Equal(t, protocol.NotSupportedMethodError("test"), err)
}

func createChainSupervisor() *upstreams.ChainSupervisor {
	chainSupervisor := upstreams.NewChainSupervisor(context.Background(), chains.ARBITRUM, fork_choice.NewHeightForkChoice())

	go chainSupervisor.Start()

	return chainSupervisor
}

func publishEvent(chainSupervisor *upstreams.ChainSupervisor, upId string, status protocol.AvailabilityStatus) {
	methodsMock := mocks.NewMethodsMock()
	methodsMock.On("GetSupportedMethods").Return(mapset.NewThreadUnsafeSet[string]("eth_getBalance"))
	methodsMock.On("HasMethod", "eth_getBalance").Return(true)
	methodsMock.On("HasMethod", "test").Return(false)
	chainSupervisor.Publish(createEvent(upId, status, 100, methodsMock))
	time.Sleep(10 * time.Millisecond)
}

func createEvent(id string, status protocol.AvailabilityStatus, height uint64, methods methods.Methods) protocol.UpstreamEvent {
	return protocol.UpstreamEvent{
		Id: id,
		State: &protocol.UpstreamState{
			Status: status,
			HeadData: &protocol.BlockData{
				Height: height,
			},
			UpstreamMethods: methods,
		},
	}
}
