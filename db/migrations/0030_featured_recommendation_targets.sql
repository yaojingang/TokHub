delete from recommend_picks;

with targets as (
  select *
  from (
    values
      ('packycode', array['packycode','packyapi','packy'], 1, '编辑首推', 'PackyCode 适合 Claude Code 与研发场景，已纳入 TokHub 精选推荐。'),
      ('aigocode', array['aigocode','aigo code'], 2, 'AI 编程推荐', 'AIGoCode 适合 AI 编程与团队协作场景，已纳入 TokHub 精选推荐。'),
      ('pipellm', array['pipellm','pipe llm'], 3, 'Agent 基础设施', 'PipeLLM 适合 Agent 与模型网关基础设施场景，已纳入 TokHub 精选推荐。')
  ) as t(target_key, aliases, position, ribbon, summary)
),
matched as (
  select
    t.target_key,
    t.position,
    t.ribbon,
    t.summary,
    c.id as channel_id,
    c.name,
    c.provider,
    c.model,
    c.endpoint,
    c.score
  from targets t
  join lateral (
    select
      c.*,
      (
        select min(
          case
            when lower(c.name) = alias or lower(c.provider) = alias then 0
            when lower(c.name) like alias || '%' or lower(c.provider) like alias || '%' then 1
            else 2
          end
        )
        from unnest(t.aliases) as alias
        where lower(c.name) = alias
          or lower(c.provider) = alias
          or lower(c.name) like '%' || alias || '%'
          or lower(c.provider) like '%' || alias || '%'
      ) as match_rank
    from channels c
    where c.owner_type = 'platform'
      and c.status <> 'deleted'
      and c.deleted_at is null
      and c.public_visible = true
      and exists (
        select 1
        from unnest(t.aliases) as alias
        where lower(c.name) = alias
          or lower(c.provider) = alias
          or lower(c.name) like '%' || alias || '%'
          or lower(c.provider) like '%' || alias || '%'
      )
    order by match_rank asc, c.score desc, c.updated_at desc, c.name asc
    limit 1
  ) c on true
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
  'rcp_featured_' || target_key,
  channel_id,
  position,
  name,
  ribbon,
  summary,
  jsonb_build_array(
    '综合评分 ' || score || '，适合优先评估',
    provider || ' · ' || model,
    '基于 TokHub 实时监控数据进入精选推荐'
  ),
  '去官方体验',
  'https://' || regexp_replace(split_part(split_part(regexp_replace(endpoint, '^https?://', '', 'i'), '/', 1), ':', 1), '^(api|api2|cc-api|chat-api|openapi|gateway|proxy|relay|upstream)\.', ''),
  true,
  now(),
  now(),
  'runtime'
from matched
order by position;
