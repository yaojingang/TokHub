package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInvalidRecommendConfig = errors.New("invalid recommendation config")

const recommendPickLimit = 3
const defaultRecommendCTALabel = "去官方体验"

type RecommendValidationError struct {
	Message string
}

func (e RecommendValidationError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return ErrInvalidRecommendConfig.Error()
	}
	return e.Message
}

func (e RecommendValidationError) Unwrap() error {
	return ErrInvalidRecommendConfig
}

type RecommendPick struct {
	ID        string        `json:"id"`
	ChannelID string        `json:"channelId"`
	Position  int           `json:"position"`
	Title     string        `json:"title"`
	Ribbon    string        `json:"ribbon"`
	Summary   string        `json:"summary"`
	Points    []string      `json:"points"`
	CTALabel  string        `json:"ctaLabel"`
	CTAURL    string        `json:"ctaUrl"`
	Enabled   bool          `json:"enabled"`
	Clicks    int           `json:"clicks"`
	CTR       float64       `json:"ctr"`
	Channel   PublicChannel `json:"channel"`
}

type RecommendReward struct {
	ID            string `json:"id"`
	ChannelID     string `json:"channelId"`
	ProviderName  string `json:"providerName"`
	RewardType    string `json:"rewardType"`
	RewardValue   string `json:"rewardValue"`
	Code          string `json:"code"`
	ExpiresAtText string `json:"expiresAtText"`
	Enabled       bool   `json:"enabled"`
	Clicks        int    `json:"clicks"`
}

type RecommendScenario struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Icon      string        `json:"icon"`
	ChannelID string        `json:"channelId"`
	Summary   string        `json:"summary"`
	Position  int           `json:"position"`
	Enabled   bool          `json:"enabled"`
	Clicks    int           `json:"clicks"`
	Channel   PublicChannel `json:"channel"`
}

type RecommendRankRule struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Metric      string `json:"metric"`
	Position    int    `json:"position"`
	Enabled     bool   `json:"enabled"`
}

type RecommendStats struct {
	Picks        int     `json:"picks"`
	Ranked       int     `json:"ranked"`
	Rewards      int     `json:"rewards"`
	Scenarios    int     `json:"scenarios"`
	Clicks       int     `json:"clicks"`
	CTR          float64 `json:"ctr"`
	AverageScore int     `json:"averageScore"`
}

type RecommendConfig struct {
	Stats     RecommendStats      `json:"stats"`
	Picks     []RecommendPick     `json:"picks"`
	Ranks     []RecommendPick     `json:"ranks"`
	RankRules []RecommendRankRule `json:"rankRules"`
	Rewards   []RecommendReward   `json:"rewards"`
	Scenarios []RecommendScenario `json:"scenarios"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

type RecommendAdminData struct {
	RecommendConfig
	Channels []PublicChannel `json:"channels"`
}

type RecommendSaveInput struct {
	Picks     []RecommendPick      `json:"picks"`
	RankRules *[]RecommendRankRule `json:"rankRules,omitempty"`
	Rewards   []RecommendReward    `json:"rewards"`
	Scenarios []RecommendScenario  `json:"scenarios"`
}

type OpenAPISite struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	SiteKeyPrefix string     `json:"siteKeyPrefix"`
	SiteKeyMask   string     `json:"siteKeyMask"`
	PlainKey      string     `json:"plainKey,omitempty"`
	Scopes        []string   `json:"scopes"`
	QPSLimit      int        `json:"qpsLimit"`
	Status        string     `json:"status"`
	CallsToday    int        `json:"callsToday"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
}

type OpenAPIEndpoint struct {
	Scope       string `json:"scope"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	CallsToday  int    `json:"callsToday"`
	AverageMs   int    `json:"averageMs"`
	Cache       string `json:"cache"`
	Status      string `json:"status"`
}

type OpenAPICallLog struct {
	ID         string    `json:"id"`
	SiteName   string    `json:"siteName"`
	Endpoint   string    `json:"endpoint"`
	StatusCode int       `json:"statusCode"`
	LatencyMs  int       `json:"latencyMs"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
}

type OpenAPIChannelSyncLog struct {
	ID           string    `json:"id"`
	ChannelID    string    `json:"channelId"`
	ChannelName  string    `json:"channelName"`
	SampledAt    time.Time `json:"sampledAt"`
	Status       string    `json:"status"`
	Score        int       `json:"score"`
	LatencyP95Ms int       `json:"latencyP95Ms"`
	L1Status     string    `json:"l1Status"`
	L2Status     string    `json:"l2Status"`
	L3Status     string    `json:"l3Status"`
	ErrorType    string    `json:"errorType"`
}

type OpenAPISummary struct {
	BaseURL   string            `json:"baseUrl"`
	Endpoints []OpenAPIEndpoint `json:"endpoints"`
	Sites     []OpenAPISite     `json:"sites"`
	Logs      []OpenAPICallLog  `json:"logs"`
	Stats     map[string]int    `json:"stats"`
}

type OpenAPISiteCreateInput struct {
	Name     string
	Scopes   []string
	QPSLimit int
	ActorID  string
}

type AuthenticatedOpenAPISite struct {
	Site OpenAPISite
}

