package config

import (
	"github.com/samber/lo"
	"time"
)

var defaultHedge = &HedgeConfig{
	Delay: 1 * time.Second,
	Count: 1,
}

func (a *AppConfig) setDefaults() {
	if a.UpstreamConfig == nil {
		a.UpstreamConfig = &UpstreamConfig{}
	}
	if a.ServerConfig == nil {
		a.ServerConfig = &ServerConfig{
			Port: 9090,
		}
	}
	if a.CacheConfig != nil {
		a.CacheConfig.setDefaults()
	}
	a.UpstreamConfig.setDefaults()
}

func (c *CacheConfig) setDefaults() {
	for _, connector := range c.CacheConnectors {
		connector.setDefaults()
	}
	for _, policy := range c.CachePolicies {
		policy.setDefaults()
	}
}

func (p *CachePolicyConfig) setDefaults() {
	if p.ObjectMaxSize == "" {
		p.ObjectMaxSize = "500KB"
	}
	if p.FinalizationType == "" {
		p.FinalizationType = None
	}
	if p.TTL == "" {
		p.TTL = "10m"
	}
	if p.TTL == "0" {
		p.TTL = "0s"
	}
}

func (c *CacheConnectorConfig) setDefaults() {
	switch c.Driver {
	case Memory:
		if c.Memory == nil {
			c.Memory = &MemoryCacheConnectorConfig{}
		}
		if c.Memory.MaxItems == 0 {
			c.Memory.MaxItems = 10000
		}
		if c.Memory.ExpiredRemoveInterval == 0 {
			c.Memory.ExpiredRemoveInterval = 30 * time.Second
		}
	}
}

func (u *UpstreamConfig) setDefaults() {
	if u.FailsafeConfig == nil {
		u.FailsafeConfig = &FailsafeConfig{
			HedgeConfig: defaultHedge,
		}
	} else {
		if u.FailsafeConfig.HedgeConfig == nil {
			u.FailsafeConfig.HedgeConfig = defaultHedge
		}
	}
	for _, upstream := range u.Upstreams {
		chainDefaults := u.ChainDefaults[upstream.ChainName]
		upstream.setDefaults(chainDefaults)
	}
}

func (u *Upstream) setDefaults(defaults *ChainDefaults) {
	if u.Methods == nil {
		u.Methods = &MethodsConfig{}
	}
	if u.HeadConnector == "" && len(u.Connectors) > 0 {
		filteredConnectors := lo.Filter(u.Connectors, func(item *ApiConnectorConfig, index int) bool {
			_, ok := connectorTypesRating[item.Type]
			return ok
		})

		if len(filteredConnectors) > 0 {
			defaultHeadConnectorType := lo.MinBy(filteredConnectors, func(a *ApiConnectorConfig, b *ApiConnectorConfig) bool {
				return connectorTypesRating[a.Type] < connectorTypesRating[b.Type]
			}).Type
			u.HeadConnector = defaultHeadConnectorType
		}
	}
	if u.PollInterval == 0 {
		u.PollInterval = 1 * time.Minute
	}
	if defaults != nil {
		if defaults.PollInterval != 0 {
			u.PollInterval = defaults.PollInterval
		}
	}
}
