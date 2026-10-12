package service

import (
	"context"
	"sort"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/pkg/pagination"
)

// AICatalogChannels 是目录服务对渠道服务的依赖，由 *AIChannelService 实现。
type AICatalogChannels interface {
	List(ctx context.Context) ([]ChannelView, error)
}

// AICatalogPlugins 是目录服务对插件服务的依赖，由 *AIPluginService 实现。
type AICatalogPlugins interface {
	List(ctx context.Context) ([]PluginView, error)
}

// AICatalogModels 是目录服务对模型服务的依赖，由 *AIConfigService 实现。
type AICatalogModels interface {
	ListConfigs(ctx context.Context) ([]ConfigListItem, error)
}

// AICatalogService 提供后台“渠道 / 插件 / 模型”三个列表的分页、搜索、筛选和状态：
// 状态要跨三类配置算（渠道健康要插件，模型状态要渠道），所以单独成一个只读服务。
// 数据量小，每次读全量，在内存里算好状态再过滤、取当页。
type AICatalogService struct {
	channels AICatalogChannels
	plugins  AICatalogPlugins
	models   AICatalogModels
}

// NewAICatalogService 创建目录服务。
func NewAICatalogService(channels AICatalogChannels, plugins AICatalogPlugins, models AICatalogModels) *AICatalogService {
	return &AICatalogService{channels: channels, plugins: plugins, models: models}
}

// catalog 是一次请求里读到的三份全量数据，加上从中派生出的状态判定用结构。
type catalog struct {
	channels []ChannelView
	plugins  []PluginView
	models   []ConfigListItem

	cPlugins  []CatalogPlugin
	cChannels []CatalogChannel
	cModels   []CatalogModel
}

func (s *AICatalogService) load(ctx context.Context) (*catalog, error) {
	channels, err := s.channels.List(ctx)
	if err != nil {
		return nil, err
	}
	plugins, err := s.plugins.List(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.models.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	c := &catalog{channels: channels, plugins: plugins, models: models}
	for _, p := range plugins {
		cp := CatalogPlugin{Key: p.Key, Name: p.Name, Enabled: p.Enabled}
		for _, v := range p.Versions {
			cp.Versions = append(cp.Versions, catalogVersion(v))
		}
		c.cPlugins = append(c.cPlugins, cp)
	}
	for _, ch := range channels {
		c.cChannels = append(c.cChannels, CatalogChannel{Key: ch.Key, Name: ch.Name, PluginKey: ch.PluginKey,
			PluginVersionID: ch.PluginVersionID, PluginVersion: ch.PluginVersion, Enabled: ch.Enabled, SecretSet: ch.SecretSet})
	}
	for _, m := range models {
		c.cModels = append(c.cModels, CatalogModel{Key: m.Key, Kind: m.Kind, Channel: m.Channel, Enabled: m.Enabled})
	}
	return c, nil
}

// catalogVersion 把插件版本视图换成状态判定用的结构；meta 读不到时 AuthType 为空串（未知）。
func catalogVersion(v PluginVersionView) CatalogVersion {
	cv := CatalogVersion{ID: v.ID, Version: v.Version}
	if v.Meta == nil {
		return cv
	}
	cv.AuthType = v.Meta.Auth.Type
	if cv.AuthType == "" {
		cv.AuthType = "none" // meta 读出来了但没写鉴权：不需要 Key
	}
	for kind := range v.Meta.Endpoints {
		cv.Kinds = append(cv.Kinds, kind)
	}
	sort.Strings(cv.Kinds)
	return cv
}

// pluginOf 按 key 找插件，返回在 cPlugins 里的下标；找不到为 -1。
func (c *catalog) pluginOf(key string) int {
	for i := range c.cPlugins {
		if c.cPlugins[i].Key == key {
			return i
		}
	}
	return -1
}

// channelVersion 返回渠道固定的插件版本；找不到为 nil。
func (c *catalog) channelVersion(ch CatalogChannel) *CatalogVersion {
	i := c.pluginOf(ch.PluginKey)
	if i < 0 {
		return nil
	}
	return versionOf(&c.cPlugins[i], ch.PluginVersionID, ch.PluginVersion)
}

// channelKinds 返回渠道所用插件版本声明支持的生成方式（含 agent）；版本读不到为空。
func (c *catalog) channelKinds(ch CatalogChannel) []string {
	v := c.channelVersion(ch)
	if v == nil {
		return []string{}
	}
	out := append([]string{}, v.Kinds...)
	if MetaSupportsKind(*v, "agent") {
		out = append(out, "agent")
	}
	return out
}

// upgradeTo 返回渠道所属插件里比它固定版本更新的最高版本号；没有为空串。
func (c *catalog) upgradeTo(ch CatalogChannel) string {
	i := c.pluginOf(ch.PluginKey)
	if i < 0 {
		return ""
	}
	return AvailableUpgrade(c.cPlugins[i], ch.PluginVersion)
}

// ---- 渠道 ----

// CatalogChannelItem 是渠道列表的一行：渠道视图 + 健康状态、可升级版本、能力、模型数。
type CatalogChannelItem struct {
	ChannelView
	Health           ChannelHealth `json:"health"`
	UpgradeTo        string        `json:"upgrade_to"` // 有更新版本时是最高版本号，否则为空串
	Kinds            []string      `json:"kinds"`      // 所用插件版本支持的生成方式
	ModelCount       int           `json:"model_count"`
	OnlineModelCount int           `json:"online_model_count"` // 已上架的模型数
}

// CatalogChannelPage 是渠道列表的一页。
type CatalogChannelPage struct {
	pagination.Result[CatalogChannelItem]
}

func (c *catalog) channelItem(i int) CatalogChannelItem {
	ch := c.cChannels[i]
	item := CatalogChannelItem{
		ChannelView: c.channels[i], Health: ChannelHealthOf(ch, c.cPlugins),
		UpgradeTo: c.upgradeTo(ch), Kinds: c.channelKinds(ch),
	}
	for _, m := range c.cModels {
		if m.Channel == ch.Key {
			item.ModelCount++
			if m.Enabled {
				item.OnlineModelCount++
			}
		}
	}
	return item
}

// ListChannels 按关键字（名称 / key / 接口地址）、能力、插件和状态筛选并分页返回渠道。
// 能力按渠道所用插件版本的声明筛；读不到版本时不当成“不支持”，免得整张表被筛空。
func (s *AICatalogService) ListChannels(ctx context.Context, req *model.ListChannelReq) (*CatalogChannelPage, error) {
	c, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]CatalogChannelItem, 0, len(c.channels))
	for i := range c.channels {
		items = append(items, c.channelItem(i))
	}
	found := pagination.Paginate(items, pagination.AdminQuery(req.Page, req.PageSize), func(it CatalogChannelItem) bool {
		if !pagination.MatchKeyword(req.Q, it.Name, it.Key, it.BaseURL) {
			return false
		}
		if req.Plugin != "" && it.PluginKey != req.Plugin {
			return false
		}
		if req.Kind != "" && len(it.Kinds) > 0 && !contains(it.Kinds, req.Kind) {
			return false
		}
		switch req.Status {
		case "":
			return true
		case "upgrade":
			return it.UpgradeTo != ""
		default:
			return it.Health.Tone == req.Status
		}
	})
	return &CatalogChannelPage{Result: found}, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ChannelOption 是弹窗下拉和概览用的渠道视图（全部渠道，不分页）：完整渠道视图 + 健康状态和能力。
