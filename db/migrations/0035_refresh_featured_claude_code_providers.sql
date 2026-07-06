with provider_updates(name_key, endpoint, official_url, provider_config) as (
  values
    (
      'aigocode',
      'https://api.aigocode.app',
      'https://aigocode.com/invite/AP5KFJWJ',
      '{"timeoutMs":60000,"clientProfile":"claude-code","l3ContentPolicy":"non_empty"}'::jsonb
    ),
    (
      'packycode',
      'https://www.packyapi.com',
      'https://www.packyapi.com/register?aff=lqD6',
      '{"timeoutMs":60000,"authHeader":"authorization","l3ProbeMode":"l2_only"}'::jsonb
    ),
    (
      'pipellm',
      'https://cc-api.pipellm.ai',
      'https://code.pipellm.ai/login?ref=vbsdxpv8',
      '{"timeoutMs":60000}'::jsonb
    )
)
update channels c
set
  type = 'anthropic',
  model = 'claude-sonnet-4-6',
  upstream_model = 'claude-sonnet-4-6',
  endpoint = u.endpoint,
  official_site_url = u.official_url,
  provider_config = coalesce(c.provider_config, '{}'::jsonb) || u.provider_config,
  updated_at = now()
from provider_updates u
where c.owner_type = 'platform'
  and c.deleted_at is null
  and lower(replace(c.name, ' ', '')) = u.name_key;

update recommend_picks rp
set
  cta_label = '去官方体验',
  cta_url = c.official_site_url,
  updated_at = now()
from channels c
where rp.channel_id = c.id
  and c.owner_type = 'platform'
  and c.deleted_at is null
  and lower(replace(c.name, ' ', '')) in ('aigocode', 'packycode', 'pipellm');
