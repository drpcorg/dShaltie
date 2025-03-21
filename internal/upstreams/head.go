package upstreams

import (
	"context"
	"github.com/drpcorg/dshaltie/internal/config"
	"github.com/drpcorg/dshaltie/internal/protocol"
	"github.com/drpcorg/dshaltie/internal/upstreams/chains_specific"
	"github.com/drpcorg/dshaltie/internal/upstreams/connectors"
	"github.com/drpcorg/dshaltie/pkg/chains"
	"github.com/drpcorg/dshaltie/pkg/utils"
	"github.com/rs/zerolog/log"
	"sync/atomic"
	"time"
)

type HeadProcessor struct {
	upstreamId           string
	ctx                  context.Context
	head                 Head
	lastUpdate           *utils.Atomic[time.Time]
	headNoUpdatesTimeout time.Duration
}

func NewHeadProcessor(
	ctx context.Context,
	upConfig *config.Upstream,
	configuredChain chains.ConfiguredChain,
	apiConnector connectors.ApiConnector,
	specific specific.ChainSpecific,
) *HeadProcessor {
	head := createHead(ctx, upConfig.Id, upConfig.PollInterval, apiConnector, specific)

	headNoUpdatesTimeout := 1 * time.Minute
	switch head.(type) {
	case *RpcHead:
		if upConfig.PollInterval >= headNoUpdatesTimeout {
			headNoUpdatesTimeout = upConfig.PollInterval * 3
		}
	case *SubscriptionHead:
		if configuredChain.Settings.ExpectedBlockTime >= headNoUpdatesTimeout {
			headNoUpdatesTimeout = configuredChain.Settings.ExpectedBlockTime + headNoUpdatesTimeout
		}
	}

	return &HeadProcessor{
		upstreamId:           upConfig.Id,
		head:                 head,
		ctx:                  ctx,
		headNoUpdatesTimeout: headNoUpdatesTimeout,
		lastUpdate:           utils.NewAtomic[time.Time](),
	}
}

func (h *HeadProcessor) Start() {
	go h.head.Start()
	h.lastUpdate.Store(time.Now())

	timeout := time.NewTimer(h.headNoUpdatesTimeout)
	for {
		select {
		case <-timeout.C:
			difference := time.Since(h.lastUpdate.Load())
			log.Warn().Msgf("No head updates of upstream %s for %d ms", h.upstreamId, difference.Milliseconds())
			h.head.OnNoHeadUpdates()
		case <-h.ctx.Done():
			return
		case block, ok := <-h.head.HeadsChan():
			if ok {
				h.lastUpdate.Store(time.Now())
				// process events with heads
				log.Debug().Msgf("got a new head - %d", block.Height)
			}
		}
		timeout.Reset(h.headNoUpdatesTimeout)
	}
}

func createHead(ctx context.Context, id string, pollInterval time.Duration, apiConnector connectors.ApiConnector, specific specific.ChainSpecific) Head {
	switch apiConnector.GetType() {
	case protocol.JsonRpcConnector, protocol.RestConnector:
		log.Info().Msgf("starting an rpc head of upstream %s with poll interval %s", id, pollInterval)
		return NewRpcHead(ctx, id, apiConnector, specific, pollInterval)
	case protocol.WsConnector:
		log.Info().Msgf("starting a ws head of upstream %s", id)
		return NewWsHead(ctx, id, apiConnector, specific)
	default:
		return nil
	}
}

type Head interface {
	GetCurrentHeight() uint64
	Start()
	HeadsChan() chan *protocol.Block
	OnNoHeadUpdates()
}

type RpcHead struct {
	ctx            context.Context
	block          utils.Atomic[protocol.Block]
	chainSpecific  specific.ChainSpecific
	pollInterval   time.Duration
	connector      connectors.ApiConnector
	upstreamId     string
	pollInProgress atomic.Bool
	headsChan      chan *protocol.Block
}

var _ Head = (*RpcHead)(nil)

func (r *RpcHead) GetCurrentHeight() uint64 {
	return r.block.Load().Height
}