type ChannelOption struct {
	ChannelView
	Health ChannelHealth `json:"health"`
	Kinds  []string      `json:"kinds"`
}

// ChannelOptions 返回全部渠道（不分页），给模型弹窗选渠道、批量迁移、导入、升级和概览页用。
func (s *AICatalogService) ChannelOptions(ctx context.Context) ([]ChannelOption, error) {
	c, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelOption, 0, len(c.channels))
	for i, ch := range c.channels {
		cc := c.cChannels[i]
		out = append(out, ChannelOption{ChannelView: ch, Health: ChannelHealthOf(cc, c.cPlugins), Kinds: c.channelKinds(cc)})
	}
	return out, nil
}

// ---- 插件 ----

// CatalogPluginItem 是插件列表的一行：插件视图（带全部版本）+ 渠道数、旧版渠道数、在线模型数、最新版本。
type CatalogPluginItem struct {
	PluginView
	LatestVersion        string `json:"latest_version"`         // 按 semver 最高的版本号；没有版本为空串
	ChannelCount         int    `json:"channel_count"`          // 在用这个插件的渠道数
	OutdatedChannelCount int    `json:"outdated_channel_count"` // 其中固定在旧版本上的渠道数
	OnlineModelCount     int    `json:"online_model_count"`     // 这些渠道下已上架的模型数
}

// CatalogPluginPage 是插件列表的一页。
type CatalogPluginPage struct {
	pagination.Result[CatalogPluginItem]
}

// latestOf 返回插件里 semver 最高的版本。
func latestOf(p CatalogPlugin) (CatalogVersion, bool) {
	var best CatalogVersion
	found := false
	for _, v := range p.Versions {
		if !found || compareSemver(v.Version, best.Version) > 0 {
			best, found = v, true
		}
	}
	return best, found
}

