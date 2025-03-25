package upstreams

import (
	"context"
	"fmt"
	"github.com/drpcorg/dshaltie/internal/protocol"
	choice "github.com/drpcorg/dshaltie/internal/upstreams/fork_choice"
	"github.com/drpcorg/dshaltie/pkg/chains"
	"github.com/drpcorg/dshaltie/pkg/utils"
	"github.com/rs/zerolog/log"
	"strings"
	"time"
)

type ChainSupervisor struct {
	ctx            context.Context
	chain          chains.Chain
	fc             choice.ForkChoice
	state          *utils.Atomic[ChainSupervisorState]
	eventsChan     chan protocol.UpstreamEvent
	upstreamStates utils.CMap[string, protocol.UpstreamState]
}

type ChainSupervisorState struct {
	Status protocol.AvailabilityStatus
	Head   uint64
}

func NewChainSupervisor(ctx context.Context, chain chains.Chain, fc choice.ForkChoice) *ChainSupervisor {
	state := utils.NewAtomic[ChainSupervisorState]()
	state.Store(ChainSupervisorState{Status: protocol.Available})

	return &ChainSupervisor{
		ctx:            ctx,
		chain:          chain,
		fc:             fc,
		eventsChan:     make(chan protocol.UpstreamEvent, 100),
		upstreamStates: utils.CMap[string, protocol.UpstreamState]{},
		state:          utils.NewAtomic[ChainSupervisorState](),
	}
}

func (c *ChainSupervisor) Start() {
	go c.processEvents()

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}

			c.monitor()
		}
	}()
}

func (c *ChainSupervisor) Publish(event protocol.UpstreamEvent) {
	c.eventsChan <- event
}

func (c *ChainSupervisor) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-c.eventsChan:
			if ok {
				state := c.state.Load()
				c.upstreamStates.Store(event.Id, event.State)

				updated, headHeight := c.fc.Choose(event)
				if updated {
					state.Head = headHeight
				}
				state.Status = c.processUpstreamsStatus()

				c.state.Store(state)
			}
		}
	}
}

func (c *ChainSupervisor) processUpstreamsStatus() protocol.AvailabilityStatus {
	var status protocol.AvailabilityStatus
	c.upstreamStates.Range(func(upId string, upState *protocol.UpstreamState) bool {
		if upState.Status < status {
			status = upState.Status
		}
		return true
	})

	return status
}

func (c *ChainSupervisor) monitor() {
	state := c.state.Load()

	var height string
	if state.Head > 0 {
		height = fmt.Sprintf("%d", state.Head)
	} else {
		height = "?"
	}

	statuses := make(map[protocol.AvailabilityStatus]int)
	c.upstreamStates.Range(func(key string, upState *protocol.UpstreamState) bool {
		statuses[upState.Status]++

		return true
	})

	upstreamStatuses, weakUpstreams := c.getStatuses()

	log.Info().Msgf("State of %s: height=%s, statuses=[%s], weak=[%s]", strings.ToUpper(c.chain.String()), height, upstreamStatuses, weakUpstreams)
}

func (c *ChainSupervisor) getStatuses() (string, string) {
	statuses := make(map[protocol.AvailabilityStatus]int)
	weakUpstreams := make([]string, 0)
	c.upstreamStates.Range(func(upId string, upState *protocol.UpstreamState) bool {
		statuses[upState.Status]++
		if upState.Status != protocol.Available {
			weakUpstreams = append(weakUpstreams, upId)
		}

		return true
	})

	if len(statuses) == 0 {
		return "", ""
	}
	statusPairs := make([]string, 0)
	for key, value := range statuses {
		statusPairs = append(statusPairs, fmt.Sprintf("%s/%d", key, value))
	}

	return strings.Join(statusPairs, ", "), strings.Join(weakUpstreams, ", ")
}