type OpenAPIIncident struct {
	ID         string     `json:"id"`
	ChannelID  string     `json:"channelId"`
	Channel    string     `json:"channel"`
	Status     string     `json:"status"`
	Title      string     `json:"title"`
	OpenedAt   time.Time  `json:"openedAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

func SeedPhase6(ctx context.Context, db DBTX, seedMode string) error {
	if err := seedRecommend(ctx, db, seedMode); err != nil {
		return err
	}
	return nil
}

func seedRecommend(ctx context.Context, db DBTX, seedMode string) error {
	dataOrigin := "demo"
	if seedMode == "test" {
		dataOrigin = "test"
	}
	var existing int
	if err := db.QueryRow(ctx, `select count(*) from recommend_picks`).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	rows, err := db.Query(ctx, `
		select id,name,provider,endpoint,coalesce(official_site_url,''),score
		from channels
		where owner_type='platform'
		order by score desc, name asc
		limit 6
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ch struct {
		id, name, provider, endpoint, officialSiteURL string
		score                                         int
	}
	var channels []ch
	for rows.Next() {
		var item ch
		if err := rows.Scan(&item.id, &item.name, &item.provider, &item.endpoint, &item.officialSiteURL, &item.score); err != nil {
			return err
		}
		channels = append(channels, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ribbons := []string{"编辑首推", "性价比之选", "稳定王"}
	for i, item := range channels {
		if i < 3 {
			points := []string{
				fmt.Sprintf("综合评分 %d，长期监控表现稳定", item.score),
				fmt.Sprintf("%s 模型通道，适合生产和研发场景", item.provider),
				"支持 OpenAI 兼容调用，可纳入 TokHub 企业网关",
				"官方入口与接入说明可由运营后台维护",
			}
			raw, _ := json.Marshal(points)
			if _, err := db.Exec(ctx, `
				insert into recommend_picks(id,channel_id,position,title,ribbon,summary,points_json,cta_label,cta_url,enabled,created_at,updated_at,data_origin)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9,true,now(),now(),$10)
				on conflict(position) do nothing
			`, "rcp_"+uuid.NewString(), item.id, i+1, item.name, ribbons[i], "基于真实 L1/L2/L3 探测和平台评分进入本周精选。", raw, defaultRecommendCTALabel, defaultRecommendCTAURL(PublicChannel{Endpoint: item.endpoint, OfficialSiteURL: item.officialSiteURL}), dataOrigin); err != nil {
				return err
			}
		}
		rewardValue := "以官网为准"
		if _, err := db.Exec(ctx, `
			insert into recommend_rewards(id,channel_id,provider_name,reward_type,reward_value,code,expires_at_text,enabled,created_at,updated_at,data_origin)
			values($1,$2,$3,'接入说明',$4,$5,'长期有效',true,now(),now(),$6)
		`, "rcr_"+uuid.NewString(), item.id, item.name, rewardValue, "OFFICIAL", dataOrigin); err != nil {
			return err
		}
	}
	scenarios := []struct {
		title, icon, summary string
		index                int
	}{
		{"Claude Code 编程", "⌨", "优先选择低延迟、Claude 兼容稳定的通道。", 0},
		{"RAG / Agent 应用", "AI", "优先选择成功率高、上下文窗口稳定的通道。", 1},
		{"公司采购", "B2B", "优先选择有稳定 SLA 和企业服务能力的通道。", 2},
		{"学生 / 个人开发者", "EDU", "优先选择试用额度充足、成本低的通道。", 3},
	}
	for i, item := range scenarios {
		if len(channels) == 0 {
			break
		}
		ch := channels[item.index%len(channels)]
		if _, err := db.Exec(ctx, `
			insert into recommend_scenarios(id,title,icon,channel_id,summary,position,enabled,created_at,updated_at,data_origin)
			values($1,$2,$3,$4,$5,$6,true,now(),now(),$7)
		`, "rcs_"+uuid.NewString(), item.title, item.icon, ch.id, item.summary, i+1, dataOrigin); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) RecommendConfig(ctx context.Context) (RecommendConfig, error) {
	picks, err := r.recommendPicks(ctx, true)
	if err != nil {
		return RecommendConfig{}, err
	}
	picks = visibleRecommendPicks(picks)
	rewards, err := r.recommendRewards(ctx, true)
	if err != nil {
		return RecommendConfig{}, err
	}
	scenarios, err := r.recommendScenarios(ctx, true)
	if err != nil {
		return RecommendConfig{}, err
	}
	rankRules, err := r.recommendRankRules(ctx, true)
	if err != nil {
		return RecommendConfig{}, err
	}
	ranks, err := r.recommendRanks(ctx, rankRules, picks)
	if err != nil {
		return RecommendConfig{}, err
	}
	stats := recommendStats(picks, ranks, rewards, scenarios)
	return RecommendConfig{Stats: stats, Picks: picks, Ranks: ranks, RankRules: rankRules, Rewards: rewards, Scenarios: scenarios, UpdatedAt: time.Now()}, nil
}

func (r *Repository) RecommendAdminData(ctx context.Context) (RecommendAdminData, error) {
	cfg, err := r.RecommendConfig(ctx)
	if err != nil {
		return RecommendAdminData{}, err
	}
	picks, err := r.recommendPicks(ctx, false)
	if err != nil {
		return RecommendAdminData{}, err
	}
	rewards, err := r.recommendRewards(ctx, false)
	if err != nil {
		return RecommendAdminData{}, err
	}
	scenarios, err := r.recommendScenarios(ctx, false)
	if err != nil {
		return RecommendAdminData{}, err
	}
	rankRules, err := r.recommendRankRules(ctx, false)
	if err != nil {
		return RecommendAdminData{}, err
	}
	cfg.Picks = picks
	cfg.Rewards = rewards
	cfg.Scenarios = scenarios
	cfg.RankRules = rankRules
	channels, err := r.allPublicChannels(ctx)
	if err != nil {
		return RecommendAdminData{}, err
	}
	return RecommendAdminData{RecommendConfig: cfg, Channels: channels}, nil
}

func (r *Repository) SaveRecommendConfig(ctx context.Context, input RecommendSaveInput, actorID string) error {
	if err := r.validateRecommendSaveInput(ctx, input); err != nil {
		return err
	}
	channels, err := r.allPublicChannels(ctx)
	if err != nil {
		return err
	}
	publicChannels := make(map[string]PublicChannel, len(channels))
	for _, channel := range channels {
		publicChannels[channel.ID] = channel
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `delete from recommend_picks`); err != nil {
		return err
	}
	for i, pick := range input.Picks {
		pick.Position = i + 1
		if pick.Title == "" {
			pick.Title = pick.Channel.Name
		}
		pick.Points = cleanRecommendLines(pick.Points)
		points, err := json.Marshal(pick.Points)
		if err != nil {
			return err
		}
		channel := publicChannels[strings.TrimSpace(pick.ChannelID)]
		if _, err := tx.Exec(ctx, `
			insert into recommend_picks(id,channel_id,position,title,ribbon,summary,points_json,cta_label,cta_url,enabled,created_at,updated_at)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),now())
		`, idOrNew(pick.ID, "rcp_"), pick.ChannelID, pick.Position, pick.Title, pick.Ribbon, pick.Summary, points, defaultString(pick.CTALabel, defaultRecommendCTALabel), defaultString(pick.CTAURL, defaultRecommendCTAURL(channel)), pick.Enabled); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `delete from recommend_rewards`); err != nil {
		return err
	}
	for _, reward := range input.Rewards {
		if reward.ProviderName == "" || reward.RewardValue == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			insert into recommend_rewards(id,channel_id,provider_name,reward_type,reward_value,code,expires_at_text,enabled,created_at,updated_at)
			values($1,$2,$3,$4,$5,$6,$7,$8,now(),now())
		`, idOrNew(reward.ID, "rcr_"), nullableText(reward.ChannelID), reward.ProviderName, defaultString(reward.RewardType, "接入说明"), reward.RewardValue, reward.Code, reward.ExpiresAtText, reward.Enabled); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `delete from recommend_scenarios`); err != nil {
		return err
	}
	for i, scenario := range input.Scenarios {
		scenario.Position = i + 1
		if scenario.Title == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			insert into recommend_scenarios(id,title,icon,channel_id,summary,position,enabled,created_at,updated_at)
			values($1,$2,$3,$4,$5,$6,$7,now(),now())
		`, idOrNew(scenario.ID, "rcs_"), scenario.Title, scenario.Icon, nullableText(scenario.ChannelID), scenario.Summary, scenario.Position, scenario.Enabled); err != nil {
			return err
		}
	}
	rankRuleCount := 0
	if input.RankRules != nil {
		if _, err := tx.Exec(ctx, `delete from recommend_rank_rules`); err != nil {
			return err
		}
		for i, rule := range *input.RankRules {
			rule = normalizeRecommendRankRule(rule, i)
			if rule.Label == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				insert into recommend_rank_rules(id,label,description,metric,position,enabled,created_at,updated_at,data_origin)
				values($1,$2,$3,$4,$5,$6,now(),now(),'runtime')
			`, idOrNew(rule.ID, "rnk_"), rule.Label, rule.Description, rule.Metric, rule.Position, rule.Enabled); err != nil {
				return err
			}
			rankRuleCount++
		}
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "recommend.config.updated",
		ObjectType: "recommend_config",
		ObjectID:   "recommend",
		Result:     "success",
		Metadata: map[string]any{
			"picks":     len(input.Picks),
			"rankRules": rankRuleCount,
			"rewards":   len(input.Rewards),
			"scenarios": len(input.Scenarios),
		},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) AddChannelRecommendation(ctx context.Context, actorID string, channelID string) (AdminPlatformChannel, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return AdminPlatformChannel{}, invalidRecommendConfig("请选择要加入精选推荐的平台通道")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var name, provider, model, endpoint, officialSiteURL, status string
	var score int
	var publicVisible bool
	if err := tx.QueryRow(ctx, `
		select name,provider,model,endpoint,coalesce(official_site_url,''),status,score,public_visible
		from channels
		where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null
	`, channelID).Scan(&name, &provider, &model, &endpoint, &officialSiteURL, &status, &score, &publicVisible); err != nil {
		return AdminPlatformChannel{}, err
	}
	if !publicVisible || status == "disabled" {
		return AdminPlatformChannel{}, invalidRecommendConfig("只有公开且未禁用的平台通道才能加入精选推荐")
	}

	var alreadyRecommended bool
	if err := tx.QueryRow(ctx, `
		select exists(select 1 from recommend_picks where channel_id=$1 and enabled=true)
	`, channelID).Scan(&alreadyRecommended); err != nil {
		return AdminPlatformChannel{}, err
	}
	if alreadyRecommended {
		if err := tx.Commit(ctx); err != nil {
			return AdminPlatformChannel{}, err
		}
		return r.AdminPlatformChannel(ctx, channelID)
	}

	var enabledCount int
	if err := tx.QueryRow(ctx, `select count(*) from recommend_picks where enabled=true`).Scan(&enabledCount); err != nil {
		return AdminPlatformChannel{}, err
	}
	if enabledCount >= recommendPickLimit {
		return AdminPlatformChannel{}, invalidRecommendConfig("精选推荐最多支持 %d 个，请先取消一个推荐位", recommendPickLimit)
	}
	if _, err := tx.Exec(ctx, `delete from recommend_picks where channel_id=$1 and enabled=false`, channelID); err != nil {
		return AdminPlatformChannel{}, err
	}

	var position int
	if err := tx.QueryRow(ctx, `
		select slot.position
		from generate_series(1, 12) as slot(position)
		where not exists(select 1 from recommend_picks rp where rp.position=slot.position)
		order by slot.position asc
		limit 1
	`).Scan(&position); err != nil {
		if err == pgx.ErrNoRows {
			return AdminPlatformChannel{}, invalidRecommendConfig("精选推荐位置已满，请先删除一个推荐位")
		}
		return AdminPlatformChannel{}, err
	}

	points := []string{
		fmt.Sprintf("综合评分 %d，适合优先评估", score),
		fmt.Sprintf("%s · %s", provider, model),
		"基于 TokHub 实时监控数据进入精选推荐",
	}
	pointsJSON, err := json.Marshal(points)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into recommend_picks(id,channel_id,position,title,ribbon,summary,points_json,cta_label,cta_url,enabled,created_at,updated_at,data_origin)
		values($1,$2,$3,$4,'精选推荐','基于真实探测和平台评分进入精选推荐，适合优先评估。',$5,$6,$7,true,now(),now(),'runtime')
	`, "rcp_"+uuid.NewString(), channelID, position, name, pointsJSON, defaultRecommendCTALabel, defaultRecommendCTAURL(PublicChannel{Endpoint: endpoint, OfficialSiteURL: officialSiteURL})); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := writeAuditTx(ctx, tx, AuditEvent{
		ActorType:  "user",
		ActorID:    actorID,
		Action:     "platform_channel.recommend_added",
		ObjectType: "channel",
		ObjectID:   channelID,
		Result:     "success",
		Metadata:   map[string]any{"position": position},
	}); err != nil {
		return AdminPlatformChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPlatformChannel{}, err
	}
	return r.AdminPlatformChannel(ctx, channelID)
}

func (r *Repository) RemoveChannelRecommendation(ctx context.Context, actorID string, channelID string) (AdminPlatformChannel, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return AdminPlatformChannel{}, invalidRecommendConfig("请选择要取消精选推荐的平台通道")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists(
			select 1 from channels
			where id=$1 and owner_type='platform' and status <> 'deleted' and deleted_at is null
		)
	`, channelID).Scan(&exists); err != nil {
		return AdminPlatformChannel{}, err
	}
	if !exists {
		return AdminPlatformChannel{}, pgx.ErrNoRows
	}
	rows, err := tx.Query(ctx, `delete from recommend_picks where channel_id=$1 returning id`, channelID)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	removedIDs, err := collectStringRows(rows)
	if err != nil {
		return AdminPlatformChannel{}, err
	}
	if len(removedIDs) > 0 {
		if err := writeAuditTx(ctx, tx, AuditEvent{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     "platform_channel.recommend_removed",
			ObjectType: "channel",
			ObjectID:   channelID,
			Result:     "success",
			Metadata:   map[string]any{"removed": len(removedIDs)},
		}); err != nil {
			return AdminPlatformChannel{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminPlatformChannel{}, err
	}
	return r.AdminPlatformChannel(ctx, channelID)
}

func (r *Repository) validateRecommendSaveInput(ctx context.Context, input RecommendSaveInput) error {
	channels, err := r.allPublicChannels(ctx)
	if err != nil {
		return err
	}
	publicChannels := make(map[string]PublicChannel, len(channels))
	for _, channel := range channels {
		publicChannels[channel.ID] = channel
	}
	hasRecommendRows := len(input.Picks) > 0 || len(input.Rewards) > 0 || len(input.Scenarios) > 0
	if len(publicChannels) == 0 && hasRecommendRows {
		return invalidRecommendConfig("请先创建并公开至少一个平台通道，再维护推荐配置")
	}
	if len(input.Picks) > recommendPickLimit {
		return invalidRecommendConfig("TOP3/推荐位最多支持 %d 项", recommendPickLimit)
	}
	for index, pick := range input.Picks {
		channel, ok := publicChannels[strings.TrimSpace(pick.ChannelID)]
		if strings.TrimSpace(pick.ChannelID) == "" || !ok {
			return invalidRecommendConfig("TOP3 第 %d 项关联的通道不存在或未公开", index+1)
		}
		title := strings.TrimSpace(pick.Title)
		if title == "" {
			title = strings.TrimSpace(pick.Channel.Name)
		}
		if title == "" {
			return invalidRecommendConfig("TOP3 第 %d 项缺少标题", index+1)
		}
		if strings.TrimSpace(pick.Summary) == "" {
			return invalidRecommendConfig("TOP3 第 %d 项缺少推荐摘要", index+1)
		}
		if len(cleanRecommendLines(pick.Points)) == 0 {
			return invalidRecommendConfig("TOP3 第 %d 项至少需要一个卖点", index+1)
		}
		if strings.TrimSpace(defaultString(pick.CTALabel, defaultRecommendCTALabel)) == "" {
			return invalidRecommendConfig("TOP3 第 %d 项缺少按钮文案", index+1)
		}
		if !validRecommendCTA(defaultString(pick.CTAURL, defaultRecommendCTAURL(channel))) {
			return invalidRecommendConfig("TOP3 第 %d 项官方注册页 URL 必须是 http(s) 链接", index+1)
		}
	}
	if input.RankRules != nil {
		for index, rule := range *input.RankRules {
			if strings.TrimSpace(rule.Label) == "" {
				return invalidRecommendConfig("榜单规则第 %d 项缺少名称", index+1)
			}
			if !validRecommendRankMetric(rule.Metric) {
				return invalidRecommendConfig("榜单规则第 %d 项类型无效", index+1)
			}
		}
	}
	for index, reward := range input.Rewards {
		channelID := strings.TrimSpace(reward.ChannelID)
		if channelID != "" {
			if _, ok := publicChannels[channelID]; !ok {
				return invalidRecommendConfig("入口说明第 %d 项关联的通道不存在或未公开", index+1)
			}
		}
		if strings.TrimSpace(reward.ProviderName) == "" {
			return invalidRecommendConfig("入口说明第 %d 项缺少服务商名称", index+1)
		}
		if strings.TrimSpace(reward.RewardValue) == "" {
			return invalidRecommendConfig("入口说明第 %d 项缺少说明内容", index+1)
		}
	}
	for index, scenario := range input.Scenarios {
		channelID := strings.TrimSpace(scenario.ChannelID)
		if channelID == "" {
			return invalidRecommendConfig("场景第 %d 项关联的通道不存在或未公开", index+1)
		}
		if _, ok := publicChannels[channelID]; !ok {
			return invalidRecommendConfig("场景第 %d 项关联的通道不存在或未公开", index+1)
		}
		if strings.TrimSpace(scenario.Title) == "" {
			return invalidRecommendConfig("场景第 %d 项缺少标题", index+1)
		}
		if strings.TrimSpace(scenario.Summary) == "" {
			return invalidRecommendConfig("场景第 %d 项缺少说明", index+1)
		}
	}
	return nil
}

func invalidRecommendConfig(format string, args ...any) error {
	return RecommendValidationError{Message: fmt.Sprintf(format, args...)}
}

func cleanRecommendLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func validRecommendCTA(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func validRecommendRankMetric(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "overall", "speed", "price", "stable", "custom":
		return true
	default:
		return false
	}
}

func defaultRecommendCTAURL(channel PublicChannel) string {
	if url := strings.TrimSpace(channel.OfficialSiteURL); url != "" {
		if validRecommendCTA(url) {
			return url
		}
	}
	return officialEntryURL(channel.Endpoint)
}

func officialEntryURL(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	hostname := publicWebsiteHost(parsed.Hostname())
	if hostname == "" {
		return ""
	}
	return "https://" + hostname
}

func publicWebsiteHost(hostname string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(hostname)), ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) < 2 {
		return ""
	}
	apiPrefixes := map[string]bool{
		"api":      true,
		"api2":     true,
		"cc-api":   true,
		"chat-api": true,
		"openapi":  true,
		"gateway":  true,
		"proxy":    true,
		"relay":    true,
		"upstream": true,
	}
	for len(out) > 2 && apiPrefixes[out[0]] {
		out = out[1:]
	}
	return strings.Join(out, ".")
}

func (r *Repository) TrackRecommendClick(ctx context.Context, itemType string, itemID string, channelID string, userID string, ip string, ua string) error {
	switch itemType {
	case "pick", "rank", "reward", "scenario", "cta":
	default:
		itemType = "cta"
	}
	_, err := r.db.Exec(ctx, `
		insert into recommend_click_events(id,item_type,item_id,channel_id,user_id,ip,user_agent,created_at)
		values($1,$2,$3,$4,$5,$6,$7,now())
	`, "rcc_"+uuid.NewString(), itemType, strings.TrimSpace(itemID), nullableText(channelID), nullableText(userID), ip, ua)
	return err
}

func (r *Repository) recommendPicks(ctx context.Context, enabledOnly bool) ([]RecommendPick, error) {
	where := ""
	if enabledOnly {
		where = "where rp.enabled=true"
	}
	rows, err := r.db.Query(ctx, `
		select rp.id,rp.channel_id,rp.position,rp.title,rp.ribbon,rp.summary,rp.points_json,rp.cta_label,rp.cta_url,rp.enabled,
			coalesce((select count(*) from recommend_click_events e where e.item_type='pick' and e.item_id=rp.id),0)
		from recommend_picks rp
		`+where+`
		order by rp.position asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecommendPick{}
	for rows.Next() {
		var item RecommendPick
		var points []byte
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.Position, &item.Title, &item.Ribbon, &item.Summary, &points, &item.CTALabel, &item.CTAURL, &item.Enabled, &item.Clicks); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(points, &item.Points)
		if detail, err := r.PublicChannel(ctx, item.ChannelID); err == nil {
			item.Channel = detail.Channel
			syncGeneratedRecommendPickPoints(&item)
		}
		syncLegacyRecommendPickCTA(&item)
		out = append(out, item)
	}
	return out, rows.Err()
}

func syncGeneratedRecommendPickPoints(item *RecommendPick) {
	if item.Channel.ID == "" || len(item.Points) == 0 {
		return
	}
	if len(item.Points) >= 3 && item.Points[2] == "基于 TokHub 实时监控数据进入精选推荐" {
		item.Points[0] = fmt.Sprintf("综合评分 %d，适合优先评估", item.Channel.Score)
		if item.Channel.Provider != "" || item.Channel.Model != "" {
			item.Points[1] = strings.TrimSpace(strings.Trim(strings.TrimSpace(item.Channel.Provider+" · "+item.Channel.Model), "·"))
		}
		return
	}
	if strings.HasPrefix(item.Points[0], "综合评分 ") && strings.Contains(item.Points[0], "长期监控表现稳定") {
		item.Points[0] = fmt.Sprintf("综合评分 %d，长期监控表现稳定", item.Channel.Score)
	}
}

func syncLegacyRecommendPickCTA(item *RecommendPick) {
	switch strings.TrimSpace(item.CTALabel) {
	case "", "查看详情", "立即试用":
		item.CTALabel = defaultRecommendCTALabel
	}
	ctaURL := strings.TrimSpace(item.CTAURL)
	if ctaURL == "" || ctaURL == "/login" || strings.HasPrefix(ctaURL, "/channels/") {
		if official := defaultRecommendCTAURL(item.Channel); official != "" {
			item.CTAURL = official
		}
	}
}

func (r *Repository) recommendRewards(ctx context.Context, enabledOnly bool) ([]RecommendReward, error) {
	where := ""
	if enabledOnly {
		where = "where rr.enabled=true"
	}
	rows, err := r.db.Query(ctx, `
		select rr.id,coalesce(rr.channel_id,''),rr.provider_name,rr.reward_type,rr.reward_value,rr.code,rr.expires_at_text,rr.enabled,
			coalesce((select count(*) from recommend_click_events e where e.item_type='reward' and e.item_id=rr.id),0)
		from recommend_rewards rr
		`+where+`
		order by rr.created_at asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecommendReward{}
	for rows.Next() {
		var item RecommendReward
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ProviderName, &item.RewardType, &item.RewardValue, &item.Code, &item.ExpiresAtText, &item.Enabled, &item.Clicks); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) recommendScenarios(ctx context.Context, enabledOnly bool) ([]RecommendScenario, error) {
	where := ""
	if enabledOnly {
		where = "where rs.enabled=true"
	}
	rows, err := r.db.Query(ctx, `
		select rs.id,rs.title,rs.icon,coalesce(rs.channel_id,''),rs.summary,rs.position,rs.enabled,
			coalesce((select count(*) from recommend_click_events e where e.item_type='scenario' and e.item_id=rs.id),0)
		from recommend_scenarios rs
		`+where+`
		order by rs.position asc, rs.created_at asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecommendScenario{}
	for rows.Next() {
		var item RecommendScenario
		if err := rows.Scan(&item.ID, &item.Title, &item.Icon, &item.ChannelID, &item.Summary, &item.Position, &item.Enabled, &item.Clicks); err != nil {
			return nil, err
		}
		if item.ChannelID != "" {
			if detail, err := r.PublicChannel(ctx, item.ChannelID); err == nil {
				item.Channel = detail.Channel
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) recommendRankRules(ctx context.Context, enabledOnly bool) ([]RecommendRankRule, error) {
	where := ""
	if enabledOnly {
		where = "where enabled=true"
	}
	rows, err := r.db.Query(ctx, `
		select id,label,description,metric,position,enabled
		from recommend_rank_rules
		`+where+`
		order by position asc, created_at asc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecommendRankRule{}
	for rows.Next() {
		var item RecommendRankRule
		if err := rows.Scan(&item.ID, &item.Label, &item.Description, &item.Metric, &item.Position, &item.Enabled); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) recommendRanks(ctx context.Context, rules []RecommendRankRule, picks []RecommendPick) ([]RecommendPick, error) {
	if len(rules) == 0 {
		return []RecommendPick{}, nil
	}
	out := make([]RecommendPick, 0, len(picks))
	for i, pick := range picks {
		pick.Position = i + 1
		out = append(out, pick)
	}
	return out, nil
}

func visibleRecommendPicks(picks []RecommendPick) []RecommendPick {
	if len(picks) == 0 {
		return []RecommendPick{}
	}
	limit := recommendPickLimit
	if len(picks) < limit {
		limit = len(picks)
	}
	out := make([]RecommendPick, 0, limit)
	for i := 0; i < limit; i++ {
		pick := picks[i]
		pick.Position = i + 1
		out = append(out, pick)
	}
	return out
}

func normalizeRecommendRankRule(rule RecommendRankRule, index int) RecommendRankRule {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Label = strings.TrimSpace(rule.Label)
	rule.Description = strings.TrimSpace(rule.Description)
	rule.Metric = strings.ToLower(strings.TrimSpace(rule.Metric))
	switch rule.Metric {
	case "overall", "speed", "price", "stable", "custom":
	default:
		rule.Metric = "custom"
	}
	if rule.Position <= 0 {
		rule.Position = index + 1
	}
	if len(rule.Label) > 40 {
		rule.Label = rule.Label[:40]
	}
	if len(rule.Description) > 160 {
		rule.Description = rule.Description[:160]
	}
	return rule
}

func recommendStats(picks []RecommendPick, ranks []RecommendPick, rewards []RecommendReward, scenarios []RecommendScenario) RecommendStats {
	var clicks int
	var score int
	for _, pick := range picks {
		clicks += pick.Clicks
		score += pick.Channel.Score
	}
	for _, reward := range rewards {
		clicks += reward.Clicks
	}
	for _, scenario := range scenarios {
		clicks += scenario.Clicks
	}
	avg := 0
	if len(picks) > 0 {
		avg = score / len(picks)
	}
	ctr := 0.0
	if len(picks)+len(ranks)+len(rewards)+len(scenarios) > 0 {
		ctr = round1(float64(clicks) / float64((len(picks)+len(ranks)+len(rewards)+len(scenarios))*100) * 100)
	}
	return RecommendStats{Picks: len(picks), Ranked: len(ranks), Rewards: len(rewards), Scenarios: len(scenarios), Clicks: clicks, CTR: ctr, AverageScore: avg}
}

func (r *Repository) OpenAPISummary(ctx context.Context, baseURL string) (OpenAPISummary, error) {
	sites, err := r.ListOpenAPISites(ctx)
	if err != nil {
		return OpenAPISummary{}, err
	}
	logs, err := r.OpenAPICallLogs(ctx)
	if err != nil {
		return OpenAPISummary{}, err
	}
	endpoints, err := r.OpenAPIEndpoints(ctx)
	if err != nil {
		return OpenAPISummary{}, err
	}
	active := 0
	for _, site := range sites {
		if site.Status == "active" {
			active++
		}
	}
	stats := map[string]int{"sites": len(sites), "active": active, "endpoints": len(defaultOpenAPIEndpoints()), "callsToday": 0}
	for _, site := range sites {
		stats["callsToday"] += site.CallsToday
	}
	return OpenAPISummary{BaseURL: strings.TrimRight(baseURL, "/") + "/v1/status", Sites: sites, Logs: logs, Endpoints: endpoints, Stats: stats}, nil
}

func (r *Repository) OpenAPIEndpoints(ctx context.Context) ([]OpenAPIEndpoint, error) {
	out := defaultOpenAPIEndpoints()
	for i := range out {
		var averageMs float64
		if err := r.db.QueryRow(ctx, `
			select coalesce(count(*),0), coalesce(avg(latency_ms),0)
			from open_api_call_logs
			where endpoint=$1 and created_at::date=current_date
		`, out[i].Path).Scan(&out[i].CallsToday, &averageMs); err != nil {
			return nil, err
		}
		out[i].AverageMs = int(averageMs)
	}
	return out, nil
}

func defaultOpenAPIEndpoints() []OpenAPIEndpoint {
	return []OpenAPIEndpoint{
		{Scope: "channels", Method: "GET", Path: "/v1/status/channels", Description: "通道状态列表", Cache: "10s", Status: "active"},
		{Scope: "channels", Method: "GET", Path: "/v1/status/channels/{channelID}", Description: "单通道详情", Cache: "10s", Status: "active"},
		{Scope: "channel_sync", Method: "GET", Path: "/v1/status/channel-sync", Description: "通道同步包，含平台凭据与监控快照", Cache: "no-store", Status: "active"},
		{Scope: "uptime", Method: "GET", Path: "/v1/status/uptime", Description: "可用率与趋势摘要", Cache: "30s", Status: "active"},
		{Scope: "incidents", Method: "GET", Path: "/v1/status/incidents", Description: "故障事件列表", Cache: "30s", Status: "active"},
		{Scope: "overview", Method: "GET", Path: "/v1/status/overview", Description: "公开总览指标", Cache: "10s", Status: "active"},
	}
}

func (r *Repository) ListOpenAPISites(ctx context.Context) ([]OpenAPISite, error) {
	rows, err := r.db.Query(ctx, `
		select s.id,s.name,s.site_key_prefix,s.site_key_mask,s.scopes,s.qps_limit,s.status,s.created_at,s.last_used_at,
			coalesce((select count(*) from open_api_call_logs l where l.site_id=s.id and l.created_at::date=current_date),0)
		from open_api_sites s
		where s.deleted_at is null
		order by s.created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenAPISite{}
	for rows.Next() {
		var site OpenAPISite
		if err := rows.Scan(&site.ID, &site.Name, &site.SiteKeyPrefix, &site.SiteKeyMask, &site.Scopes, &site.QPSLimit, &site.Status, &site.CreatedAt, nullableTimePtr(&site.LastUsedAt), &site.CallsToday); err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

func (r *Repository) CreateOpenAPISite(ctx context.Context, input OpenAPISiteCreateInput) (OpenAPISite, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "官网状态页"
	}
	scopes, err := normalizeOpenAPIScopes(input.Scopes, true)
	if err != nil {
		return OpenAPISite{}, err
	}
	qps := input.QPSLimit
	if qps <= 0 {
		qps = 60
	}
	plain, err := NewOpenAPIPlainKey()
	if err != nil {
		return OpenAPISite{}, err
	}
	hash := HashOpaqueToken(plain)
	site := OpenAPISite{
		ID:            "oas_" + uuid.NewString(),
		Name:          name,
		SiteKeyPrefix: OpenAPIKeyPrefix(plain),
		SiteKeyMask:   MaskGatewayKey(plain),
		PlainKey:      plain,
		Scopes:        scopes,
		QPSLimit:      qps,
		Status:        "active",
		CreatedAt:     time.Now(),
	}
	err = r.db.QueryRow(ctx, `
		insert into open_api_sites(id,name,site_key_hash,site_key_prefix,site_key_mask,scopes,qps_limit,status,created_by,created_at,updated_at)
		values($1,$2,$3,$4,$5,$6,$7,'active',$8,now(),now())
		returning created_at
	`, site.ID, site.Name, hash, site.SiteKeyPrefix, site.SiteKeyMask, site.Scopes, site.QPSLimit, nullableText(input.ActorID)).Scan(&site.CreatedAt)
	if err != nil {
		return OpenAPISite{}, err
	}
	_ = r.WriteAudit(ctx, AuditEvent{
		ActorType:  "user",
		ActorID:    input.ActorID,
		Action:     "open_api.site.created",
		ObjectType: "open_api_site",
		ObjectID:   site.ID,
		Result:     "success",
		Metadata:   map[string]any{"name": site.Name, "scopes": site.Scopes, "site_key_prefix": site.SiteKeyPrefix},
	})
	return site, nil
}

func (r *Repository) AuthenticateOpenAPISite(ctx context.Context, plainKey string, scope string) (AuthenticatedOpenAPISite, error) {
	hash := HashOpaqueToken(strings.TrimSpace(plainKey))
	var site OpenAPISite
	err := r.db.QueryRow(ctx, `
		select id,name,site_key_prefix,site_key_mask,scopes,qps_limit,status,created_at,last_used_at
		from open_api_sites
		where site_key_hash=$1 and status='active' and deleted_at is null
	`, hash).Scan(&site.ID, &site.Name, &site.SiteKeyPrefix, &site.SiteKeyMask, &site.Scopes, &site.QPSLimit, &site.Status, &site.CreatedAt, nullableTimePtr(&site.LastUsedAt))
	if err != nil {
		return AuthenticatedOpenAPISite{}, err
	}
	if !scopeAllowed(site.Scopes, scope) {
		return AuthenticatedOpenAPISite{}, pgx.ErrNoRows
	}
	return AuthenticatedOpenAPISite{Site: site}, nil
}

func (r *Repository) TouchOpenAPISite(ctx context.Context, siteID string) error {
	_, err := r.db.Exec(ctx, `update open_api_sites set last_used_at=now(), updated_at=now() where id=$1 and deleted_at is null`, siteID)
	return err
}

func (r *Repository) RecordOpenAPICall(ctx context.Context, siteID string, endpoint string, status int, latencyMs int, ip string, ua string) error {
	_, err := r.db.Exec(ctx, `
		insert into open_api_call_logs(id,site_id,endpoint,status_code,latency_ms,ip,user_agent,created_at)
		values($1,$2,$3,$4,$5,$6,$7,now())
	`, "oal_"+uuid.NewString(), nullableText(siteID), endpoint, status, latencyMs, ip, ua)
	return err
}

func (r *Repository) OpenAPICallLogs(ctx context.Context) ([]OpenAPICallLog, error) {
	rows, err := r.db.Query(ctx, `
		select l.id,coalesce(s.name,'-'),l.endpoint,l.status_code,l.latency_ms,coalesce(l.ip,''),l.created_at
		from open_api_call_logs l
		left join open_api_sites s on s.id=l.site_id
		order by l.created_at desc
		limit 30
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenAPICallLog{}
	for rows.Next() {
		var item OpenAPICallLog
		if err := rows.Scan(&item.ID, &item.SiteName, &item.Endpoint, &item.StatusCode, &item.LatencyMs, &item.IP, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) OpenAPIChannelSyncLogs(ctx context.Context, limit int) ([]OpenAPIChannelSyncLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		select ss.id,ss.channel_id,c.name,ss.sampled_at,ss.status,ss.score,ss.latency_p95_ms,
			ss.l1_status,ss.l2_status,ss.l3_status,coalesce(ss.error_type,'')
		from channel_status_snapshots ss
		join channels c on c.id=ss.channel_id and c.owner_type='platform'
		where c.status <> 'deleted' and c.deleted_at is null
		order by ss.sampled_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenAPIChannelSyncLog{}
	for rows.Next() {
		var item OpenAPIChannelSyncLog
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.SampledAt, &item.Status, &item.Score, &item.LatencyP95Ms, &item.L1Status, &item.L2Status, &item.L3Status, &item.ErrorType); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) OpenAPIUptime(ctx context.Context) (map[string]any, error) {
	overview, err := r.PublicOverview(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := r.allPublicChannels(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		items = append(items, map[string]any{
			"channelId":    ch.ID,
			"name":         ch.Name,
			"status":       ch.Status,
			"uptime24h":    ch.Uptime24h,
			"successRate":  ch.SuccessRate,
			"latencyP95Ms": ch.LatencyP95Ms,
			"lastProbeAt":  ch.LastProbeAt,
		})
	}
	return map[string]any{"overview": overview, "items": items}, nil
}

func (r *Repository) OpenAPIIncidents(ctx context.Context) ([]OpenAPIIncident, error) {
	rows, err := r.db.Query(ctx, `
		select i.id,i.channel_id,c.name,i.status,i.title,i.opened_at,i.resolved_at
		from incidents i
		join channels c on c.id=i.channel_id and c.owner_type='platform'
		where i.deleted_at is null
		order by i.opened_at desc
		limit 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenAPIIncident{}
	for rows.Next() {
		var item OpenAPIIncident
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.Channel, &item.Status, &item.Title, &item.OpenedAt, nullableTimePtr(&item.ResolvedAt)); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func normalizeOpenAPIScopes(scopes []string, allowDefault bool) ([]string, error) {
	allowed := map[string]bool{"channels": true, "uptime": true, "incidents": true, "overview": true, "channel_sync": true}
	if len(scopes) == 0 {
		if allowDefault {
			return []string{"channels", "uptime", "incidents", "overview"}, nil
		}
		return nil, ErrInvalidOpenAPIScope
	}
	out := []string{}
	seen := map[string]bool{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(strings.ToLower(scope))
		if !allowed[scope] {
			return nil, ErrInvalidOpenAPIScope
		}
		if !seen[scope] {
			out = append(out, scope)
			seen[scope] = true
		}
	}
	if len(out) == 0 {
		return nil, ErrInvalidOpenAPIScope
	}
	return out, nil
}

func scopeAllowed(scopes []string, scope string) bool {
	scope = strings.TrimSpace(strings.ToLower(scope))
	for _, item := range scopes {
		if item == "*" || strings.EqualFold(item, scope) {
			return true
		}
	}
	return false
}

func NewOpenAPIPlainKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "site-th-" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func OpenAPIKeyPrefix(key string) string {
	if len(key) <= 14 {
		return key
	}
	return key[:14]
}

func idOrNew(value string, prefix string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return prefix + uuid.NewString()
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func writeAuditTx(ctx context.Context, tx pgx.Tx, event AuditEvent) error {
	event = enrichAgentAuditEvent(ctx, event)
	meta, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into audit_events(id,actor_type,actor_id,action,object_type,object_id,ip,result,metadata,created_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
	`, "aud_"+uuid.NewString(), event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.IP, event.Result, meta)
	return err
}

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