func (c *catalog) pluginItem(i int) CatalogPluginItem {
	p := c.cPlugins[i]
	item := CatalogPluginItem{PluginView: c.plugins[i]}
	if latest, ok := latestOf(p); ok {
		item.LatestVersion = latest.Version
	}
	for _, ch := range c.cChannels {
		if ch.PluginKey != p.Key {
			continue
		}
		item.ChannelCount++
		if AvailableUpgrade(p, ch.PluginVersion) != "" {
			item.OutdatedChannelCount++
		}
		for _, m := range c.cModels {
			if m.Channel == ch.Key && m.Enabled {
				item.OnlineModelCount++
			}
		}
	}
	return item
}

// ListPlugins 按关键字（名称 / key）、能力（按最新版本的声明）、来源和状态筛选并分页返回插件。
func (s *AICatalogService) ListPlugins(ctx context.Context, req *model.ListPluginReq) (*CatalogPluginPage, error) {
	c, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]CatalogPluginItem, 0, len(c.plugins))
	for i := range c.plugins {
		items = append(items, c.pluginItem(i))
	}
	found := pagination.Paginate(items, pagination.AdminQuery(req.Page, req.PageSize), func(it CatalogPluginItem) bool {
		if !pagination.MatchKeyword(req.Q, it.Name, it.Key) {
			return false
		}
		if req.Source != "" && it.Source != req.Source {
			return false
		}
		if req.Kind != "" {
			p := c.cPlugins[c.pluginOf(it.Key)]
			latest, ok := latestOf(p)
			if !ok || !MetaSupportsKind(latest, req.Kind) {
				return false
			}
		}
		switch req.Status {
		case "on":
			return it.Enabled
		case "off":
			return !it.Enabled
		case "outdated":
			return it.OutdatedChannelCount > 0
		}
		return true
	})
	return &CatalogPluginPage{Result: found}, nil
}

// PluginOptions 返回全部插件（带全部版本和 meta），不分页，给渠道弹窗选插件 / 版本、渲染设置项用。
func (s *AICatalogService) PluginOptions(ctx context.Context) ([]PluginView, error) {
	return s.plugins.List(ctx)
}

// GetPlugin 返回一个插件（带全部版本），不存在返回 ErrPluginNotFound；深链 ?key= 打开插件弹窗时用。
func (s *AICatalogService) GetPlugin(ctx context.Context, key string) (*PluginView, error) {
	list, err := s.plugins.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Key == key {
			return &list[i], nil
		}
	}
	return nil, errcode.ErrPluginNotFound
}

// ---- 模型 ----

// CatalogModelItem 是模型列表的一行：列表项 + 状态、原因和渠道名。
type CatalogModelItem struct {
	ConfigListItem
	Health      ModelHealth `json:"health"`
	ChannelName string      `json:"channel_name"` // 绑定渠道的显示名；没绑或渠道不存在为空串
}

// CatalogModelPage 是模型列表的一页。
type CatalogModelPage struct {
	pagination.Result[CatalogModelItem]
}

// ListModels 按关键字（key / 展示名 / 厂商 / 标签 / 渠道名）、类型、渠道和状态筛选并分页返回模型。
// 状态是跨渠道和插件算的：在线 / 不可用（已上架但渠道用不了）/ 未上线。
func (s *AICatalogService) ListModels(ctx context.Context, req *model.ListModelReq) (*CatalogModelPage, error) {
	c, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(c.channels))
	for _, ch := range c.channels {
		names[ch.Key] = ch.Name
	}
	items := make([]CatalogModelItem, 0, len(c.models))
	for i, m := range c.models {
		items = append(items, CatalogModelItem{
			ConfigListItem: m, Health: ModelHealthOf(c.cModels[i], c.cChannels, c.cPlugins), ChannelName: names[m.Channel],
		})
	}
	found := pagination.Paginate(items, pagination.AdminQuery(req.Page, req.PageSize), func(it CatalogModelItem) bool {
		fields := append([]string{it.Key, it.Label, it.Vendor, it.ChannelName}, it.Tags...)
		if !pagination.MatchKeyword(req.Q, fields...) {
			return false
		}
		if req.Kind != "" && it.Kind != req.Kind {
			return false
		}
		if req.Channel != "" && it.Channel != req.Channel {
			return false
		}
		return req.Status == "" || it.Health.Status == req.Status
	})
	return &CatalogModelPage{Result: found}, nil
}

// ModelOptions 返回全部模型的列表项（不含状态），不分页：概览等需要“有哪些模型、绑在哪个渠道”的页面用。
func (s *AICatalogService) ModelOptions(ctx context.Context) ([]ConfigListItem, error) {
	return s.models.ListConfigs(ctx)
}
