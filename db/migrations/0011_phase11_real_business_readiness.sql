alter table users add column if not exists data_origin text not null default 'runtime';
alter table orgs add column if not exists data_origin text not null default 'runtime';
alter table channels add column if not exists data_origin text not null default 'runtime';
alter table gateways add column if not exists data_origin text not null default 'runtime';
alter table gateway_keys add column if not exists data_origin text not null default 'runtime';
alter table open_api_sites add column if not exists data_origin text not null default 'runtime';
alter table notification_channels add column if not exists data_origin text not null default 'runtime';
alter table alert_rules add column if not exists data_origin text not null default 'runtime';
alter table recommend_picks add column if not exists data_origin text not null default 'runtime';
alter table recommend_rewards add column if not exists data_origin text not null default 'runtime';
alter table recommend_scenarios add column if not exists data_origin text not null default 'runtime';

create index if not exists idx_users_data_origin on users(data_origin);
create index if not exists idx_orgs_data_origin on orgs(data_origin);
create index if not exists idx_channels_data_origin on channels(data_origin);
create index if not exists idx_gateways_data_origin on gateways(data_origin);
create index if not exists idx_gateway_keys_data_origin on gateway_keys(data_origin);
create index if not exists idx_open_api_sites_data_origin on open_api_sites(data_origin);
create index if not exists idx_notification_channels_data_origin on notification_channels(data_origin);
create index if not exists idx_alert_rules_data_origin on alert_rules(data_origin);
create index if not exists idx_recommend_picks_data_origin on recommend_picks(data_origin);
create index if not exists idx_recommend_rewards_data_origin on recommend_rewards(data_origin);
create index if not exists idx_recommend_scenarios_data_origin on recommend_scenarios(data_origin);

update users
set data_origin='system'
where role='owner' and email in ('admin@tokhub.local','admin@example.com');

update users
set data_origin='test'
where data_origin='runtime'
  and role <> 'owner'
  and (
    email ~* '(^|[+._-])(phase[0-9]*|load|closed|test|e2e)([+._@-]|$)'
    or name ~* '(^|[[:space:]])(phase|load|test|e2e)([[:space:]]|$)'
  );

update orgs
set data_origin='system'
where id='org_default';

update orgs
set data_origin='test'
where data_origin='runtime'
  and id <> 'org_default'
  and (name ~* '(phase|load|test|mock|e2e)' or slug ~* '(phase|load|test|mock|e2e)');

update channels
set data_origin='demo'
where owner_type='platform'
  and (
    endpoint ~* '^https?://[^/]*\.example([/:]|$)'
    or endpoint ~* 'example/'
    or id in (
      'ch_cc_claude','ch_cx_mix','ch_gm_gemini','ch_or_openrouter','ch_sf_fast',
      'ch_nb_backup','ch_qn_qwen','ch_ds_deepseek','ch_mn_mistral','ch_vl_volc',
      'ch_az_azure','ch_gp_groq','ch_bd_baidu','ch_lt_local'
    )
  );

update channels
set data_origin='test'
where owner_type='user'
  and (
    name ~* '(phase|load|test|mock|e2e)'
    or endpoint ~* '^https?://[^/]*\.example([/:]|$)'
    or endpoint ~* 'example/'
    or owner_id in (select id from users where data_origin='test')
  );

update gateways g
set data_origin='test'
where data_origin='runtime'
  and (
    g.name ~* '(phase|load|test|mock|e2e)'
    or exists(select 1 from orgs o where o.id=g.org_id and o.data_origin='test')
    or exists(select 1 from users u where u.id=g.created_by and u.data_origin='test')
  );

update gateway_keys k
set data_origin='test'
where data_origin='runtime'
  and (
    k.name ~* '(phase|load|test|mock|e2e)'
    or exists(select 1 from gateways g where g.id=k.gateway_id and g.data_origin='test')
    or exists(select 1 from users u where u.id=k.created_by and u.data_origin='test')
  );

update open_api_sites s
set data_origin='test'
where data_origin='runtime'
  and (
    s.name ~* '(phase|load|test|mock|e2e)'
    or exists(select 1 from users u where u.id=s.created_by and u.data_origin='test')
  );

update notification_channels
set data_origin='demo'
where target ~* '(^|[./])example([/:]|$)' or name ~* 'mock';

update alert_rules
set data_origin='system'
where id in (
  'alr_admin_gateway_error_rate',
  'alr_admin_cost_threshold',
  'alr_admin_l3_failures',
  'alr_admin_quota_anomaly'
);

update alert_rules r
set data_origin='test'
where data_origin='runtime'
  and (
    r.name ~* '(phase|load|test|mock|e2e)'
    or exists(select 1 from orgs o where o.id=r.org_id and o.data_origin='test')
    or exists(select 1 from users u where u.id=r.created_by and u.data_origin='test')
  );

update recommend_picks p
set data_origin=c.data_origin
from channels c
where c.id=p.channel_id and c.data_origin in ('demo','test');

update recommend_rewards r
set data_origin=c.data_origin
from channels c
where c.id=r.channel_id and c.data_origin in ('demo','test');

update recommend_scenarios s
set data_origin=c.data_origin
from channels c
where c.id=s.channel_id and c.data_origin in ('demo','test');
