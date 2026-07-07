update gateway_upstreams gu
set enabled=false
from channels c
where gu.channel_id=c.id
  and gu.enabled is true
  and (
    c.deleted_at is not null
    or c.gateway_enabled is not true
    or c.status not in ('healthy','degraded')
  );
