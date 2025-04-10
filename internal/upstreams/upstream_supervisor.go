package upstreams

import (
	"context"
	"fmt"
	"github.com/drpcorg/dshaltie/internal/config"
	"github.com/drpcorg/dshaltie/internal/protocol"
	choice "github.com/drpcorg/dshaltie/internal/upstreams/fork_choice"
	"github.com/drpcorg/dshaltie/pkg/chains"
	"github.com/drpcorg/dshaltie/pkg/utils"
	"github.com/failsafe-go/failsafe-go"
)

type UpstreamSupervisor interface {
	GetChainSupervisor(chain chains.Chain) *ChainSupervisor
	GetUpstream(string) *Upstream
	GetExecutor() failsafe.Executor[*protocol.ResponseHolderWrapper]
	StartUpstreams()
}

type BaseUpstreamSupervisor struct {
	ctx              context.Context
	chainSupervisors utils.CMap[chains.Chain, ChainSupervisor]
	upstreams        utils.CMap[string, Upstream]
	eventsChan       chan protocol.UpstreamEvent
	upstreamsConfig  *config.UpstreamConfig
	executor         failsafe.Executor[*protocol.ResponseHolderWrapper]
}

func NewBaseUpstreamSupervisor(ctx context.Context, upstreamsConfig *config.UpstreamConfig) UpstreamSupervisor {
	return &BaseUpstreamSupervisor{
		ctx:              ctx,
		upstreams:        utils.CMap[string, Upstream]{},
		chainSupervisors: utils.CMap[chains.Chain, ChainSupervisor]{},
		eventsChan:       make(chan protocol.UpstreamEvent, 100),
		upstreamsConfig:  upstreamsConfig,
		executor:         createExecutor(createHedgePolicy(upstreamsConfig.FailsafeConfig.HedgeConfig)),
	}
}

func (u *BaseUpstreamSupervisor) GetChainSupervisor(chain chains.Chain) *ChainSupervisor {
	if c, ok := u.chainSupervisors.Load(chain); ok {
		return c
	}
	return nil
}

func (u *BaseUpstreamSupervisor) GetUpstream(upstreamId string) *Upstream {
	if up, ok := u.upstreams.Load(upstreamId); ok {
		return up
	}
	return nil
}

func (u *BaseUpstreamSupervisor) GetExecutor() failsafe.Executor[*protocol.ResponseHolderWrapper] {
	return u.executor
}

func (u *BaseUpstreamSupervisor) StartUpstreams() {
	go u.processEvents()

	for _, upConfig := range u.upstreamsConfig.Upstreams {
		go func() {
			up := NewUpstream(u.ctx, upConfig)
			up.Start()

			u.upstreams.Store(up.Id, up)

			upSub := up.Subscribe(fmt.Sprintf("upstream_supervisor_%s_updates", up.Id))
			defer upSub.Unsubscribe()

			for {
				select {
				case <-u.ctx.Done():
					return
				case upstreamEvent, ok := <-upSub.Events:
					if ok {
						u.eventsChan <- upstreamEvent
					}
				}
			}
		}()
	}
}

func (u *BaseUpstreamSupervisor) processEvents() {
	for {
		select {
		case <-u.ctx.Done():
			return
		case event, ok := <-u.eventsChan:
			if ok {
				chainSupervisor, exists := u.chainSupervisors.LoadOrStore(event.Chain, NewChainSupervisor(u.ctx, event.Chain, choice.NewHeightForkChoice()))

				if !exists {
					chainSupervisor.Start()
				}

				chainSupervisor.Publish(event)
			}
		}
	}
}
