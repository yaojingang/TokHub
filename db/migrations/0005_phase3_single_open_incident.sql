with ranked_open_incidents as (
  select
    id,
    row_number() over (partition by channel_id order by opened_at desc) as rn
  from incidents
  where resolved_at is null
)
update incidents i
set
  resolved_at = now(),
  metadata = i.metadata || '{"resolved_by":"phase3_single_open_incident"}'::jsonb
from ranked_open_incidents r
where i.id = r.id and r.rn > 1;

create unique index if not exists uniq_open_incident_channel
  on incidents(channel_id)
  where resolved_at is null;
