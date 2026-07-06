update recommend_picks rp
set
  cta_label = case
    when rp.cta_label in ('查看详情', '立即试用') then '去官方体验'
    else rp.cta_label
  end,
  cta_url = case
    when rp.cta_url = ''
      or rp.cta_url = '/login'
      or rp.cta_url like '/channels/%'
    then 'https://' || regexp_replace(split_part(split_part(regexp_replace(c.endpoint, '^https?://', '', 'i'), '/', 1), ':', 1), '^(api|api2|cc-api|chat-api|openapi|gateway|proxy|relay|upstream)\.', '')
    else rp.cta_url
  end,
  updated_at = now()
from channels c
where rp.channel_id = c.id
  and c.endpoint ~* '^https?://'
  and (
    rp.cta_label in ('查看详情', '立即试用')
    or rp.cta_url = ''
    or rp.cta_url = '/login'
    or rp.cta_url like '/channels/%'
  );
