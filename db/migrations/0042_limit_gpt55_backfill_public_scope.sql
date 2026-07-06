-- GPT-5.5 channels created by the bulk backfill are candidates, not guaranteed
-- provider support. Keep the currently verified GPT-5.5 providers public and
-- leave the rest for admin review before exposing them to users or gateways.
update channels
set public_visible=false,
    gateway_enabled=false,
    updated_at=now()
where owner_type='platform'
  and model='gpt-5.5'
  and coalesce(data_origin,'')='runtime'
  and provider not in ('CrazyRouter','Pipellm')
  and deleted_at is null;
