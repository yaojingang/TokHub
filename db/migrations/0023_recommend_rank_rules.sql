create table if not exists recommend_rank_rules (
  id text primary key,
  label text not null,
  description text not null default '',
  metric text not null default 'overall' check(metric in ('overall','speed','price','stable','custom')),
  position integer not null default 0,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  data_origin text not null default 'system'
);

create index if not exists idx_recommend_rank_rules_enabled_position
  on recommend_rank_rules(enabled, position);

insert into recommend_rank_rules(id,label,description,metric,position,enabled,data_origin)
values
  ('rank_rule_overall','综合榜','按运营配置顺序兜底展示','overall',1,true,'system'),
  ('rank_rule_speed','速度王','按 P95 延迟由低到高排序','speed',2,true,'system'),
  ('rank_rule_price','性价比','按价格倍数和质量评分加权','price',3,true,'system'),
  ('rank_rule_stable','稳定王','按 30 天成功率排序','stable',4,true,'system')
on conflict(id) do nothing;