func NewRpcHead(ctx context.Context, upstreamId string, connector connectors.ApiConnector, chainSpecific specific.ChainSpecific, pollInterval time.Duration) *RpcHead {
	head := RpcHead{
		ctx:            ctx,
		block:          *utils.NewAtomic[protocol.Block](),
		chainSpecific:  chainSpecific,
		pollInterval:   pollInterval,
		connector:      connector,
		upstreamId:     upstreamId,
		pollInProgress: atomic.Bool{},
		headsChan:      make(chan *protocol.Block),
	}

	return &head
}

func (r *RpcHead) Start() {
	for {
		r.poll()
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(r.pollInterval):
		}
	}
}

func (r *RpcHead) HeadsChan() chan *protocol.Block {
	return r.headsChan
}

func (r *RpcHead) OnNoHeadUpdates() {
}

func (r *RpcHead) poll() {
	if !r.pollInProgress.Load() {
		r.pollInProgress.Store(true)
		defer r.pollInProgress.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		block, err := r.chainSpecific.GetLatestBlock(ctx, r.connector)
		if err != nil {
			log.Warn().Err(err).Msgf("couldn't get the latest block of upstream %s", r.upstreamId)
		} else {
			r.block.Store(*block)
			r.headsChan <- block
		}
	}
}

type SubscriptionHead struct {
	ctx             context.Context
	block           *utils.Atomic[protocol.Block]
	chainSpecific   specific.ChainSpecific
	connector       connectors.ApiConnector
	upstreamId      string
	headsChan       chan *protocol.Block
	stopped         chan struct{}
	startInProgress atomic.Bool
	headStopped     atomic.Bool
}

var _ Head = (*SubscriptionHead)(nil)

func (w *SubscriptionHead) GetCurrentHeight() uint64 {
	return w.block.Load().Height
}

func (w *SubscriptionHead) Start() {
	if !w.startInProgress.Load() {
		w.startInProgress.Store(true)
		defer w.startInProgress.Store(false)

		subReq, err := w.chainSpecific.SubscribeHeadRequest()
		if err != nil {
			log.Warn().Err(err).Msgf("couldn't create a subscription request to upstream %s", w.upstreamId)
			return
		}

		ctx, cancel := context.WithCancel(w.ctx)
		subResponse, err := w.connector.Subscribe(ctx, subReq)
		if err != nil {
			log.Warn().Err(err).Msgf("couldn't subscribe to upstream %s heads", w.upstreamId)
			cancel()
			return
		}
		w.headStopped.Store(false)
		go w.processMessages(subResponse, cancel)
	}
}

func (w *SubscriptionHead) HeadsChan() chan *protocol.Block {
	return w.headsChan
}

func (w *SubscriptionHead) OnNoHeadUpdates() {
	if !w.headStopped.Load() {
		w.stopped <- struct{}{}
	}

	log.Info().Msgf("trying to resubscribe to new heads of upstream %s", w.upstreamId)
	go w.Start()
}

func (w *SubscriptionHead) processMessages(subResponse protocol.UpstreamSubscriptionResponse, cancelFunc context.CancelFunc) {
	defer cancelFunc()
	for {
		select {
		case message, ok := <-subResponse.ResponseChan():
			if !ok {
				return
			}
			if message.Error != nil {
				log.Warn().Err(message.Error).Msgf("got an error from heads subscription of upstream %s", w.upstreamId)
				return
			}
			if message.Type == protocol.Ws {
				block, err := w.chainSpecific.ParseSubscriptionBlock(message.Message)
				if err != nil {
					log.Warn().Err(err).Msgf("couldn't parse a message from heads subscription of upstream %s", w.upstreamId)
					return
				}
				w.block.Store(*block)
				w.headsChan <- block
			}
		case <-w.ctx.Done():
			return
		case <-w.stopped:
			w.headStopped.Store(true)
			return
		}
	}
}

func NewWsHead(ctx context.Context, upstreamId string, connector connectors.ApiConnector, chainSpecific specific.ChainSpecific) *SubscriptionHead {
	head := SubscriptionHead{
		ctx:             ctx,
		upstreamId:      upstreamId,
		chainSpecific:   chainSpecific,
		connector:       connector,
		block:           utils.NewAtomic[protocol.Block](),
		headsChan:       make(chan *protocol.Block),
		stopped:         make(chan struct{}),
		startInProgress: atomic.Bool{},
		headStopped:     atomic.Bool{},
	}

	return &head
}
