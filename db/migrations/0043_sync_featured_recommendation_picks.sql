with featured_source(
  target_key,
  channel_id,
  name_key,
  name,
  provider,
  endpoint,
  official_url,
  public_slug,
  score,
  provider_config,
  pick_id,
  position,
  ribbon,
  summary,
  points_json
) as (
  values
    (
      'aigocode',
      'ch_aa0af8fa-1ca3-44b6-b5a9-e015314a15cf',
      'aigocode',
      'AIGoCode',
      'AIGoCode',
      'https://api.aigocode.app',
      'https://aigocode.com/invite/AP5KFJWJ',
      'c4bfcfa8',
      42,
      '{"timeoutMs":60000,"clientProfile":"claude-code","l3ContentPolicy":"non_empty"}'::jsonb,
      'rcp_featured_aigocode',
      1,
      'AI 编程推荐',
      'AIGoCode 适合 AI 编程与团队协作场景，已纳入 TokHub 精选推荐。',
      '["综合评分 42，适合优先评估","AIGoCode · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    ),
    (
      'pipellm',
      'ch_6cfb132a-f91d-434a-8120-0a7c55b3cb8e',
      'pipellm',
      'Pipellm',
      'Pipellm',
      'https://cc-api.pipellm.ai',
      'https://code.pipellm.ai/login?ref=vbsdxpv8',
      'a2f65017',
      99,
      '{"timeoutMs":60000}'::jsonb,
      'rcp_featured_pipellm',
      2,
      'Agent 基础设施',
      'PipeLLM 适合 Agent 与模型网关基础设施场景，已纳入 TokHub 精选推荐。',
      '["综合评分 99，适合优先评估","Pipellm · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    ),
    (
      'packycode',
      'ch_c348c4e7-dbb7-4458-8084-17417c46f46b',
      'packycode',
      'PackyCode',
      'PackyCode',
      'https://www.packyapi.com',
      'https://www.packyapi.com/register?aff=lqD6',
      '5b05862a',
      70,
      '{"timeoutMs":60000,"authHeader":"authorization","l3ProbeMode":"l2_only"}'::jsonb,
      'rcp_featured_packycode',
      3,
      '编辑首推',
      'PackyCode 适合 Claude Code 与研发场景，已纳入 TokHub 精选推荐。',
      '["综合评分 70，适合优先评估","PackyCode · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    )
)
insert into channels(
  id,
  owner_type,
  owner_id,
  name,
  provider,
  type,
  model,
  upstream_model,
  endpoint,
  official_site_url,
  status,
  score,
  probe_daily,
  probes_used_today,
  probe_reset_date,
  public_visible,
  gateway_enabled,
  disabled_at,
  deleted_at,
  provider_config,
  data_origin,
  public_slug,
  intro_source_url,
  created_at,
  updated_at
)
select
  s.channel_id,
  'platform',
  null,
  s.name,
  s.provider,
  'anthropic',
  'claude-sonnet-4-6',
  'claude-sonnet-4-6',
  s.endpoint,
  s.official_url,
  'healthy',
  s.score,
  24,
  0,
  current_date,
  true,
  false,
  null,
  null,
  s.provider_config,
  'runtime',
  case
    when exists(select 1 from channels c where c.public_slug = s.public_slug and c.id <> s.channel_id)
      then lower(substr(md5(s.channel_id || s.public_slug || s.name_key), 1, 8))
    else s.public_slug
  end,
  s.official_url,
  now(),
  now()
from featured_source s
where not exists (
  select 1
  from channels c
  where c.owner_type = 'platform'
    and c.deleted_at is null
    and (
      c.id = s.channel_id
      or lower(replace(c.name, ' ', '')) = s.name_key
      or lower(replace(c.provider, ' ', '')) = s.name_key
      or c.official_site_url = s.official_url
    )
)
on conflict(id) do update set
  name = excluded.name,
  provider = excluded.provider,
  type = excluded.type,
  model = excluded.model,
  upstream_model = excluded.upstream_model,
  endpoint = excluded.endpoint,
  official_site_url = excluded.official_site_url,
  status = excluded.status,
  score = excluded.score,
  probe_daily = excluded.probe_daily,
  public_visible = true,
  disabled_at = null,
  deleted_at = null,
  provider_config = coalesce(channels.provider_config, '{}'::jsonb) || excluded.provider_config,
  data_origin = 'runtime',
  intro_source_url = excluded.intro_source_url,
  updated_at = now();

with featured_source(
  target_key,
  channel_id,
  name_key,
  name,
  provider,
  endpoint,
  official_url,
  public_slug,
  score,
  provider_config
) as (
  values
    ('aigocode','ch_aa0af8fa-1ca3-44b6-b5a9-e015314a15cf','aigocode','AIGoCode','AIGoCode','https://api.aigocode.app','https://aigocode.com/invite/AP5KFJWJ','c4bfcfa8',42,'{"timeoutMs":60000,"clientProfile":"claude-code","l3ContentPolicy":"non_empty"}'::jsonb),
    ('pipellm','ch_6cfb132a-f91d-434a-8120-0a7c55b3cb8e','pipellm','Pipellm','Pipellm','https://cc-api.pipellm.ai','https://code.pipellm.ai/login?ref=vbsdxpv8','a2f65017',99,'{"timeoutMs":60000}'::jsonb),
    ('packycode','ch_c348c4e7-dbb7-4458-8084-17417c46f46b','packycode','PackyCode','PackyCode','https://www.packyapi.com','https://www.packyapi.com/register?aff=lqD6','5b05862a',70,'{"timeoutMs":60000,"authHeader":"authorization","l3ProbeMode":"l2_only"}'::jsonb)
),
selected_channels as (
  select distinct on (s.target_key)
    s.*,
    c.id as matched_channel_id
  from featured_source s
  join channels c on c.owner_type = 'platform'
    and c.deleted_at is null
    and (
      c.id = s.channel_id
      or lower(replace(c.name, ' ', '')) = s.name_key
      or lower(replace(c.provider, ' ', '')) = s.name_key
      or c.official_site_url = s.official_url
    )
  order by
    s.target_key,
    case
      when c.id = s.channel_id then 0
      when c.official_site_url = s.official_url then 1
      when lower(replace(c.name, ' ', '')) = s.name_key then 2
      when lower(replace(c.provider, ' ', '')) = s.name_key then 3
      else 4
    end,
    c.updated_at desc
)
update channels c
set
  name = s.name,
  provider = s.provider,
  type = 'anthropic',
  model = 'claude-sonnet-4-6',
  upstream_model = 'claude-sonnet-4-6',
  endpoint = s.endpoint,
  official_site_url = s.official_url,
  status = 'healthy',
  score = s.score,
  public_visible = true,
  disabled_at = null,
  deleted_at = null,
  provider_config = coalesce(c.provider_config, '{}'::jsonb) || s.provider_config,
  data_origin = 'runtime',
  public_slug = case
    when coalesce(c.public_slug, '') = '' then s.public_slug
    when c.public_slug = s.public_slug then c.public_slug
    when not exists(select 1 from channels other where other.public_slug = s.public_slug and other.id <> c.id) then s.public_slug
    else c.public_slug
  end,
  intro_source_url = s.official_url,
  updated_at = now()
from selected_channels s
where c.id = s.matched_channel_id;

delete from recommend_picks
where id in ('rcp_featured_aigocode', 'rcp_featured_pipellm', 'rcp_featured_packycode')
   or position between 1 and 3;

with featured_source(
  target_key,
  channel_id,
  name_key,
  name,
  provider,
  endpoint,
  official_url,
  pick_id,
  position,
  ribbon,
  summary,
  points_json
) as (
  values
    (
      'aigocode',
      'ch_aa0af8fa-1ca3-44b6-b5a9-e015314a15cf',
      'aigocode',
      'AIGoCode',
      'AIGoCode',
      'https://api.aigocode.app',
      'https://aigocode.com/invite/AP5KFJWJ',
      'rcp_featured_aigocode',
      1,
      'AI 编程推荐',
      'AIGoCode 适合 AI 编程与团队协作场景，已纳入 TokHub 精选推荐。',
      '["综合评分 42，适合优先评估","AIGoCode · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    ),
    (
      'pipellm',
      'ch_6cfb132a-f91d-434a-8120-0a7c55b3cb8e',
      'pipellm',
      'Pipellm',
      'Pipellm',
      'https://cc-api.pipellm.ai',
      'https://code.pipellm.ai/login?ref=vbsdxpv8',
      'rcp_featured_pipellm',
      2,
      'Agent 基础设施',
      'PipeLLM 适合 Agent 与模型网关基础设施场景，已纳入 TokHub 精选推荐。',
      '["综合评分 99，适合优先评估","Pipellm · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    ),
    (
      'packycode',
      'ch_c348c4e7-dbb7-4458-8084-17417c46f46b',
      'packycode',
      'PackyCode',
      'PackyCode',
      'https://www.packyapi.com',
      'https://www.packyapi.com/register?aff=lqD6',
      'rcp_featured_packycode',
      3,
      '编辑首推',
      'PackyCode 适合 Claude Code 与研发场景，已纳入 TokHub 精选推荐。',
      '["综合评分 70，适合优先评估","PackyCode · claude-sonnet-4-6","基于 TokHub 实时监控数据进入精选推荐"]'::jsonb
    )
),
selected_channels as (
  select distinct on (s.target_key)
    s.*,
    c.id as matched_channel_id
  from featured_source s
  join channels c on c.owner_type = 'platform'
    and c.public_visible is true
    and c.status not in ('disabled', 'deleted')
    and c.deleted_at is null
    and coalesce(c.data_origin, '') not in ('demo', 'test')
    and (
      c.id = s.channel_id
      or lower(replace(c.name, ' ', '')) = s.name_key
      or lower(replace(c.provider, ' ', '')) = s.name_key
      or c.official_site_url = s.official_url
    )
  order by
    s.target_key,
    case
      when c.id = s.channel_id then 0
      when c.official_site_url = s.official_url then 1
      when lower(replace(c.name, ' ', '')) = s.name_key then 2
      when lower(replace(c.provider, ' ', '')) = s.name_key then 3
      else 4
    end,
    c.updated_at desc
)
insert into recommend_picks(
  id,
  channel_id,
  position,
  title,
  ribbon,
  summary,
  points_json,
  cta_label,
  cta_url,
  enabled,
  created_at,
  updated_at,
  data_origin
)
select
  s.pick_id,
  s.matched_channel_id,
  s.position,
  s.name,
  s.ribbon,
  s.summary,
  s.points_json,
  '去官方体验',
  s.official_url,
  true,
  now(),
  now(),
  'runtime'
from selected_channels s
order by s.position;
