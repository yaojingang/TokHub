with source_channels as (
  select
    c.*,
    trim(regexp_replace(
      c.name,
      '[[:space:]]*·[[:space:]]*(claude|gpt|gemini|o[0-9]|llama|qwen|deepseek|kimi|glm|mistral|mixtral|yi|doubao|ernie|hunyuan)[[:alnum:]_.-]*$',
      '',
      'i'
    )) as base_name
  from channels c
  join channel_credentials cc on cc.channel_id=c.id
  where c.owner_type='platform'
    and c.deleted_at is null
    and c.status not in ('disabled','deleted')
    and c.model <> 'gpt-5.5'
),
prepared as (
  select
    s.id as source_channel_id,
    'ch_gpt55_' || substr(md5(s.id), 1, 24) as channel_id,
    substr(md5('gpt55:' || s.id), 1, 12) as public_slug,
    case
      when s.base_name = '' then s.provider || ' · gpt-5.5'
      else s.base_name || ' · gpt-5.5'
    end as name,
    s.provider,
    s.endpoint,
    s.official_site_url,
    s.probe_daily,
    s.public_visible,
    s.gateway_enabled,
    coalesce(s.provider_config, '{}'::jsonb)
      - 'clientProfile'
      - 'clientVersion'
      - 'authHeader'
      - 'l3ProbeMode' as provider_config
  from source_channels s
)
insert into channels(
  id,owner_type,owner_id,name,provider,type,model,upstream_model,endpoint,official_site_url,status,score,
  probe_daily,probes_used_today,probe_reset_date,public_visible,gateway_enabled,disabled_at,deleted_at,
  provider_config,data_origin,public_slug,created_at,updated_at
)
select
  p.channel_id,'platform',null,p.name,p.provider,'openai-compatible','gpt-5.5','gpt-5.5',p.endpoint,p.official_site_url,'unknown',0,
  p.probe_daily,0,current_date,p.public_visible,p.gateway_enabled,null,null,
  p.provider_config,'runtime',p.public_slug,now(),now()
from prepared p
on conflict(id) do nothing;

with source_channels as (
  select
    c.*,
    trim(regexp_replace(
      c.name,
      '[[:space:]]*·[[:space:]]*(claude|gpt|gemini|o[0-9]|llama|qwen|deepseek|kimi|glm|mistral|mixtral|yi|doubao|ernie|hunyuan)[[:alnum:]_.-]*$',
      '',
      'i'
    )) as base_name
  from channels c
  join channel_credentials cc on cc.channel_id=c.id
  where c.owner_type='platform'
    and c.deleted_at is null
    and c.status not in ('disabled','deleted')
    and c.model <> 'gpt-5.5'
),
prepared as (
  select
    s.id as source_channel_id,
    'ch_gpt55_' || substr(md5(s.id), 1, 24) as channel_id
  from source_channels s
)
insert into channel_credentials(
  id,channel_id,owner_id,key_ciphertext,key_nonce,key_fingerprint,key_mask,algorithm,created_at,updated_at
)
select
  'cc_gpt55_' || substr(md5(p.source_channel_id), 1, 24),
  p.channel_id,
  cc.owner_id,
  cc.key_ciphertext,
  cc.key_nonce,
  cc.key_fingerprint,
  cc.key_mask,
  cc.algorithm,
  now(),
  now()
from prepared p
join channel_credentials cc on cc.channel_id=p.source_channel_id
join channels target on target.id=p.channel_id
on conflict do nothing;

with source_channels as (
  select c.*
  from channels c
  join channel_credentials cc on cc.channel_id=c.id
  where c.owner_type='platform'
    and c.deleted_at is null
    and c.status not in ('disabled','deleted')
    and c.model <> 'gpt-5.5'
),
prepared as (
  select
    s.id as source_channel_id,
    'ch_gpt55_' || substr(md5(s.id), 1, 24) as channel_id
  from source_channels s
)
insert into channel_status_snapshots(
  id,channel_id,sampled_at,status,score,uptime_24h,success_rate,latency_p95_ms,
  l1_status,l2_status,l3_status,l1_latency_ms,l2_latency_ms,l3_latency_ms,
  tokens_used,cost_usd,error_type,metadata
)
select
  'snap_gpt55_' || substr(md5(p.source_channel_id), 1, 24),
  p.channel_id,now(),'unknown',0,0,0,0,
  'na','na','na',0,0,0,
  0,0,null,jsonb_build_object('source','gpt55_channel_backfill','source_channel_id',p.source_channel_id)
from prepared p
join channels target on target.id=p.channel_id
on conflict(id) do nothing;

with source_channels as (
  select c.*
  from channels c
  join channel_credentials cc on cc.channel_id=c.id
  where c.owner_type='platform'
    and c.deleted_at is null
    and c.status not in ('disabled','deleted')
    and c.model <> 'gpt-5.5'
),
prepared as (
  select
    s.id as source_channel_id,
    'ch_gpt55_' || substr(md5(s.id), 1, 24) as channel_id
  from source_channels s
)
insert into model_prices(id,model_id,channel_id,input_per_mtok,output_per_mtok,currency,effective_at)
select
  'mpr_gpt55_' || substr(md5(p.source_channel_id), 1, 24),
  m.id,
  p.channel_id,
  5.0000,
  30.0000,
  'USD',
  now()
from prepared p
join channels target on target.id=p.channel_id
join model_catalog m on m.model_key='gpt-5.5'
on conflict(id) do update set
  input_per_mtok=excluded.input_per_mtok,
  output_per_mtok=excluded.output_per_mtok,
  currency=excluded.currency,
  effective_at=excluded.effective_at;
